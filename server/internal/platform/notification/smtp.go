// Package notificationplatform contains concrete notification transports.
package notificationplatform

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	appnotification "example.com/gin-vben-admin/server/internal/application/notification"
)

// SMTPMailer sends application messages over a single SMTP connection. It
// deliberately owns no retry loop or queue; those belong to a later worker
// slice. Errors are reduced to stable provider categories so credentials and
// remote server details never cross the application boundary.
type SMTPMailer struct {
	config      appnotification.SMTPConfig
	dialContext func(context.Context, string, string) (net.Conn, error)
	account     *appnotification.SMTPAccount
}

// SMTPPoolConfig configures multi-account selection. An empty Selection uses weighted_random.
type SMTPPoolConfig struct {
	Accounts  []appnotification.SMTPAccount
	Selection appnotification.SMTPSelection
}

// SMTPPoolMailer selects enabled accounts and retries each selected candidate at most once.
type SMTPPoolMailer struct {
	accounts    []appnotification.SMTPAccount
	selection   appnotification.SMTPSelection
	dialContext func(context.Context, string, string) (net.Conn, error)
	rng         func(int) int
	sequence    uint64
	cooldown    time.Duration
	now         func() time.Time
	failed      map[string]time.Time
	failedMu    sync.Mutex
	maxAttempts int
	retryDelays []time.Duration
}

// SMTPMultiMailer is an alias used by integrations that call the pool a multi-mailer.
type SMTPMultiMailer = SMTPPoolMailer

// SMTPAccountProvider adapts one persisted account to the application mail
// service. Selection/retry remains in the application layer when delivery
// records need an account id; this adapter only speaks SMTP.
type SMTPAccountProvider struct {
	dialContext func(context.Context, string, string) (net.Conn, error)
}

func NewSMTPAccountProvider() *SMTPAccountProvider {
	return &SMTPAccountProvider{dialContext: (&net.Dialer{}).DialContext}
}

func (p *SMTPAccountProvider) SetDialContext(fn func(context.Context, string, string) (net.Conn, error)) {
	if p != nil && fn != nil {
		p.dialContext = fn
	}
}

func (p *SMTPAccountProvider) Send(ctx context.Context, account appnotification.SMTPAccount, message appnotification.Message) error {
	if p == nil {
		return appnotification.ErrProvider
	}
	mailer, err := NewSMTPMailerFromAccount(account)
	if err != nil {
		return err
	}
	if p.dialContext != nil {
		mailer.dialContext = p.dialContext
	}
	return mailer.Send(ctx, message)
}

// SendWithResult returns the locally generated RFC Message-ID value. The
// SMTP adapter does not expose server credentials or response text; the
// application records this stable id for idempotency/audit correlation.
func (p *SMTPAccountProvider) SendWithResult(ctx context.Context, account appnotification.SMTPAccount, message appnotification.Message) (string, error) {
	if err := p.Send(ctx, account, message); err != nil {
		return "", err
	}
	return strings.TrimSpace(message.ID), nil
}

func (p *SMTPAccountProvider) TestConnection(ctx context.Context, account appnotification.SMTPAccount) error {
	if p == nil {
		return appnotification.ErrProvider
	}
	mailer, err := NewSMTPMailerFromAccount(account)
	if err != nil {
		return err
	}
	if p.dialContext != nil {
		mailer.dialContext = p.dialContext
	}
	return mailer.TestConnection(ctx)
}

func NewSMTPPoolMailer(cfg SMTPPoolConfig) (*SMTPPoolMailer, error) {
	sel := cfg.Selection
	if sel == "" {
		sel = appnotification.SMTPSelectionWeightedRandom
	}
	if sel != appnotification.SMTPSelectionWeightedRandom && sel != appnotification.SMTPSelectionRoundRobin {
		return nil, appnotification.ErrInvalidMessage
	}
	accounts := make([]appnotification.SMTPAccount, 0, len(cfg.Accounts))
	for _, a := range cfg.Accounts {
		if !a.Enabled {
			continue
		}
		if err := a.Validate(); err != nil {
			return nil, err
		}
		if a.Weight == 0 {
			a.Weight = 1
		}
		accounts = append(accounts, a)
	}
	if len(accounts) == 0 {
		return nil, appnotification.ErrInvalidMessage
	}
	return &SMTPPoolMailer{
		accounts:    accounts,
		selection:   sel,
		dialContext: (&net.Dialer{}).DialContext,
		rng:         rand.Intn,
		now:         time.Now,
		failed:      make(map[string]time.Time),
		maxAttempts: 3,
		retryDelays: []time.Duration{time.Second, 2 * time.Second, 4 * time.Second},
	}, nil
}

