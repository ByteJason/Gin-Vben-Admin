package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	mailapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/mail"
	monitorapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/monitor"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/tasks"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

type iamReaderStub struct {
	users domain.UserPage
	roles []domain.Role
}

func (s iamReaderStub) ListUsersPage(context.Context, domain.UserListQuery) (domain.UserPage, error) {
	return s.users, nil
}

func (s iamReaderStub) ListRoles(context.Context) ([]domain.Role, error) {
	return append([]domain.Role(nil), s.roles...), nil
}

type slowTaskReader struct{}

func (slowTaskReader) List(context.Context) ([]tasks.TaskDefinition, error) {
	time.Sleep(200 * time.Millisecond)
	return nil, errors.New("secret task repository detail")
}

type monitorReaderStub struct{ overview monitorapp.Overview }

func (s monitorReaderStub) Overview(context.Context) (monitorapp.Overview, error) {
	return s.overview, nil
}

type tenantScopedMailReader struct{}

func (tenantScopedMailReader) ListAccounts(ctx context.Context) ([]mailapp.Account, error) {
	count, _ := tenantScopedMailCounts(ctx)
	return make([]mailapp.Account, count), nil
}

func (tenantScopedMailReader) ListMessages(ctx context.Context, _ mailapp.MessageFilter) (mailapp.MessagePage, error) {
	_, count := tenantScopedMailCounts(ctx)
	return mailapp.MessagePage{Total: count}, nil
}

func tenantScopedMailCounts(ctx context.Context) (int, int) {
	scope, _ := tenant.RequireContext(ctx)
	if scope.PlatformAdmin {
		return 99, 999
	}
	switch scope.TenantID + "/" + scope.Organization {
	case "tenant-a/org-a":
		return 1, 10
	case "tenant-b/org-b":
		return 2, 20
	default:
		return 0, 0
	}
}

func TestSummaryPreservesRealZeroAndDegradesOnlyTimedOutItem(t *testing.T) {
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	started := time.Now()
	summary, err := NewService(Config{
		IAM:     iamReaderStub{users: domain.UserPage{Total: 0}, roles: []domain.Role{{ID: "r1"}, {ID: "r2"}}},
		Tasks:   slowTaskReader{},
		Timeout: 20 * time.Millisecond,
		Clock:   func() time.Time { return time.Unix(10, 0) },
	}).Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 100*time.Millisecond {
		t.Fatalf("summary collectors were not independently bounded: %s", elapsed)
	}
	if summary.Counts.Users.Value == nil || *summary.Counts.Users.Value != 0 || summary.Counts.Users.Status != StatusOK {
		t.Fatalf("real zero user count = %#v", summary.Counts.Users)
	}
	if summary.Counts.Roles.Value == nil || *summary.Counts.Roles.Value != 2 {
		t.Fatalf("role count = %#v", summary.Counts.Roles)
	}
	if summary.Counts.Tasks.Value != nil || summary.Counts.Tasks.Status != StatusDegraded {
		t.Fatalf("timed-out task count = %#v", summary.Counts.Tasks)
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"value":0`) || strings.Contains(string(payload), "secret") {
		t.Fatalf("summary JSON = %s", payload)
	}
}

func TestSummaryExposesLightweightInstanceAndDependencyStatusWithoutPoolDetails(t *testing.T) {
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	summary, err := NewService(Config{Monitor: monitorReaderStub{overview: monitorapp.Overview{
		Scope: monitorapp.MetricScopeProcess, Version: "v1", UptimeSeconds: 0,
		Runtime:  monitorapp.RuntimeMetric{Status: monitorapp.StatusOK},
		Database: monitorapp.DatabaseMetric{Status: monitorapp.StatusDegraded},
		Redis:    monitorapp.RedisMetric{Status: monitorapp.StatusOK},
	}}}).Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Instance.UptimeSeconds == nil || *summary.Instance.UptimeSeconds != 0 || summary.Instance.Version == nil || *summary.Instance.Version != "v1" {
		t.Fatalf("instance summary = %#v", summary.Instance)
	}
	if summary.Health.Database.State != string(monitorapp.StatusDegraded) || summary.Health.Database.Status != StatusDegraded || summary.Health.Redis.State != string(monitorapp.StatusOK) {
		t.Fatalf("dependency summary = %#v", summary.Health)
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), `"pool"`) || strings.Contains(string(payload), `"keyspace"`) {
		t.Fatalf("lightweight overview leaked detailed monitor fields: %s", payload)
	}
}

func TestSummaryScopesPlatformAdministratorCountsToRequestedTenantOrganization(t *testing.T) {
	service := NewService(Config{Mail: tenantScopedMailReader{}})
	for _, tt := range []struct {
		name         string
		scope        tenant.Context
		wantAccounts int64
		wantMessages int64
	}{
		{name: "tenant a organization a", scope: tenant.Context{TenantID: "tenant-a", Organization: "org-a", PlatformAdmin: true}, wantAccounts: 1, wantMessages: 10},
		{name: "tenant b organization b", scope: tenant.Context{TenantID: "tenant-b", Organization: "org-b", PlatformAdmin: true}, wantAccounts: 2, wantMessages: 20},
	} {
		t.Run(tt.name, func(t *testing.T) {
			summary, err := service.Summary(tenant.WithContext(context.Background(), tt.scope))
			if err != nil {
				t.Fatal(err)
			}
			accounts := summary.Counts.MailAccounts.Value
			messages := summary.Counts.MailMessages.Value
			if accounts == nil || *accounts != tt.wantAccounts || messages == nil || *messages != tt.wantMessages {
				t.Fatalf("mail counts accounts=%v messages=%v", accounts, messages)
			}
		})
	}
}
