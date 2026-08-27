package gormdb

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Mode string

const (
	ModeSingle          Mode = "single"
	ModeReadWrite       Mode = "read_write"
	ModeClusterEndpoint Mode = "cluster_endpoint"
)

type ReadPolicy string

const (
	ReadPolicyRandom     ReadPolicy = "random"
	ReadPolicyRoundRobin ReadPolicy = "round_robin"
)

const (
	// DriverMySQL and DriverPostgres are the canonical runtime driver names.
	DriverMySQL    = "mysql"
	DriverPostgres = "postgres"
)

type Options struct {
	Driver          string
	Mode            Mode
	DSN             string
	PrimaryDSN      string
	ReplicaDSNs     []string
	ReadPolicy      ReadPolicy
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

func (options *Options) applyDefaults() {
	options.Driver = NormalizeDriver(options.Driver)
	if options.Mode == "" {
		options.Mode = ModeSingle
	}
	if options.ReadPolicy == "" {
		options.ReadPolicy = ReadPolicyRandom
	}
	if options.MaxOpenConns == 0 {
		options.MaxOpenConns = 20
	}
	if options.MaxIdleConns == 0 {
		options.MaxIdleConns = 10
	}
	if options.ConnMaxLifetime == 0 {
		options.ConnMaxLifetime = 30 * time.Minute
	}
	if options.ConnMaxIdleTime == 0 {
		options.ConnMaxIdleTime = 5 * time.Minute
	}
}

func (options Options) Validate() error {
	options.Driver = NormalizeDriver(options.Driver)
	if options.Driver != DriverMySQL && options.Driver != DriverPostgres {
		return fmt.Errorf("database driver must be mysql or postgres")
	}
	if options.MaxOpenConns <= 0 || options.MaxIdleConns <= 0 || options.MaxIdleConns > options.MaxOpenConns {
		return errors.New("database connection pool bounds are invalid")
	}
	if options.ConnMaxLifetime <= 0 || options.ConnMaxIdleTime <= 0 {
		return errors.New("database connection pool durations must be positive")
	}
	if options.ReadPolicy != ReadPolicyRandom && options.ReadPolicy != ReadPolicyRoundRobin {
		return errors.New("database read policy must be random or round_robin")
	}

	switch options.Mode {
	case ModeSingle, ModeClusterEndpoint:
		if strings.TrimSpace(options.DSN) == "" {
			return fmt.Errorf("database dsn is required for %s mode", options.Mode)
		}
	case ModeReadWrite:
		if strings.TrimSpace(options.PrimaryDSN) == "" {
			return errors.New("database primary dsn is required for read_write mode")
		}
		if len(options.ReplicaDSNs) == 0 {
			return errors.New("at least one database replica dsn is required for read_write mode")
		}
		for _, dsn := range options.ReplicaDSNs {
			if strings.TrimSpace(dsn) == "" {
				return errors.New("database replica dsn must not be empty")
			}
		}
	default:
		return fmt.Errorf("unsupported database mode %q", options.Mode)
	}
	return nil
}

// NormalizeDriver maps the names accepted by configuration and the command
// line to the two canonical GORM dialect names.  In particular, pgsql is a
// common spelling in existing deployments and is intentionally equivalent to
// postgres.
func NormalizeDriver(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "pgsql", "postgresql", "pg":
		return DriverPostgres
	case DriverMySQL:
		return DriverMySQL
	default:
		return strings.ToLower(strings.TrimSpace(driver))
	}
}