func NewSMTPMultiMailer(cfg SMTPPoolConfig) (*SMTPMultiMailer, error) { return NewSMTPPoolMailer(cfg) }

// SetRNG and SetDialContext are deterministic seams for tests.
func (m *SMTPPoolMailer) SetRNG(fn func(int) int) {
	if fn != nil {
		m.rng = fn
	}
}
func (m *SMTPPoolMailer) SetDialContext(fn func(context.Context, string, string) (net.Conn, error)) {
	if fn != nil {
		m.dialContext = fn
	}
}
func (m *SMTPPoolMailer) SetCooldown(d time.Duration) {
	if d >= 0 {
		m.cooldown = d
	}
}
func (m *SMTPPoolMailer) SetClock(fn func() time.Time) {
	if fn != nil {
		m.now = fn
	}
}

// SetRetryPolicy is a deterministic seam for tests and operators. Production
// defaults are three candidates with 1s/2s/4s exponential backoff.
func (m *SMTPPoolMailer) SetRetryPolicy(maxAttempts int, delays []time.Duration) {
	if maxAttempts > 0 {
		m.maxAttempts = maxAttempts
	}
	if delays != nil {
		m.retryDelays = append([]time.Duration(nil), delays...)
	}
}

func accountKey(a appnotification.SMTPAccount) string {
	return a.Name + "\x00" + a.TenantID + "\x00" + a.Host + ":" + strconv.Itoa(a.Port) + "\x00" + a.Username
}
func (m *SMTPPoolMailer) isCooling(a appnotification.SMTPAccount) bool {
	if m.cooldown <= 0 {
		return false
	}
	now := m.now
	if now == nil {
		now = time.Now
	}
	m.failedMu.Lock()
	defer m.failedMu.Unlock()
	until, ok := m.failed[accountKey(a)]
	return ok && now().Before(until)
}
func (m *SMTPPoolMailer) markFailure(a appnotification.SMTPAccount) {
	if m.cooldown <= 0 {
		return
	}
	now := m.now
	if now == nil {
		now = time.Now
	}
	m.failedMu.Lock()
	defer m.failedMu.Unlock()
	if m.failed == nil {
		m.failed = make(map[string]time.Time)
	}
	m.failed[accountKey(a)] = now().Add(m.cooldown)
}

func (m *SMTPPoolMailer) pick() []appnotification.SMTPAccount {
	if len(m.accounts) == 0 {
		return nil
	}
	start := 0
	if m.selection == appnotification.SMTPSelectionRoundRobin {
		start = int(atomic.AddUint64(&m.sequence, 1)-1) % len(m.accounts)
	} else {
		total := 0
		for _, a := range m.accounts {
			total += a.Weight
		}
		rng := m.rng
		if rng == nil {
			rng = rand.Intn
		}
		n := rng(total)
		if n < 0 {
			n = 0
		}
		n %= total
		for i, a := range m.accounts {
			if n < a.Weight {
				start = i
				break
			}
			n -= a.Weight
		}
	}
	out := make([]appnotification.SMTPAccount, 0, len(m.accounts))
	for i := range m.accounts {
		out = append(out, m.accounts[(start+i)%len(m.accounts)])
	}
	return out
}

func (m *SMTPPoolMailer) Send(ctx context.Context, msg appnotification.Message) error {
	if m == nil || ctx == nil {
		return appnotification.ErrInvalidMessage
	}
	maxAttempts := m.maxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	var last error
	attempts := 0
	for _, account := range m.pick() {
		if attempts >= maxAttempts {
			break
		}
		if m.isCooling(account) {
			continue
		}
		if attempts > 0 {
			delay := time.Duration(0)
			if len(m.retryDelays) > 0 {
				idx := attempts - 1
				if idx >= len(m.retryDelays) {
					idx = len(m.retryDelays) - 1
				}
				delay = m.retryDelays[idx]
			}
			if err := waitRetry(ctx, delay); err != nil {
				return err
			}
		}
		attempts++
		mailer := &SMTPMailer{config: appnotification.SMTPConfig{Host: account.Host, Port: account.Port, Username: account.Username, Password: account.Password, From: account.FromEmail, StartTLS: accountNeedsStartTLS(account)}, dialContext: m.dialContext}
		if err := mailer.sendWithAccount(ctx, msg, account); err != nil {
			last = err
			m.markFailure(account)
			if ctx.Err() != nil {
				break
			}
			continue
		}
		return nil
	}
	if last == nil {
		return appnotification.ErrProvider
	}
	return last
}

