package installer

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// DatabaseConnection is the structured connection form used by the installer.
// DSN fields are accepted for advanced users but are never copied to a result
// or emitted in logs.
type DatabaseConnection struct {
	Driver      string   `json:"driver"`
	Mode        string   `json:"mode"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Database    string   `json:"database"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	DSN         string   `json:"dsn,omitempty"`
	PrimaryDSN  string   `json:"primaryDsn,omitempty"`
	ReplicaDSNs []string `json:"replicaDsns,omitempty"`
	TLSMode     string   `json:"tlsMode,omitempty"`
}

type RedisConnection struct {
	Mode       string   `json:"mode"`
	Addr       string   `json:"addr"`
	Addrs      []string `json:"addrs,omitempty"`
	MasterName string   `json:"masterName,omitempty"`
	Username   string   `json:"username,omitempty"`
	Password   string   `json:"password,omitempty"`
	DB         int      `json:"db,omitempty"`
}

// DependencyCheck is a credential-free result safe to return to the browser.
type DependencyCheck struct {
	Kind      string `json:"kind"`
	Driver    string `json:"driver,omitempty"`
	Mode      string `json:"mode"`
	OK        bool   `json:"ok"`
	Reason    string `json:"reason"`
	LatencyMS int64  `json:"latencyMs,omitempty"`
	Message   string `json:"message,omitempty"`
}

type DatabaseProbe interface {
	CheckDatabase(context.Context, DatabaseConnection) (DependencyCheck, error)
}

type RedisProbe interface {
	CheckRedis(context.Context, RedisConnection) (DependencyCheck, error)
}

type DependencyCheckService struct {
	database DatabaseProbe
	redis    RedisProbe
}

func NewDependencyCheckService(database DatabaseProbe, redis RedisProbe) *DependencyCheckService {
	return &DependencyCheckService{database: database, redis: redis}
}

func (s *DependencyCheckService) CheckDatabase(ctx context.Context, request DatabaseConnection) (DependencyCheck, error) {
	if err := contextError(ctx); err != nil {
		return DependencyCheck{}, err
	}
	if err := validateDatabaseConnection(request); err != nil {
		return DependencyCheck{}, err
	}
	if s == nil || s.database == nil {
		return DependencyCheck{}, errors.New("database connection probe is not configured")
	}
	result, err := s.database.CheckDatabase(ctx, request)
	if err != nil {
		return safeDependencyFailure("database", request.Driver, request.Mode), nil
	}
	return sanitizeDependencyResult(result, "database", request.Driver, request.Mode), nil
}

func (s *DependencyCheckService) CheckRedis(ctx context.Context, request RedisConnection) (DependencyCheck, error) {
	if err := contextError(ctx); err != nil {
		return DependencyCheck{}, err
	}
	if err := validateRedisConnection(request); err != nil {
		return DependencyCheck{}, err
	}
	if s == nil || s.redis == nil {
		return DependencyCheck{}, errors.New("redis connection probe is not configured")
	}
	result, err := s.redis.CheckRedis(ctx, request)
	if err != nil {
		return safeDependencyFailure("redis", "", request.Mode), nil
	}
	return sanitizeDependencyResult(result, "redis", "", request.Mode), nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func validateDatabaseConnection(request DatabaseConnection) error {
	driver := strings.ToLower(strings.TrimSpace(request.Driver))
	if driver != "mysql" && driver != "postgres" {
		return fmt.Errorf("unsupported database driver %q", request.Driver)
	}
	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	if mode == "" {
		mode = "single"
	}
	switch mode {
	case "single", "cluster_endpoint":
		if strings.TrimSpace(request.DSN) == "" && (strings.TrimSpace(request.Host) == "" || request.Port <= 0 || strings.TrimSpace(request.Database) == "") {
			return errors.New("database host, port, and database are required")
		}
	case "read_write":
		if strings.TrimSpace(request.PrimaryDSN) == "" || len(nonEmptyStrings(request.ReplicaDSNs)) == 0 {
			return errors.New("primary and replica database endpoints are required")
		}
	default:
		return fmt.Errorf("unsupported database mode %q", request.Mode)
	}
	return nil
}

func validateRedisConnection(request RedisConnection) error {
	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	if mode == "" {
		mode = "single"
	}
	switch mode {
	case "single":
		if strings.TrimSpace(request.Addr) == "" {
			return errors.New("redis address is required")
		}
	case "sentinel":
		if len(nonEmptyStrings(request.Addrs)) == 0 || strings.TrimSpace(request.MasterName) == "" {
			return errors.New("redis sentinel addresses and master are required")
		}
	case "cluster":
		if len(nonEmptyStrings(request.Addrs)) < 2 {
			return errors.New("at least two redis cluster addresses are required")
		}
		if request.DB != 0 {
			return errors.New("redis cluster database must be zero")
		}
	default:
		return fmt.Errorf("unsupported redis mode %q", request.Mode)
	}
	return nil
}

func safeDependencyFailure(kind, driver, mode string) DependencyCheck {
	return DependencyCheck{Kind: kind, Driver: driver, Mode: mode, OK: false, Reason: "connection_failed"}
}

func sanitizeDependencyResult(result DependencyCheck, kind, driver, mode string) DependencyCheck {
	result.Kind = kind
	result.Driver = driver
	result.Mode = mode
	result.Message = ""
	if result.Reason == "" {
		if result.OK {
			result.Reason = "reachable"
		} else {
			result.Reason = "connection_failed"
		}
	}
	return result
}

func nonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}
