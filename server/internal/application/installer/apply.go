package installer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	installstate "example.com/gin-vben-admin/server/internal/domain/installstate"
	platformi18n "example.com/gin-vben-admin/server/internal/platform/i18n"
)

var (
	ErrAlreadyInstalled = errors.New("application is already installed")
	ErrApplyBusy        = errors.New("installation is already running")
	ErrInvalidApply     = errors.New("installation request is invalid")
	ErrPreflightFailed  = errors.New("installation preflight failed")
	ErrApplyFailed      = errors.New("installation apply failed")
	ErrApplyRollback    = errors.New("installation rollback failed")
)

type AdminAccount struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ApplyRequest struct {
	SelectedUI     string             `json:"selectedUi"`
	Mode           string             `json:"mode"`
	Locale         string             `json:"locale,omitempty"`
	LocaleMode     string             `json:"localeMode,omitempty"`
	Database       DatabaseConnection `json:"database"`
	Redis          RedisConnection    `json:"redis"`
	Admin          AdminAccount       `json:"admin"`
	ConfirmCleanup bool               `json:"confirmCleanup"`
}

type StepStatus string

const StepCompleted StepStatus = "completed"

type ApplyStep struct {
	ID     string     `json:"id"`
	Status StepStatus `json:"status"`
}

type ApplyResult struct {
	State       State             `json:"state"`
	SelectedUI  installstate.UI   `json:"selectedUi"`
	Mode        installstate.Mode `json:"mode"`
	InstalledAt time.Time         `json:"installedAt"`
	Steps       []ApplyStep       `json:"steps"`
}

type SchemaReceipt struct {
	Version         uint
	PreviousVersion uint
	AppliedSteps    uint
}

type AssetReceipt struct {
	ArtifactHash string
	ManifestHash string
	Reference    string
}

type IdentityReceipt struct {
	Reference string
}

type EnvironmentReceipt struct {
	Digest    string
	Reference string
}

type ApplyMarkerStore interface {
	Load(context.Context) (installstate.Marker, bool, error)
	Create(context.Context, installstate.Marker) error
	Remove(context.Context, installstate.Marker) error
}

type ApplyDependencyChecker interface {
	CheckDatabase(context.Context, DatabaseConnection) (DependencyCheck, error)
	CheckRedis(context.Context, RedisConnection) (DependencyCheck, error)
}

type SchemaInstaller interface {
	Up(context.Context, DatabaseConnection) (SchemaReceipt, error)
}

type AssetInstaller interface {
	Prepare(context.Context, Plan) (AssetReceipt, error)
	Rollback(context.Context, AssetReceipt) error
}

type IdentityInstaller interface {
	Initialize(context.Context, DatabaseConnection, AdminAccount) (IdentityReceipt, error)
	Rollback(context.Context, IdentityReceipt) error
}

type EnvironmentInstaller interface {
	Publish(context.Context, ApplyRequest, AssetReceipt) (EnvironmentReceipt, error)
	Rollback(context.Context, EnvironmentReceipt) error
}

type ApplyService struct {
	markers      ApplyMarkerStore
	plans        PlanProvider
	dependencies ApplyDependencyChecker
	schemas      SchemaInstaller
	assets       AssetInstaller
	identity     IdentityInstaller
	environment  EnvironmentInstaller
	now          func() time.Time
	mutex        sync.Mutex
	pending      *pendingRollback
}

type pendingRollback struct {
	assets      *AssetReceipt
	identity    *IdentityReceipt
	environment *EnvironmentReceipt
	marker      *installstate.Marker
}

func NewApplyService(markers ApplyMarkerStore, plans PlanProvider, dependencies ApplyDependencyChecker, schemas SchemaInstaller, assets AssetInstaller, identity IdentityInstaller, environment EnvironmentInstaller, now func() time.Time) *ApplyService {
	if now == nil {
		now = time.Now
	}
	return &ApplyService{
		markers: markers, plans: plans, dependencies: dependencies, schemas: schemas,
		assets: assets, identity: identity, environment: environment, now: now,
	}
}

func (s *ApplyService) Apply(ctx context.Context, request ApplyRequest) (ApplyResult, error) {
	return s.apply(ctx, request, nil)
}

// ApplyWithProgress is the asynchronous-install seam. The callback receives
// only allowlisted stage identifiers and never receives credentials or raw
// infrastructure errors.
func (s *ApplyService) ApplyWithProgress(ctx context.Context, request ApplyRequest, report func(string)) (ApplyResult, error) {
	return s.apply(ctx, request, report)
}

