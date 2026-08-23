package monitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

type probeFunc struct{ err error }

func (p probeFunc) Ping(context.Context) error { return p.err }

type statsProbe struct{ probeFunc }

func (statsProbe) RuntimeStats(context.Context) (int, int, int, int64, error) {
	return 7, 3, 11, 42, nil
}

func TestServiceIncludesSafeDependencyPoolStats(t *testing.T) {
	scope, err := tenant.NewContext("tenant-a", "", true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenant.WithContext(context.Background(), scope)
	overview, err := NewService(Config{Database: statsProbe{probeFunc{}}, Redis: statsProbe{probeFunc{}}}).Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for name, metric := range map[string]DependencyMetric{"database": overview.Database, "redis": overview.Redis} {
		if metric.PoolOpen != 7 || metric.PoolIdle != 3 || metric.PoolMax != 11 || metric.Keyspace != 42 {
			t.Fatalf("%s stats = %#v", name, metric)
		}
	}
}

func TestServiceReturnsDegradedDependencyWithoutSecrets(t *testing.T) {
	scope, err := tenant.NewContext("tenant-a", "", true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenant.WithContext(context.Background(), scope)
	svc := NewService(Config{Version: "fixture", Start: time.Unix(1, 0), Clock: func() time.Time { return time.Unix(2, 0) }, Database: probeFunc{err: errors.New("dsn/password must not escape")}, Redis: probeFunc{err: errors.New("token must not escape")}})
	overview, err := svc.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Database.Status != StatusDegraded || overview.Redis.Status != StatusDegraded {
		t.Fatalf("dependency statuses = %#v", overview)
	}
	if overview.Database.Message == "" || overview.Redis.Message == "" {
		t.Fatalf("degraded messages missing: %#v", overview)
	}
	if overview.Database.Message == "dsn/password must not escape" || overview.Redis.Message == "token must not escape" {
		t.Fatalf("raw dependency error leaked: %#v", overview)
	}
}

func TestServiceRequiresPlatformAdminScope(t *testing.T) {
	scope, err := tenant.NewContext("tenant-a", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewService(Config{}).Overview(tenant.WithContext(context.Background(), scope))
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("Overview() error = %v, want ErrPermissionDenied", err)
	}
}
