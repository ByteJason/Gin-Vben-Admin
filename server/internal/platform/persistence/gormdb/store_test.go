package gormdb

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestAggregatePoolStatsPreservesInUseWaitsAndZeroMaximum(t *testing.T) {
	got := aggregatePoolStats([]sql.DBStats{
		{OpenConnections: 3, InUse: 2, Idle: 1, MaxOpenConnections: 0, WaitCount: 4, WaitDuration: 2 * time.Millisecond, MaxIdleClosed: 5},
		{OpenConnections: 7, InUse: 3, Idle: 4, MaxOpenConnections: 11, WaitCount: 6, WaitDuration: 3 * time.Millisecond, MaxIdleTimeClosed: 7, MaxLifetimeClosed: 8},
	})
	if got.Open != 10 || got.InUse != 5 || got.Idle != 5 || got.Max != 0 {
		t.Fatalf("pool totals = %#v", got)
	}
	if got.WaitCount != 10 || got.WaitDurationMS != 5 || got.MaxIdleClosed != 5 || got.MaxIdleTimeClosed != 7 || got.MaxLifetimeClosed != 8 {
		t.Fatalf("pool wait/closure counters = %#v", got)
	}
}

func TestDatabaseRuntimeStatsExposeOnlyDriverModeAndPool(t *testing.T) {
	store, err := Open(Options{Driver: "postgres", Mode: ModeSingle, DSN: "host=127.0.0.1 port=1 user=fixture dbname=fixture sslmode=disable"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	stats, err := store.DatabaseRuntimeStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !stats.DriverAvailable || stats.Driver != "postgres" || !stats.ModeAvailable || stats.Mode != string(ModeSingle) || !stats.PoolAvailable {
		t.Fatalf("runtime identity/stats = %#v", stats)
	}
}

func TestOptionsValidateSupportedTopologies(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		wantErr string
	}{
		{
			name: "single",
			options: Options{
				Driver: "mysql", Mode: ModeSingle, DSN: "user:secret@tcp(database:3306)/app",
			},
		},
		{
			name: "read write",
			options: Options{
				Driver: "postgres", Mode: ModeReadWrite, PrimaryDSN: "primary", ReplicaDSNs: []string{"replica"},
				ReadPolicy: ReadPolicyRoundRobin,
			},
		},
		{
			name: "cluster endpoint",
			options: Options{
				Driver: "postgres", Mode: ModeClusterEndpoint, DSN: "cluster-endpoint",
			},
		},
		{
			name:    "read write needs replica",
			options: Options{Driver: "mysql", Mode: ModeReadWrite, PrimaryDSN: "primary"},
			wantErr: "replica",
		},
		{
			name:    "reject driver",
			options: Options{Driver: "sqlite", Mode: ModeSingle, DSN: "local.db"},
			wantErr: "driver",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.options.applyDefaults()
			err := tt.options.Validate()
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestOptionsDefaultsBoundTheConnectionPool(t *testing.T) {
	options := Options{Driver: "mysql", Mode: ModeSingle, DSN: "database"}
	options.applyDefaults()

	if options.MaxOpenConns <= 0 || options.MaxIdleConns <= 0 || options.MaxIdleConns > options.MaxOpenConns {
		t.Fatalf("invalid default pool bounds: open=%d idle=%d", options.MaxOpenConns, options.MaxIdleConns)
	}
	for name, duration := range map[string]time.Duration{
		"connection lifetime":  options.ConnMaxLifetime,
		"connection idle time": options.ConnMaxIdleTime,
	} {
		if duration <= 0 {
			t.Fatalf("%s = %s, want positive", name, duration)
		}
	}
}

func TestOpenDoesNotProbeNetworkUntilPing(t *testing.T) {
	store, err := Open(Options{
		Driver: "mysql",
		Mode:   ModeSingle,
		DSN:    "user:secret@tcp(127.0.0.1:1)/test?parseTime=true",
	})
	if err != nil {
		t.Fatalf("Open() error = %v; constructor must not probe the endpoint", err)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := store.Ping(ctx); err == nil {
		t.Fatal("Ping() error = nil, want unavailable endpoint error")
	}
}

func TestOpenPublishesCanonicalGlobalGORMDatabase(t *testing.T) {
	store, err := Open(Options{
		Driver: "pgsql",
		Mode:   ModeSingle,
		DSN:    "host=127.0.0.1 port=1 user=fixture dbname=fixture sslmode=disable",
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if GetDB() != store.DB() {
		t.Fatal("global GORM database does not match the configured store")
	}
	if GetDriver() != DriverPostgres || store.Driver() != DriverPostgres {
		t.Fatalf("global/store drivers = %q/%q, want postgres", GetDriver(), store.Driver())
	}
	if !store.DB().DisableForeignKeyConstraintWhenMigrating {
		t.Fatal("GORM migration foreign-key creation is enabled")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if GetDB() != nil || GetDriver() != "" {
		t.Fatalf("global database state survived close: db=%p driver=%q", GetDB(), GetDriver())
	}
}

func TestClosingTemporaryStoreRestoresPreviousGlobalDatabase(t *testing.T) {
	first, err := Open(Options{
		Driver: "mysql", Mode: ModeSingle,
		DSN: "fixture@tcp(127.0.0.1:1)/first?parseTime=true",
	})
	if err != nil {
		t.Fatalf("open first store: %v", err)
	}
	second, err := Open(Options{
		Driver: "postgres", Mode: ModeSingle,
		DSN: "host=127.0.0.1 port=1 user=fixture dbname=second sslmode=disable",
	})
	if err != nil {
		_ = first.Close()
		t.Fatalf("open second store: %v", err)
	}
	if GetDB() != second.DB() {
		t.Fatal("temporary store was not published")
	}
	if err := second.Close(); err != nil {
		_ = first.Close()
		t.Fatalf("close second store: %v", err)
	}
	if GetDB() != first.DB() || GetDriver() != DriverMySQL {
		t.Fatalf("previous global store was not restored: db=%p driver=%q", GetDB(), GetDriver())
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}
}

func TestClosingNestedStoresOutOfOrderDoesNotRestoreClosedHandle(t *testing.T) {
	first, err := Open(Options{
		Driver: "mysql", Mode: ModeSingle,
		DSN: "fixture@tcp(127.0.0.1:1)/first?parseTime=true",
	})
	if err != nil {
		t.Fatalf("open first store: %v", err)
	}
	second, err := Open(Options{
		Driver: "postgres", Mode: ModeSingle,
		DSN: "host=127.0.0.1 port=1 user=fixture dbname=second sslmode=disable",
	})
	if err != nil {
		_ = first.Close()
		t.Fatalf("open second store: %v", err)
	}
	third, err := Open(Options{
		Driver: "mysql", Mode: ModeSingle,
		DSN: "fixture@tcp(127.0.0.1:1)/third?parseTime=true",
	})
	if err != nil {
		_ = second.Close()
		_ = first.Close()
		t.Fatalf("open third store: %v", err)
	}

	// Close older stores first. The current global handle must remain the
	// newest live store, and closing the last store must clear rather than
	// resurrect the already-closed first handle.
	if err := first.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}
	if err := second.Close(); err != nil {
		_ = third.Close()
		t.Fatalf("close second store: %v", err)
	}
	if globalDB := GetDB(); globalDB != third.DB() {
		t.Fatalf("global database after out-of-order close = %p, want third %p", globalDB, third.DB())
	}
	if err := third.Close(); err != nil {
		t.Fatalf("close third store: %v", err)
	}
	if GetDB() != nil || GetDriver() != "" {
		t.Fatalf("closed nested stores left global state: db=%p driver=%q", GetDB(), GetDriver())
	}
}