func (s *ApplyService) apply(ctx context.Context, request ApplyRequest, report func(string)) (ApplyResult, error) {
	if s == nil || s.markers == nil || s.plans == nil || s.dependencies == nil || s.schemas == nil || s.assets == nil || s.identity == nil || s.environment == nil {
		return ApplyResult{}, errors.New("installation apply service is not configured")
	}
	if !s.mutex.TryLock() {
		return ApplyResult{}, ErrApplyBusy
	}
	defer s.mutex.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}
	ui, mode, err := validateApplyRequest(request)
	if err != nil {
		return ApplyResult{}, err
	}
	if _, installed, err := s.markers.Load(ctx); err != nil {
		return ApplyResult{}, errors.New("read installation lock")
	} else if installed {
		return ApplyResult{}, ErrAlreadyInstalled
	}

	steps := make([]ApplyStep, 0, 8)
	plan, err := s.plans.Plan(ctx, PlanRequest{SelectedUI: string(ui), Mode: string(mode)})
	if err != nil || plan.SelectedUI != ui || plan.Mode != mode || !plan.CanCleanup || !plan.CanBuild || !plan.CanWriteEnv {
		return ApplyResult{}, ErrPreflightFailed
	}
	steps = appendCompleted(steps, "plan")
	reportApplyStep(report, "plan")

	database, err := s.dependencies.CheckDatabase(ctx, request.Database)
	if err != nil || !database.OK {
		return ApplyResult{}, ErrPreflightFailed
	}
	steps = appendCompleted(steps, "database")
	reportApplyStep(report, "database")
	redis, err := s.dependencies.CheckRedis(ctx, request.Redis)
	if err != nil || !redis.OK {
		return ApplyResult{}, ErrPreflightFailed
	}
	steps = appendCompleted(steps, "redis")
	reportApplyStep(report, "redis")

	if _, err := s.schemas.Up(ctx, request.Database); err != nil {
		return ApplyResult{}, errors.New("install database schema")
	}
	steps = appendCompleted(steps, "schema")
	reportApplyStep(report, "schema")
	assetReceipt, err := s.assets.Prepare(ctx, plan)
	if err != nil || !validDigest(assetReceipt.ArtifactHash) || !validDigest(assetReceipt.ManifestHash) {
		var rollbackErr error
		if assetReceipt != (AssetReceipt{}) {
			s.pending = &pendingRollback{assets: &assetReceipt}
			rollbackErr = s.rollback(ctx, nil, nil, &assetReceipt)
			if rollbackErr == nil {
				s.pending = nil
			}
		}
		return ApplyResult{}, applyFailure("assets", rollbackErr)
	}
	steps = appendCompleted(steps, "assets")
	reportApplyStep(report, "assets")
	s.pending = &pendingRollback{assets: &assetReceipt}
	identityReceipt, err := s.identity.Initialize(ctx, request.Database, request.Admin)
	if identityReceipt != (IdentityReceipt{}) {
		s.pending.identity = &identityReceipt
	}
	if err != nil {
		rollbackErr := s.rollback(ctx, nil, receiptPointer(identityReceipt), &assetReceipt)
		if rollbackErr == nil {
			s.pending = nil
		}
		return ApplyResult{}, applyFailure("identity", rollbackErr)
	}
	steps = appendCompleted(steps, "identity")
	reportApplyStep(report, "identity")
	environmentReceipt, err := s.environment.Publish(ctx, request, assetReceipt)
	if environmentReceipt != (EnvironmentReceipt{}) {
		s.pending.environment = &environmentReceipt
	}
	if err != nil {
		rollbackErr := s.rollback(ctx, receiptPointer(environmentReceipt), &identityReceipt, &assetReceipt)
		if rollbackErr == nil {
			s.pending = nil
		}
		return ApplyResult{}, applyFailure("environment", rollbackErr)
	}
	steps = appendCompleted(steps, "environment")
	reportApplyStep(report, "environment")

	installedAt := s.now().UTC()
	marker := installstate.Marker{
		SchemaVersion:    installstate.CurrentSchemaVersion,
		InstallerVersion: CurrentInstallerVersion,
		InstalledAt:      installedAt,
		SelectedUI:       ui,
		Mode:             mode,
		ArtifactHash:     assetReceipt.ArtifactHash,
		ManifestHash:     assetReceipt.ManifestHash,
	}
	if err := marker.Validate(); err != nil {
		rollbackErr := s.rollback(ctx, &environmentReceipt, &identityReceipt, &assetReceipt)
		if rollbackErr == nil {
			s.pending = nil
		}
		return ApplyResult{}, applyFailure("marker", rollbackErr)
	}
	s.pending.marker = &marker
	if err := s.markers.Create(ctx, marker); err != nil {
		rollbackErr := errors.Join(s.rollbackMarker(marker), s.rollback(ctx, &environmentReceipt, &identityReceipt, &assetReceipt))
		if rollbackErr == nil {
			s.pending = nil
		}
		return ApplyResult{}, applyFailure("lock", rollbackErr)
	}
	steps = appendCompleted(steps, "lock")
	reportApplyStep(report, "lock")
	s.pending = nil
	return ApplyResult{State: StateInstalled, SelectedUI: ui, Mode: mode, InstalledAt: installedAt, Steps: steps}, nil
}

