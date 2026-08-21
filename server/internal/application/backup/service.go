// Package backup contains the provider-independent backup and restore
// application seam. Database commands and artifact storage are injected so
// local command-line tools can be exercised without coupling the application
// layer to a shell, a database driver, or a remote object store.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type Driver string

const (
	DriverMySQL      Driver = "mysql"
	DriverPostgres   Driver = "postgres"
	DriverPostgreSQL Driver = DriverPostgres
)

const EncryptionAES256GCM = "AES-256-GCM-CHUNKED"

var (
	ErrInvalidBackupRequest   = errors.New("invalid backup request")
	ErrEncryptionKeyRequired  = errors.New("backup encryption key is required")
	ErrUnsupportedDriver      = errors.New("unsupported backup database driver")
	ErrBackupFailed           = errors.New("backup failed")
	ErrRestoreFailed          = errors.New("restore failed")
	ErrArtifactDriverMismatch = errors.New("backup artifact driver does not match restore source")
)

// Source identifies a database endpoint. DSN is consumed only by the injected
// command adapter and is never copied into artifact metadata.
type Source struct {
	Driver Driver
	DSN    string
}

// BackupRequest describes one local encrypted backup operation.
type BackupRequest struct {
	Source        Source
	Destination   string
	EncryptionKey []byte
	TargetRPO     time.Duration
	TargetRTO     time.Duration
}

// RestoreRequest describes a restore from a local artifact.
type RestoreRequest struct {
	Source        Source
	ArtifactPath  string
	EncryptionKey []byte
}

// Artifact is credential-free metadata written alongside a local artifact.
// SHA256 is the digest of the plaintext dump; it allows a restore rehearsal to
// prove that the decrypted stream is unchanged without exposing credentials.
type Artifact struct {
	ID              string        `json:"id"`
	Driver          Driver        `json:"driver"`
	Path            string        `json:"path"`
	CreatedAt       time.Time     `json:"createdAt"`
	CompletedAt     time.Time     `json:"completedAt"`
	PlaintextBytes  int64         `json:"plaintextBytes"`
	CiphertextBytes int64         `json:"ciphertextBytes"`
	SHA256          string        `json:"sha256"`
	Encryption      string        `json:"encryption"`
	KeyID           string        `json:"keyId,omitempty"`
	TargetRPO       time.Duration `json:"targetRpo"`
	TargetRTO       time.Duration `json:"targetRto"`
}

// RestoreResult exposes RPO/RTO observations while keeping a successful data
// restore independent from whether an operational target was met.
type RestoreResult struct {
	Artifact    Artifact
	StartedAt   time.Time
	CompletedAt time.Time
	Duration    time.Duration
	ObservedRPO time.Duration
	WithinRPO   bool
	WithinRTO   bool
}

// ArtifactRequest is the storage-port input. Source intentionally contains
// only the driver; credentials stay in the command adapter boundary.
type ArtifactRequest struct {
	Destination   string
	Source        Source
	EncryptionKey []byte
	TargetRPO     time.Duration
	TargetRTO     time.Duration
	CreatedAt     time.Time
}

// Dumper writes a logical database dump to dst. Implementations may invoke
// mysqldump or pg_dump, but the application layer only sees an io.Writer.
type Dumper interface {
	Dump(context.Context, Source, io.Writer) error
}

// Restorer consumes a decrypted logical dump from src.
type Restorer interface {
	Restore(context.Context, Source, io.Reader) error
}

// ArtifactSink is a transactional local-artifact writer. Commit publishes the
// artifact atomically; Abort removes all temporary state.
type ArtifactSink interface {
	io.Writer
	Commit(context.Context) (Artifact, error)
	Abort() error
}

// ArtifactReader exposes decrypted bytes and the verified artifact metadata.
type ArtifactReader interface {
	io.ReadCloser
	Artifact() Artifact
}

