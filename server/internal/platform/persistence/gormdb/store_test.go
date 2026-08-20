package gormdb

import (
	"strings"
	"testing"
	"time"
)

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
