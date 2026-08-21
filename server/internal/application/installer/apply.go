package installer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	installstate "example.com/gin-vben-admin/server/internal/domain/installstate"
)

var (
	ErrAlreadyInstalled = errors.New("application is already installed")
	ErrApplyBusy        = errors.New("installation is already running")
	ErrInvalidApply     = errors.New("installation request is invalid")
	ErrPreflightFailed  = errors.New("installation preflight failed")
)

type AdminAccount struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ApplyRequest struct {
	SelectedUI     string             `json:"selectedUi"`
	Mode           string             `json:"mode"`
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
	Version uint
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
}

type IdentityInstaller interface {
	Initialize(context.Context, DatabaseConnection, AdminAccount) (IdentityReceipt, error)
}

type EnvironmentInstaller interface {
	Publish(context.Context, ApplyRequest, AssetReceipt) (EnvironmentReceipt, error)
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

	database, err := s.dependencies.CheckDatabase(ctx, request.Database)
	if err != nil || !database.OK {
		return ApplyResult{}, ErrPreflightFailed
	}
	steps = appendCompleted(steps, "database")
	redis, err := s.dependencies.CheckRedis(ctx, request.Redis)
	if err != nil || !redis.OK {
		return ApplyResult{}, ErrPreflightFailed
	}
	steps = appendCompleted(steps, "redis")

	if _, err := s.schemas.Up(ctx, request.Database); err != nil {
		return ApplyResult{}, errors.New("install database schema")
	}
	steps = appendCompleted(steps, "schema")
	assetReceipt, err := s.assets.Prepare(ctx, plan)
	if err != nil || !validDigest(assetReceipt.ArtifactHash) || !validDigest(assetReceipt.ManifestHash) {
		return ApplyResult{}, errors.New("prepare installation assets")
	}
	steps = appendCompleted(steps, "assets")
	if _, err := s.identity.Initialize(ctx, request.Database, request.Admin); err != nil {
		return ApplyResult{}, errors.New("initialize administrator")
	}
	steps = appendCompleted(steps, "identity")
	if _, err := s.environment.Publish(ctx, request, assetReceipt); err != nil {
		return ApplyResult{}, errors.New("publish installation configuration")
	}
	steps = appendCompleted(steps, "environment")

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
		return ApplyResult{}, errors.New("validate installation marker")
	}
	if err := s.markers.Create(ctx, marker); err != nil {
		return ApplyResult{}, errors.New("create installation lock")
	}
	steps = appendCompleted(steps, "lock")
	return ApplyResult{State: StateInstalled, SelectedUI: ui, Mode: mode, InstalledAt: installedAt, Steps: steps}, nil
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
