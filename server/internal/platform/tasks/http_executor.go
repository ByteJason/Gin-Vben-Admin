package tasksplatform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	tasksapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/tasks"
)

var (
	ErrHTTPStatus    = errors.New("task http returned a non-success status")
	ErrHTTPTransport = errors.New("task http transport failed")
)

// HTTPExecutor is the infrastructure adapter. Both deployment capability and
// the persisted task's explicit opt-in are required for private destinations.
type HTTPExecutor struct {
	Client        *http.Client
	AllowInternal bool
	MaxBodyBytes  int64
}
type httpPayload struct {
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	Headers        map[string]string `json:"headers"`
	Body           json.RawMessage   `json:"body"`
	AllowInternal  bool              `json:"allowInternal"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
}

func NewHTTPExecutor(client *http.Client, allowInternal bool) *HTTPExecutor {
	return &HTTPExecutor{Client: client, AllowInternal: allowInternal, MaxBodyBytes: 32768}
}
func (e *HTTPExecutor) Execute(ctx context.Context, payload json.RawMessage) (tasksapp.ExecutionResult, error) {
	var p httpPayload
	if e == nil || len(payload) > 1<<20 || json.Unmarshal(payload, &p) != nil {
		return tasksapp.ExecutionResult{}, tasksapp.ErrInvalidHTTPPayload
	}
	u, err := url.Parse(strings.TrimSpace(p.URL))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return tasksapp.ExecutionResult{}, tasksapp.ErrInvalidHTTPPayload
	}
	allow := e.AllowInternal && p.AllowInternal
	if !allow {
		if ip := net.ParseIP(u.Hostname()); ip != nil && blockedIP(ip) {
			return tasksapp.ExecutionResult{}, tasksapp.ErrSSRFBlocked
		}
		host := strings.ToLower(u.Hostname())
		if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
			return tasksapp.ExecutionResult{}, tasksapp.ErrSSRFBlocked
		}
	}
	method := strings.ToUpper(strings.TrimSpace(p.Method))
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
	default:
		return tasksapp.ExecutionResult{}, tasksapp.ErrInvalidHTTPPayload
	}
	body := string(p.Body)
	if body == "null" {
		body = ""
	}
	if strings.HasPrefix(body, `"`) {
		if json.Unmarshal(p.Body, &body) != nil {
			return tasksapp.ExecutionResult{}, tasksapp.ErrInvalidHTTPPayload
		}
	}
	timeout := 30 * time.Second
	if p.TimeoutSeconds > 0 && p.TimeoutSeconds <= 3600 {
		timeout = time.Duration(p.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), strings.NewReader(body))
	if err != nil {
		return tasksapp.ExecutionResult{}, tasksapp.ErrInvalidHTTPPayload
	}
	secrets := []string{}
	if len(p.Headers) > 64 {
		return tasksapp.ExecutionResult{}, tasksapp.ErrInvalidHTTPPayload
	}
	for k, v := range p.Headers {
		if strings.ContainsAny(k+v, "\r\n") || strings.EqualFold(k, "Host") || strings.HasPrefix(strings.ToLower(k), "proxy-") {
			return tasksapp.ExecutionResult{}, tasksapp.ErrInvalidHTTPPayload
		}
		req.Header.Set(k, v)
		if tasksapp.SensitiveKey(k) {
			secrets = append(secrets, v)
			if strings.HasPrefix(strings.ToLower(v), "bearer ") {
				secrets = append(secrets, v[7:])
			}
		}
	}
	collectSecrets(json.RawMessage(body), &secrets)
	for key, values := range u.Query() {
		if tasksapp.SensitiveKey(key) {
			secrets = append(secrets, values...)
		}
	}
	client := http.Client{Timeout: timeout}
	if e.Client != nil {
		client = *e.Client
	}
	client.Jar = nil
	// Never forward a credential-bearing request to a redirect. Non-2xx is a
	// failure, and redirects can be retried only after updating the definition.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if provided, ok := client.Transport.(*http.Transport); ok {
		transport = provided.Clone()
	} else if client.Transport != nil {
		return tasksapp.ExecutionResult{}, tasksapp.ErrInvalidHTTPPayload
	}
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, tasksapp.ErrSSRFBlocked
		}
		for _, ip := range ips {
			if !allow && blockedIP(ip.IP) {
				return nil, tasksapp.ErrSSRFBlocked
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	client.Transport = transport
	defer transport.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, tasksapp.ErrSSRFBlocked):
			return tasksapp.ExecutionResult{}, tasksapp.ErrSSRFBlocked
		case ctx.Err() != nil:
			return tasksapp.ExecutionResult{}, ctx.Err()
		default:
			return tasksapp.ExecutionResult{}, ErrHTTPTransport
		}
	}
	defer resp.Body.Close()
	max := e.MaxBodyBytes
	if max <= 0 || max > 32768 {
		max = 32768
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		if ctx.Err() != nil {
			return tasksapp.ExecutionResult{}, ctx.Err()
		}
		return tasksapp.ExecutionResult{}, ErrHTTPTransport
	}
	truncated := int64(len(data)) > max
	if truncated {
		data = data[:max]
	}
	output := tasksapp.RedactOutput(string(data), secrets...)
	if truncated {
		output += "\n[truncated]"
	}
	result := tasksapp.ExecutionResult{ResultSummary: fmt.Sprintf("HTTP %d", resp.StatusCode), RedactedOutput: output}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, ErrHTTPStatus
	}
	return result, nil
}
func blockedIP(ip net.IP) bool {
	return !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.Equal(net.IPv4bcast) || ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1] >= 64 && ip.To4()[1] <= 127
}
func collectSecrets(raw json.RawMessage, secrets *[]string) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return
	}
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for k, item := range v {
				if tasksapp.SensitiveKey(k) {
					if text, ok := item.(string); ok {
						*secrets = append(*secrets, text)
					}
				} else {
					visit(item)
				}
			}
		case []any:
			for _, item := range v {
				visit(item)
			}
		}
	}
	visit(value)
}
