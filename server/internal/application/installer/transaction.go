package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

const (
	ApplyTransactionSchema = 1
	ApplyTransactionOwner  = "server-installer"
)

type ApplyTransactionPhase string

const (
	TransactionApplying            ApplyTransactionPhase = "applying"
	TransactionRetryable           ApplyTransactionPhase = "retryable"
	TransactionCompensationPending ApplyTransactionPhase = "compensation_pending"
)

var ErrTransactionChanged = errors.New("installation transaction changed")

// ApplyTransaction is the complete allowlist for the durable web installer
// journal. It intentionally has no request, database, Redis, administrator,
// password, DSN, token, or environment-value field.
type ApplyTransaction struct {
	Schema            int                   `json:"schema"`
	Owner             string                `json:"owner"`
	ID                string                `json:"id"`
	SelectedUI        installstate.UI       `json:"selectedUi"`
	Mode              installstate.Mode     `json:"mode"`
	DatabaseTarget    string                `json:"databaseTarget"`
	Phase             ApplyTransactionPhase `json:"phase"`
	CurrentStep       string                `json:"currentStep"`
	CompletedSteps    []string              `json:"completedSteps"`
	Identity          *IdentityReceipt      `json:"identity,omitempty"`
	EnvironmentIntent string                `json:"environmentIntent,omitempty"`
	Environment       *EnvironmentReceipt   `json:"environment,omitempty"`
	Marker            *installstate.Marker  `json:"marker,omitempty"`
	UpdatedAt         time.Time             `json:"updatedAt"`
}

type ApplyTransactionJournal interface {
	Load(context.Context) (ApplyTransaction, bool, error)
	Create(context.Context, ApplyTransaction) error
	Update(context.Context, ApplyTransaction) error
	Remove(context.Context, string) error
}

// ApplyOwnership serializes apply and recovery across server processes. The
// returned release function owns the lease for the complete orchestration,
// not merely an individual journal update.
type ApplyOwnership interface {
	AcquireApply(context.Context) (func() error, error)
}

type CompletionHousekeeper interface {
	CleanupCompleted(context.Context, installstate.Marker) error
}

func (t ApplyTransaction) Validate() error {
	if t.Schema != ApplyTransactionSchema || t.Owner != ApplyTransactionOwner || !validTransactionID(t.ID) {
		return errors.New("installation transaction identity is invalid")
	}
	if !validProfile(InstallationProfile{SelectedUI: t.SelectedUI}) {
		return errors.New("installation transaction UI is invalid")
	}
	switch t.Mode {
	case installstate.ModeEmbedded, installstate.ModeStandalone, installstate.ModeAPIOnly, installstate.ModeDev:
	default:
		return errors.New("installation transaction mode is invalid")
	}
	switch t.Phase {
	case TransactionApplying, TransactionRetryable, TransactionCompensationPending:
	default:
		return errors.New("installation transaction phase is invalid")
	}
	if !validDigest(t.DatabaseTarget) || !validTransactionStep(t.CurrentStep) || t.UpdatedAt.IsZero() {
		return errors.New("installation transaction progress is invalid")
	}
	seen := make(map[string]struct{}, len(t.CompletedSteps))
	for _, step := range t.CompletedSteps {
		if !validCompletedTransactionStep(step) {
			return errors.New("installation transaction completed step is invalid")
		}
		if _, duplicate := seen[step]; duplicate {
			return errors.New("installation transaction completed step is duplicated")
		}
		seen[step] = struct{}{}
	}
	if t.Identity != nil && t.Identity.Reference != t.ID {
		return errors.New("installation identity receipt is invalid")
	}
	if t.EnvironmentIntent != "" && t.EnvironmentIntent != t.ID {
		return errors.New("installation environment intent is invalid")
	}
	if t.Environment != nil {
		if t.Environment.Reference != t.ID || !validDigest(t.Environment.Digest) {
			return errors.New("installation environment receipt is invalid")
		}
		if t.Environment.Replaced {
			if !validDigest(t.Environment.PreviousDigest) || !validBackupName(t.Environment.BackupName) || t.Environment.BackupName != ".env.previous-"+t.Environment.Reference {
				return errors.New("installation environment replacement receipt is invalid")
			}
		} else if t.Environment.PreviousDigest != "" || t.Environment.BackupName != "" {
			return errors.New("installation environment creation receipt is invalid")
		}
	}
	if t.Marker != nil {
		if err := t.Marker.Validate(); err != nil || t.Marker.SelectedUI != t.SelectedUI || t.Marker.Mode != t.Mode {
			return errors.New("installation marker receipt is invalid")
		}
	}
	return nil
}