// ArtifactStore is intentionally local/provider-neutral. A future remote
// object-store adapter can implement this port without changing Service.
type ArtifactStore interface {
	Create(context.Context, ArtifactRequest) (ArtifactSink, error)
	Open(context.Context, string, []byte) (ArtifactReader, error)
}

type Config struct {
	Clock      func() time.Time
	DefaultRPO time.Duration
	DefaultRTO time.Duration
}

type Service struct {
	dumper     Dumper
	restorer   Restorer
	artifacts  ArtifactStore
	clock      func() time.Time
	defaultRPO time.Duration
	defaultRTO time.Duration
}

func NewService(dumper Dumper, restorer Restorer, artifacts ArtifactStore, config Config) *Service {
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	defaultRPO := config.DefaultRPO
	if defaultRPO <= 0 {
		defaultRPO = 15 * time.Minute
	}
	defaultRTO := config.DefaultRTO
	if defaultRTO <= 0 {
		defaultRTO = 30 * time.Minute
	}
	return &Service{dumper: dumper, restorer: restorer, artifacts: artifacts, clock: clock, defaultRPO: defaultRPO, defaultRTO: defaultRTO}
}

func (s Source) validate() error {
	switch Driver(strings.ToLower(strings.TrimSpace(string(s.Driver)))) {
	case DriverMySQL, DriverPostgres:
	default:
		return fmt.Errorf("%w: %w %q", ErrInvalidBackupRequest, ErrUnsupportedDriver, s.Driver)
	}
	if strings.TrimSpace(s.DSN) == "" {
		return fmt.Errorf("%w: database DSN is required", ErrInvalidBackupRequest)
	}
	return nil
}

func (s *Service) Backup(ctx context.Context, request BackupRequest) (Artifact, error) {
	if s == nil || s.dumper == nil || s.artifacts == nil {
		return Artifact{}, fmt.Errorf("%w: backup service is not configured", ErrInvalidBackupRequest)
	}
	if err := contextError(ctx); err != nil {
		return Artifact{}, err
	}
	if err := request.Source.validate(); err != nil {
		return Artifact{}, err
	}
	if strings.TrimSpace(request.Destination) == "" {
		return Artifact{}, fmt.Errorf("%w: destination is required", ErrInvalidBackupRequest)
	}
	if len(request.EncryptionKey) == 0 {
		return Artifact{}, fmt.Errorf("%w: %w", ErrInvalidBackupRequest, ErrEncryptionKeyRequired)
	}
	rpo, rto, err := s.targets(request.TargetRPO, request.TargetRTO)
	if err != nil {
		return Artifact{}, err
	}
	started := s.clock().UTC()
	sink, err := s.artifacts.Create(ctx, ArtifactRequest{
		Destination:   request.Destination,
		Source:        Source{Driver: normalizedDriver(request.Source.Driver)},
		EncryptionKey: append([]byte(nil), request.EncryptionKey...),
		TargetRPO:     rpo,
		TargetRTO:     rto,
		CreatedAt:     started,
	})
	if err != nil {
		return Artifact{}, fmt.Errorf("%w: create artifact: %w", ErrBackupFailed, err)
	}
	if sink == nil {
		return Artifact{}, fmt.Errorf("%w: artifact store returned a nil sink", ErrBackupFailed)
	}
	if err := s.dumper.Dump(ctx, normalizedSource(request.Source), sink); err != nil {
		_ = sink.Abort()
		return Artifact{}, fmt.Errorf("%w: %w", ErrBackupFailed, err)
	}
	if err := contextError(ctx); err != nil {
		_ = sink.Abort()
		return Artifact{}, err
	}
	artifact, err := sink.Commit(ctx)
	if err != nil {
		_ = sink.Abort()
		return Artifact{}, fmt.Errorf("%w: publish artifact: %w", ErrBackupFailed, err)
	}
	artifact = normalizeArtifact(artifact, request.Destination, normalizedDriver(request.Source.Driver), started, s.clock().UTC(), rpo, rto)
	return artifact, nil
}

