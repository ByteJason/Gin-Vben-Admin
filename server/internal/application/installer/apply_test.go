package installer

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	installstate "example.com/gin-vben-admin/server/internal/domain/installstate"
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

type applyMarkerStub struct {
	calls   *[]string
	created installstate.Marker
}

func (s *applyMarkerStub) Load(context.Context) (installstate.Marker, bool, error) {
	*s.calls = append(*s.calls, "marker.load")
	return installstate.Marker{}, false, nil
}

func (s *applyMarkerStub) Create(_ context.Context, marker installstate.Marker) error {
	*s.calls = append(*s.calls, "marker.create")
	s.created = marker
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

type applyIdentityStub struct{ calls *[]string }

func (s *applyIdentityStub) Initialize(context.Context, DatabaseConnection, AdminAccount) (IdentityReceipt, error) {
	*s.calls = append(*s.calls, "identity.initialize")
	return IdentityReceipt{Reference: "installation-1"}, nil
}

type applyEnvironmentStub struct{ calls *[]string }

func (s *applyEnvironmentStub) Publish(context.Context, ApplyRequest, AssetReceipt) (EnvironmentReceipt, error) {
	*s.calls = append(*s.calls, "environment.publish")
	return EnvironmentReceipt{Digest: strings.Repeat("c", 64)}, nil
}