// databaseTargetDigest binds a recovery journal to the database instance
// without persisting a hostname, database name, username, password, or DSN.
// A non-empty DSN takes precedence exactly as it does in the runtime adapter;
// otherwise the structured target is used. Credentials are excluded from
// both forms so rotating a password does not prevent compensation.
func databaseTargetDigest(connection DatabaseConnection) (string, error) {
	if err := validateDatabaseConnection(connection); err != nil {
		return "", err
	}
	driver := strings.ToLower(strings.TrimSpace(connection.Driver))
	mode := strings.ToLower(strings.TrimSpace(connection.Mode))
	if mode == "" {
		mode = "single"
	}

	endpoints := make([]string, 0, 3)
	switch mode {
	case "single", "cluster_endpoint":
		if strings.TrimSpace(connection.DSN) != "" {
			target, err := databaseDSNTarget(driver, connection.DSN)
			if err != nil {
				return "", err
			}
			endpoints = append(endpoints, target)
		} else if target, ok := structuredDatabaseTarget(connection); ok {
			endpoints = append(endpoints, target)
		}
	case "read_write":
		primary, err := databaseDSNTarget(driver, connection.PrimaryDSN)
		if err != nil {
			return "", err
		}
		replicas := make([]string, 0, len(connection.ReplicaDSNs))
		for _, raw := range nonEmptyStrings(connection.ReplicaDSNs) {
			target, targetErr := databaseDSNTarget(driver, raw)
			if targetErr != nil {
				return "", targetErr
			}
			replicas = append(replicas, target)
		}
		sort.Strings(replicas)
		endpoints = append(endpoints, "primary:"+primary)
		for _, replica := range replicas {
			endpoints = append(endpoints, "replica:"+replica)
		}
	}
	if len(endpoints) == 0 {
		return "", errors.New("database target is unavailable")
	}
	material := "database-target:v1\x00" + driver + "\x00" + mode + "\x00" + strings.Join(endpoints, "\x00")
	digest := sha256.Sum256([]byte(material))
	return hex.EncodeToString(digest[:]), nil
}

func structuredDatabaseTarget(connection DatabaseConnection) (string, bool) {
	host := strings.TrimSpace(connection.Host)
	database := strings.TrimSpace(connection.Database)
	if host == "" || connection.Port <= 0 || database == "" {
		return "", false
	}
	host = strings.Trim(host, "[]")
	if parsed := net.ParseIP(host); parsed != nil {
		host = parsed.String()
	} else {
		host = strings.ToLower(host)
	}
	return host + "\x00" + strconv.Itoa(connection.Port) + "\x00" + database, true
}

func databaseDSNTarget(driver, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("database endpoint is required")
	}
	if driver == "postgres" {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			host := strings.ToLower(parsed.Hostname())
			port := parsed.Port()
			if port == "" {
				port = "5432"
			}
			database, unescapeErr := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
			if unescapeErr == nil && host != "" && database != "" {
				return host + "\x00" + port + "\x00" + database, nil
			}
		}
		values := make(map[string]string)
		for _, field := range strings.Fields(raw) {
			key, value, found := strings.Cut(field, "=")
			if found {
				values[strings.ToLower(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(value), "'\"")
			}
		}
		host := strings.ToLower(values["host"])
		database := values["dbname"]
		if database == "" {
			database = values["database"]
		}
		if host != "" && database != "" {
			port := values["port"]
			if port == "" {
				port = "5432"
			}
			return host + "\x00" + port + "\x00" + database, nil
		}
		return "", errors.New("postgres database target cannot be derived from DSN")
	}

	withoutCredentials := raw
	if at := strings.LastIndex(withoutCredentials, "@"); at >= 0 {
		withoutCredentials = withoutCredentials[at+1:]
	}
	separator := strings.LastIndex(withoutCredentials, "/")
	if separator <= 0 || separator == len(withoutCredentials)-1 {
		return "", errors.New("mysql database target cannot be derived from DSN")
	}
	endpoint := strings.ToLower(strings.TrimSpace(withoutCredentials[:separator]))
	database, _, _ := strings.Cut(strings.TrimSpace(withoutCredentials[separator+1:]), "?")
	if endpoint == "" || database == "" {
		return "", errors.New("mysql database target cannot be derived from DSN")
	}
	return endpoint + "\x00" + database, nil
}

func validBackupName(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 255 && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00\r\n")
}

func validTransactionID(value string) bool {
	if !strings.HasPrefix(value, "install-") || len(value) != len("install-")+32 {
		return false
	}
	for _, character := range strings.TrimPrefix(value, "install-") {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validTransactionStep(step string) bool {
	switch step {
	case "schema", "identity", "environment", "lock", "failed":
		return true
	default:
		return false
	}
}

func validCompletedTransactionStep(step string) bool {
	switch step {
	case "plan", "database", "redis", "schema", "identity", "environment":
		return true
	default:
		return false
	}
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