// NewSMTPMailer validates configuration without opening a network connection.
// Mailpit uses StartTLS=false and no credentials; production SMTP can opt into
// STARTTLS and AUTH through the same seam.
func NewSMTPMailer(cfg appnotification.SMTPConfig) (*SMTPMailer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if strings.ContainsAny(cfg.Host+cfg.Username+cfg.Password+cfg.From, "\r\n") {
		return nil, appnotification.ErrInvalidMessage
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(cfg.From)); err != nil {
		return nil, appnotification.ErrInvalidMessage
	}
	return &SMTPMailer{
		config:      cfg,
		dialContext: (&net.Dialer{}).DialContext,
	}, nil
}

// NewSMTPMailerFromAccount keeps the single-account Mailer seam available to callers
// that already model configuration as SMTPAccount.
func NewSMTPMailerFromAccount(account appnotification.SMTPAccount) (*SMTPMailer, error) {
	if err := account.Validate(); err != nil {
		return nil, err
	}
	mailer, err := NewSMTPMailer(appnotification.SMTPConfig{Host: account.Host, Port: account.Port, Username: account.Username, Password: account.Password, From: account.FromEmail, StartTLS: accountNeedsStartTLS(account)})
	if err != nil {
		return nil, err
	}
	copy := account
	mailer.account = &copy
	return mailer, nil
}

// Send implements notification.Mailer. The body is written only to the SMTP
// DATA stream and is never included in an error or status payload.
func (m *SMTPMailer) Send(ctx context.Context, message appnotification.Message) error {
	if m == nil {
		return appnotification.ErrInvalidMessage
	}
	account := appnotification.SMTPAccount{Host: m.config.Host, Port: m.config.Port, Username: m.config.Username, Password: m.config.Password, FromEmail: m.config.From, ImplicitTLS: false}
	if m.account != nil {
		account = *m.account
	}
	return m.sendWithAccount(ctx, message, account)
}

func (m *SMTPMailer) sendWithAccount(ctx context.Context, message appnotification.Message, account appnotification.SMTPAccount) error {
	if m == nil || m.dialContext == nil || ctx == nil {
		return appnotification.ErrInvalidMessage
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	to := strings.TrimSpace(message.To)
	subject := strings.TrimSpace(message.Subject)
	if subject == "" || strings.ContainsAny(to+subject, "\r\n") {
		return appnotification.ErrInvalidMessage
	}
	recipientValues := append([]string(nil), message.Recipients...)
	if len(recipientValues) == 0 && to != "" {
		recipientValues = []string{to}
	}
	if len(recipientValues) == 0 || len(recipientValues) > 100 {
		return appnotification.ErrInvalidMessage
	}
	recipients := make([]string, 0, len(recipientValues))
	seenRecipients := make(map[string]struct{}, len(recipientValues))
	for _, value := range recipientValues {
		value = strings.TrimSpace(value)
		if value == "" || strings.ContainsAny(value, "\r\n") {
			return appnotification.ErrInvalidMessage
		}
		recipient, parseErr := mail.ParseAddress(value)
		if parseErr != nil || strings.TrimSpace(recipient.Address) == "" {
			return appnotification.ErrInvalidMessage
		}
		address := strings.ToLower(strings.TrimSpace(recipient.Address))
		if _, duplicate := seenRecipients[address]; duplicate {
			continue
		}
		seenRecipients[address] = struct{}{}
		recipients = append(recipients, address)
	}
	if len(recipients) == 0 {
		return appnotification.ErrInvalidMessage
	}

	host, port, username, password, from := strings.TrimSpace(account.Host), account.Port, account.Username, account.Password, strings.TrimSpace(account.FromEmail)
	if from == "" {
		from = strings.TrimSpace(m.config.From)
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := m.dialContext(ctx, "tcp", address)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return providerFailure("dial")
	}
	done := make(chan struct{})
	go closeOnContext(ctx, conn, done)
	defer close(done)
	defer conn.Close()
	secure := false
	if account.ImplicitTLS {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return providerFailure("tls_handshake")
		}
		conn = tlsConn
		secure = true
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return providerFailure("connect")
	}
	defer client.Close()

	if m.config.StartTLS && !account.ImplicitTLS {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return providerFailure("starttls")
		}
		tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(tlsConfig); err != nil {
			return providerFailure("starttls")
		}
		secure = true
	}
	if strings.TrimSpace(username) != "" {
		if !secure {
			return providerFailure("tls_required")
		}
		auth := smtp.PlainAuth("", username, password, host)
		if err := client.Auth(auth); err != nil {
			return providerFailure("auth")
		}
	}
	if err := client.Mail(from); err != nil {
		return providerFailure("mail")
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return providerFailure("recipient")
		}
	}
	writer, err := client.Data()
	if err != nil {
		return providerFailure("data")
	}
	headerFrom := from
	if name := strings.TrimSpace(account.FromName); name != "" {
		headerFrom = (&mail.Address{Name: name, Address: from}).String()
	}
	if err := writeMessage(writer, headerFrom, strings.Join(recipients, ", "), message); err != nil {
		_ = writer.Close()
		return providerFailure("write")
	}
	if err := writer.Close(); err != nil {
		return providerFailure("commit")
	}
	if err := client.Quit(); err != nil {
		return providerFailure("quit")
	}
	return nil
}

