package installer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	installstate "example.com/gin-vben-admin/server/internal/domain/installstate"
	platformi18n "example.com/gin-vben-admin/server/internal/platform/i18n"
)

func TestApplyServiceCompletesInstallationInSafeOrder(t *testing.T) {
	t.Parallel()

	calls := make([]string, 0, 9)
	markers := &applyMarkerStub{calls: &calls}
	planner := &applyPlanStub{calls: &calls, plan: Plan{
		SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded,
		CanCleanup: true, CanBuild: true, CanWriteEnv: true,
	}}
	dependencies := &applyDependencyStub{calls: &calls}
	schemas := &applySchemaStub{calls: &calls}
	assets := &applyAssetStub{calls: &calls, receipt: AssetReceipt{
		ArtifactHash: strings.Repeat("a", 64), ManifestHash: strings.Repeat("b", 64),
	}}
	identity := &applyIdentityStub{calls: &calls}
	environment := &applyEnvironmentStub{calls: &calls}
	completedAt := time.Date(2026, time.August, 21, 13, 30, 0, 0, time.UTC)
	service := NewApplyService(markers, planner, dependencies, schemas, assets, identity, environment, func() time.Time { return completedAt })

	result, err := service.Apply(context.Background(), ApplyRequest{
		SelectedUI: "antd",
		Mode:       "embedded",
		Database: DatabaseConnection{
			Driver: "mysql", Mode: "single", Host: "127.0.0.1", Port: 3306,
			Database: "app", Username: "app", Password: "database-secret",
		},
		Redis:          RedisConnection{Mode: "single", Addr: "127.0.0.1:6379", Password: "redis-secret"},
		Admin:          AdminAccount{Username: "admin", Password: "initial-password-123"},
		ConfirmCleanup: true,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantCalls := []string{"marker.load", "plan", "database.check", "redis.check", "schema.up", "assets.prepare", "identity.initialize", "environment.publish", "marker.create"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
	if result.State != StateInstalled || result.SelectedUI != installstate.UIAntd || result.Mode != installstate.ModeEmbedded || result.InstalledAt != completedAt {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Steps) != 8 || result.Steps[len(result.Steps)-1].ID != "lock" {
		t.Fatalf("steps = %#v", result.Steps)
	}
	if markers.created.SelectedUI != installstate.UIAntd || markers.created.Mode != installstate.ModeEmbedded || markers.created.InstalledAt != completedAt {
		t.Fatalf("created marker = %#v", markers.created)
	}
}

func TestValidateApplyRequestAcceptsOnlyBundledLocalePolicy(t *testing.T) {
	valid := validApplyRequest()
	valid.Locale = platformi18n.LocaleEnUS
	valid.LocaleMode = string(platformi18n.ModeMulti)
	if _, _, err := validateApplyRequest(valid); err != nil {
		t.Fatalf("validate bundled locale policy error = %v", err)
	}
	for name, edit := range map[string]func(*ApplyRequest){
		"unknown locale": func(request *ApplyRequest) { request.Locale = "fr-FR" },
		"unknown mode":   func(request *ApplyRequest) { request.LocaleMode = "fallback" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			edit(&candidate)
			if _, _, err := validateApplyRequest(candidate); !errors.Is(err, ErrInvalidApply) {
				t.Fatalf("validate error = %v, want ErrInvalidApply", err)
			}
		})
	}
}

func TestApplyServiceRollsBackCompletedSideEffectsInReverseOrder(t *testing.T) {
	t.Parallel()

	calls := make([]string, 0, 12)
	markers := &applyMarkerStub{calls: &calls, createErr: errors.New("marker fixture failure")}
	planner := &applyPlanStub{calls: &calls, plan: Plan{
		SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded,
		CanCleanup: true, CanBuild: true, CanWriteEnv: true,
	}}
	service := NewApplyService(
		markers,
		planner,
		&applyDependencyStub{calls: &calls},
		&applySchemaStub{calls: &calls},
		&applyAssetStub{calls: &calls, receipt: AssetReceipt{ArtifactHash: strings.Repeat("a", 64), ManifestHash: strings.Repeat("b", 64)}},
		&applyIdentityStub{calls: &calls},
		&applyEnvironmentStub{calls: &calls},
		func() time.Time { return time.Date(2026, time.August, 21, 14, 0, 0, 0, time.UTC) },
	)

	_, err := service.Apply(context.Background(), validApplyRequest())
	if !errors.Is(err, ErrApplyFailed) {
		t.Fatalf("Apply() error = %v, want ErrApplyFailed", err)
	}
	wantSuffix := []string{"marker.create", "marker.rollback", "environment.rollback", "identity.rollback", "assets.rollback"}
	if len(calls) < len(wantSuffix) || !reflect.DeepEqual(calls[len(calls)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("call suffix = %#v, want %#v; all=%#v", calls, wantSuffix, calls)
	}
}

func TestApplyServiceKeepsFailedCompensationForExplicitRollback(t *testing.T) {
	t.Parallel()

	environment := &applyEnvironmentStub{calls: new([]string), rollbackErr: errors.New("temporary environment rollback failure")}
	service := NewApplyService(
		&applyMarkerStub{calls: new([]string), createErr: errors.New("marker fixture failure")},
		&applyPlanStub{calls: new([]string), plan: Plan{SelectedUI: installstate.UIAntd, Mode: installstate.ModeEmbedded, CanCleanup: true, CanBuild: true, CanWriteEnv: true}},
		&applyDependencyStub{calls: new([]string)},
		&applySchemaStub{calls: new([]string)},
		&applyAssetStub{calls: new([]string), receipt: AssetReceipt{ArtifactHash: strings.Repeat("a", 64), ManifestHash: strings.Repeat("b", 64)}},
		&applyIdentityStub{calls: new([]string)},
		environment,
		time.Now,
	)
	if _, err := service.Apply(context.Background(), validApplyRequest()); !errors.Is(err, ErrApplyRollback) {
		t.Fatalf("Apply() error = %v, want rollback sentinel", err)
	}
	if !service.CanRollback() {
		t.Fatal("CanRollback() = false, want pending compensation")
	}
	environment.rollbackErr = nil
	if err := service.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if service.CanRollback() {
		t.Fatal("CanRollback() = true after explicit rollback")
	}
}

func validApplyRequest() ApplyRequest {
	return ApplyRequest{
		SelectedUI: "antd", Mode: "embedded", ConfirmCleanup: true,
		Database: DatabaseConnection{Driver: "mysql", Mode: "single", DSN: "user:secret@tcp(db:3306)/app"},
		Redis:    RedisConnection{Mode: "single", Addr: "redis:6379"},
		Admin:    AdminAccount{Username: "admin", Password: "initial-password-123"},
	}
}

type applyMarkerStub struct {
	calls     *[]string
	created   installstate.Marker
	createErr error
}

func (s *applyMarkerStub) Load(context.Context) (installstate.Marker, bool, error) {
	*s.calls = append(*s.calls, "marker.load")
	return installstate.Marker{}, false, nil
}

func (s *applyMarkerStub) Create(_ context.Context, marker installstate.Marker) error {
	*s.calls = append(*s.calls, "marker.create")
	s.created = marker
	return s.createErr
}

func (s *applyMarkerStub) Remove(context.Context, installstate.Marker) error {
	*s.calls = append(*s.calls, "marker.rollback")
	return nil
}

type applyPlanStub struct {
	calls *[]string
	plan  Plan
}

func (s *applyPlanStub) Plan(context.Context, PlanRequest) (Plan, error) {
	*s.calls = append(*s.calls, "plan")
	return s.plan, nil
}

type applyDependencyStub struct{ calls *[]string }

func (s *applyDependencyStub) CheckDatabase(context.Context, DatabaseConnection) (DependencyCheck, error) {
	*s.calls = append(*s.calls, "database.check")
	return DependencyCheck{Kind: "database", Driver: "mysql", Mode: "single", OK: true, Reason: "reachable"}, nil
}

func (s *applyDependencyStub) CheckRedis(context.Context, RedisConnection) (DependencyCheck, error) {
	*s.calls = append(*s.calls, "redis.check")
	return DependencyCheck{Kind: "redis", Mode: "single", OK: true, Reason: "reachable"}, nil
}

type applySchemaStub struct{ calls *[]string }

func (s *applySchemaStub) Up(context.Context, DatabaseConnection) (SchemaReceipt, error) {
	*s.calls = append(*s.calls, "schema.up")
	return SchemaReceipt{Version: 4}, nil
}

type applyAssetStub struct {
	calls   *[]string
	receipt AssetReceipt
}

func (s *applyAssetStub) Prepare(context.Context, Plan) (AssetReceipt, error) {
	*s.calls = append(*s.calls, "assets.prepare")
	return s.receipt, nil
}

func (s *applyAssetStub) Rollback(context.Context, AssetReceipt) error {
	*s.calls = append(*s.calls, "assets.rollback")
	return nil
}

type applyIdentityStub struct{ calls *[]string }

func (s *applyIdentityStub) Initialize(context.Context, DatabaseConnection, AdminAccount) (IdentityReceipt, error) {
	*s.calls = append(*s.calls, "identity.initialize")
	return IdentityReceipt{Reference: "installation-1"}, nil
}

func (s *applyIdentityStub) Rollback(context.Context, IdentityReceipt) error {
	*s.calls = append(*s.calls, "identity.rollback")
	return nil
}

type applyEnvironmentStub struct {
	calls       *[]string
	rollbackErr error
}

func (s *applyEnvironmentStub) Publish(context.Context, ApplyRequest, AssetReceipt) (EnvironmentReceipt, error) {
	*s.calls = append(*s.calls, "environment.publish")
	return EnvironmentReceipt{Digest: strings.Repeat("c", 64)}, nil
}

func (s *applyEnvironmentStub) Rollback(context.Context, EnvironmentReceipt) error {
	*s.calls = append(*s.calls, "environment.rollback")
	return s.rollbackErr
}