// CanRollback reports whether a failed transaction left compensatable
// non-credential side effects. It is intentionally separate from marker
// status: a completed installation is never rolled back through this API.
func (s *ApplyService) CanRollback() bool {
	if s == nil {
		return false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.pending != nil
}

// Rollback compensates the last failed transaction after the caller has
// explicitly confirmed the operation at the HTTP boundary. Database schema
// migrations remain under the explicit migration runbook; this method only
// restores the receipts owned by the installer transaction.
func (s *ApplyService) Rollback(ctx context.Context) error {
	if s == nil {
		return ErrRollbackUnavailable
	}
	if !s.mutex.TryLock() {
		return ErrApplyBusy
	}
	defer s.mutex.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.pending == nil {
		return ErrRollbackUnavailable
	}
	pending := *s.pending
	if err := s.rollback(ctx, pending.environment, pending.identity, pending.assets); err != nil {
		return ErrRollbackUnavailable
	}
	if pending.marker != nil {
		if err := s.rollbackMarker(*pending.marker); err != nil {
			return ErrRollbackUnavailable
		}
	}
	s.pending = nil
	return nil
}

func reportApplyStep(report func(string), step string) {
	if report != nil {
		report(step)
	}
}

func (s *ApplyService) rollbackMarker(marker installstate.Marker) error {
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.markers.Remove(rollbackCtx, marker)
}

func (s *ApplyService) rollback(_ context.Context, environment *EnvironmentReceipt, identity *IdentityReceipt, assets *AssetReceipt) error {
	rollbackCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var rollbackErrors []error
	if environment != nil && *environment != (EnvironmentReceipt{}) {
		if err := s.environment.Rollback(rollbackCtx, *environment); err != nil {
			rollbackErrors = append(rollbackErrors, ErrApplyRollback)
		}
	}
	if identity != nil && *identity != (IdentityReceipt{}) {
		if err := s.identity.Rollback(rollbackCtx, *identity); err != nil {
			rollbackErrors = append(rollbackErrors, ErrApplyRollback)
		}
	}
	if assets != nil && *assets != (AssetReceipt{}) {
		if err := s.assets.Rollback(rollbackCtx, *assets); err != nil {
			rollbackErrors = append(rollbackErrors, ErrApplyRollback)
		}
	}
	return errors.Join(rollbackErrors...)
}

func applyFailure(stage string, rollbackErr error) error {
	if rollbackErr != nil {
		return fmt.Errorf("%w at %s: %w", ErrApplyFailed, stage, ErrApplyRollback)
	}
	return fmt.Errorf("%w at %s", ErrApplyFailed, stage)
}

func receiptPointer[T comparable](receipt T) *T {
	var zero T
	if receipt == zero {
		return nil
	}
	return &receipt
}

func validateApplyRequest(request ApplyRequest) (installstate.UI, installstate.Mode, error) {
	ui, mode, err := validatePlanRequest(PlanRequest{SelectedUI: request.SelectedUI, Mode: request.Mode})
	if err != nil || !request.ConfirmCleanup {
		return "", "", ErrInvalidApply
	}
	if err := validateDatabaseConnection(request.Database); err != nil {
		return "", "", ErrInvalidApply
	}
	if err := validateRedisConnection(request.Redis); err != nil {
		return "", "", ErrInvalidApply
	}
	localeMode := request.LocaleMode
	if localeMode == "" {
		localeMode = string(platformi18n.ModeSingle)
	}
	locale := request.Locale
	if locale == "" {
		locale = platformi18n.LocaleZhCN
	}
	localeConfig := platformi18n.Config{Mode: platformi18n.Mode(localeMode), DefaultLocale: locale, SupportedLocales: []string{platformi18n.LocaleZhCN, platformi18n.LocaleEnUS}}
	if err := localeConfig.Validate(); err != nil {
		return "", "", ErrInvalidApply
	}
	username := strings.TrimSpace(request.Admin.Username)
	passwordBytes := len([]byte(request.Admin.Password))
	if len(username) < 3 || len(username) > 64 || strings.ContainsAny(username, "\x00\r\n") || passwordBytes < 12 || passwordBytes > 128 || strings.ContainsAny(request.Admin.Password, "\x00\r\n") {
		return "", "", ErrInvalidApply
	}
	return ui, mode, nil
}

func appendCompleted(steps []ApplyStep, id string) []ApplyStep {
	return append(steps, ApplyStep{ID: id, Status: StepCompleted})
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