// TestConnection performs TCP, TLS/STARTTLS, EHLO and optional AUTH only; it never issues MAIL/DATA.
func (m *SMTPMailer) TestConnection(ctx context.Context) error {
	if m == nil {
		return appnotification.ErrInvalidMessage
	}
	a := appnotification.SMTPAccount{Host: m.config.Host, Port: m.config.Port, Username: m.config.Username, Password: m.config.Password, ImplicitTLS: false}
	if m.account != nil {
		a = *m.account
	}
	return m.testConnection(ctx, a)
}

func (m *SMTPMailer) testConnection(ctx context.Context, a appnotification.SMTPAccount) error {
	if m.dialContext == nil || ctx == nil {
		return appnotification.ErrInvalidMessage
	}
	host := strings.TrimSpace(a.Host)
	conn, err := m.dialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(a.Port)))
	if err != nil {
		return providerFailure("dial")
	}
	defer conn.Close()
	secure := false
	if a.ImplicitTLS {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return providerFailure("tls_handshake")
		}
		conn = tlsConn
		secure = true
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return providerFailure("connect")
	}
	defer client.Close()
	if m.config.StartTLS && !a.ImplicitTLS {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return providerFailure("starttls")
		}
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return providerFailure("starttls")
		}
		secure = true
	}
	if strings.TrimSpace(a.Username) != "" {
		if !secure {
			return providerFailure("tls_required")
		}
		if err := client.Auth(smtp.PlainAuth("", a.Username, a.Password, host)); err != nil {
			return providerFailure("auth")
		}
	}
	return nil
}

func (m *SMTPPoolMailer) TestConnections(ctx context.Context) map[string]error {
	if m == nil {
		return map[string]error{}
	}
	result := make(map[string]error, len(m.accounts))
	for _, a := range m.accounts {
		name := a.Name
		if name == "" {
			name = a.Host
		}
		mailer := &SMTPMailer{config: appnotification.SMTPConfig{StartTLS: accountNeedsStartTLS(a)}, dialContext: m.dialContext}
		result[name] = mailer.testConnection(ctx, a)
	}
	return result
}

func accountNeedsStartTLS(a appnotification.SMTPAccount) bool {
	return !a.ImplicitTLS && (a.Port == 587 || strings.TrimSpace(a.Username) != "")
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func writeMessage(writer io.Writer, from, to string, message appnotification.Message) error {
	buffer := bufio.NewWriter(writer)
	if _, err := fmt.Fprintf(buffer, "From: %s\r\nTo: %s\r\nSubject: %s\r\n", from, to, mime.QEncoding.Encode("UTF-8", strings.TrimSpace(message.Subject))); err != nil {
		return err
	}
	if !message.CreatedAt.IsZero() {
		if _, err := fmt.Fprintf(buffer, "Date: %s\r\n", message.CreatedAt.UTC().Format(time.RFC1123Z)); err != nil {
			return err
		}
	}
	if id := strings.TrimSpace(message.ID); id != "" && !strings.ContainsAny(id, "\r\n") {
		if _, err := fmt.Fprintf(buffer, "Message-ID: <%s>\r\n", id); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(buffer, "MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(buffer, strings.ReplaceAll(message.Body, "\n", "\r\n")); err != nil {
		return err
	}
	return buffer.Flush()
}

func closeOnContext(ctx context.Context, conn net.Conn, done <-chan struct{}) {
	select {
	case <-ctx.Done():
		_ = conn.Close()
	case <-done:
	}
}

// SMTPError carries stable machine-readable stage/code values without remote details.
type SMTPError struct {
	Stage string
	Code  string
}

func (e *SMTPError) Error() string { return "smtp " + e.Stage + " failed (" + e.Code + ")" }
func (e *SMTPError) Unwrap() error { return appnotification.ErrProvider }
func (e *SMTPError) SMTPStage() string {
	if e == nil {
		return ""
	}
	return e.Stage
}
func (e *SMTPError) SMTPCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func providerFailure(stage string) error {
	return &SMTPError{Stage: stage, Code: "smtp_" + stage}
}

var _ appnotification.Mailer = (*SMTPMailer)(nil)