func (s *Service) Restore(ctx context.Context, request RestoreRequest) (RestoreResult, error) {
	if s == nil || s.restorer == nil || s.artifacts == nil {
		return RestoreResult{}, fmt.Errorf("%w: restore service is not configured", ErrInvalidBackupRequest)
	}
	if err := contextError(ctx); err != nil {
		return RestoreResult{}, err
	}
	if err := request.Source.validate(); err != nil {
		return RestoreResult{}, err
	}
	if strings.TrimSpace(request.ArtifactPath) == "" {
		return RestoreResult{}, fmt.Errorf("%w: artifact path is required", ErrInvalidBackupRequest)
	}
	if len(request.EncryptionKey) == 0 {
		return RestoreResult{}, fmt.Errorf("%w: %w", ErrInvalidBackupRequest, ErrEncryptionKeyRequired)
	}
	started := s.clock().UTC()
	reader, err := s.artifacts.Open(ctx, request.ArtifactPath, append([]byte(nil), request.EncryptionKey...))
	if err != nil {
		return RestoreResult{}, fmt.Errorf("%w: open artifact: %w", ErrRestoreFailed, err)
	}
	if reader == nil {
		return RestoreResult{}, fmt.Errorf("%w: artifact store returned a nil reader", ErrRestoreFailed)
	}
	defer reader.Close()
	artifact := reader.Artifact()
	if normalizedDriver(artifact.Driver) != normalizedDriver(request.Source.Driver) {
		return RestoreResult{}, ErrArtifactDriverMismatch
	}
	if err := s.restorer.Restore(ctx, normalizedSource(request.Source), reader); err != nil {
		return RestoreResult{}, fmt.Errorf("%w: %w", ErrRestoreFailed, err)
	}
	completed := s.clock().UTC()
	duration := completed.Sub(started)
	if duration < 0 {
		duration = 0
	}
	observedRPO := started.Sub(artifact.CreatedAt)
	if observedRPO < 0 {
		observedRPO = 0
	}
	rpo := artifact.TargetRPO
	if rpo <= 0 {
		rpo = s.defaultRPO
		artifact.TargetRPO = rpo
	}
	rto := artifact.TargetRTO
	if rto <= 0 {
		rto = s.defaultRTO
		artifact.TargetRTO = rto
	}
	return RestoreResult{
		Artifact:    artifact,
		StartedAt:   started,
		CompletedAt: completed,
		Duration:    duration,
		ObservedRPO: observedRPO,
		WithinRPO:   observedRPO <= rpo,
		WithinRTO:   duration <= rto,
	}, nil
}

func (s *Service) targets(rpo, rto time.Duration) (time.Duration, time.Duration, error) {
	if rpo < 0 || rto < 0 {
		return 0, 0, fmt.Errorf("%w: RPO/RTO must not be negative", ErrInvalidBackupRequest)
	}
	if rpo == 0 {
		rpo = s.defaultRPO
	}
	if rto == 0 {
		rto = s.defaultRTO
	}
	return rpo, rto, nil
}

func normalizeSource(source Source) Source {
	source.Driver = normalizedDriver(source.Driver)
	source.DSN = strings.TrimSpace(source.DSN)
	return source
}

func normalizedSource(source Source) Source { return normalizeSource(source) }

func normalizedDriver(driver Driver) Driver {
	return Driver(strings.ToLower(strings.TrimSpace(string(driver))))
}

func normalizeArtifact(artifact Artifact, destination string, driver Driver, started, completed time.Time, rpo, rto time.Duration) Artifact {
	if artifact.Driver == "" {
		artifact.Driver = driver
	}
	if artifact.Path == "" {
		artifact.Path = destination
	}
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = started
	}
	if artifact.CompletedAt.IsZero() {
		artifact.CompletedAt = completed
	}
	if artifact.TargetRPO <= 0 {
		artifact.TargetRPO = rpo
	}
	if artifact.TargetRTO <= 0 {
		artifact.TargetRTO = rto
	}
	return artifact
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
