package installplatform

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
)

type DatabasePinger interface {
	Ping(context.Context) error
	Close() error
}

type RedisPinger interface {
	Ping(context.Context) error
	Close() error
}

type DatabaseOpener func(gormdb.Options) (DatabasePinger, error)
type RedisOpener func(rediscache.Config) (RedisPinger, error)

type DependencyProbe struct {
	database DatabaseOpener
	redis    RedisOpener
	timeout  time.Duration
}

func NewDependencyProbe(database DatabaseOpener, redis RedisOpener) *DependencyProbe {
	return &DependencyProbe{database: database, redis: redis, timeout: 5 * time.Second}
}

func NewSystemDependencyProbe() *DependencyProbe {
	return NewDependencyProbe(
		func(options gormdb.Options) (DatabasePinger, error) { return gormdb.Open(options) },
		func(options rediscache.Config) (RedisPinger, error) { return rediscache.New(options) },
	)
}

func (p *DependencyProbe) CheckDatabase(ctx context.Context, request installer.DatabaseConnection) (installer.DependencyCheck, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return installer.DependencyCheck{}, err
	}
	options, err := databaseOptionsFromRequest(request)
	if err != nil {
		return installer.DependencyCheck{}, errors.New("invalid database connection configuration")
	}
	if p == nil || p.database == nil {
		return installer.DependencyCheck{}, errors.New("database connection probe is not configured")
	}
	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	connection, err := p.database(options)
	if err != nil {
		return installer.DependencyCheck{}, errors.New("database connection probe failed")
	}
	defer connection.Close()
	started := time.Now()
	if err := connection.Ping(probeCtx); err != nil {
		return installer.DependencyCheck{}, errors.New("database connection probe failed")
	}
	return installer.DependencyCheck{
		Kind:      "database",
		Driver:    options.Driver,
		Mode:      string(options.Mode),
		OK:        true,
		Reason:    "reachable",
		LatencyMS: elapsedMilliseconds(started),
	}, nil
}

func (p *DependencyProbe) CheckRedis(ctx context.Context, request installer.RedisConnection) (installer.DependencyCheck, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return installer.DependencyCheck{}, err
	}
	options, err := redisOptionsFromRequest(request)
	if err != nil {
		return installer.DependencyCheck{}, errors.New("invalid redis connection configuration")
	}
	if p == nil || p.redis == nil {
		return installer.DependencyCheck{}, errors.New("redis connection probe is not configured")
	}
	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	connection, err := p.redis(options)
	if err != nil {
		return installer.DependencyCheck{}, errors.New("redis connection probe failed")
	}
	defer connection.Close()
	started := time.Now()
	if err := connection.Ping(probeCtx); err != nil {
		return installer.DependencyCheck{}, errors.New("redis connection probe failed")
	}
	return installer.DependencyCheck{
		Kind:      "redis",
		Mode:      options.Mode,
		OK:        true,
		Reason:    "reachable",
		LatencyMS: elapsedMilliseconds(started),
	}, nil
}

func databaseOptionsFromRequest(request installer.DatabaseConnection) (gormdb.Options, error) {
	driver := gormdb.NormalizeDriver(request.Driver)
	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	if mode == "" {
		mode = string(gormdb.ModeSingle)
	}
	options := gormdb.Options{
		Driver:          driver,
		Mode:            gormdb.Mode(mode),
		ReadPolicy:      gormdb.ReadPolicyRandom,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 15 * time.Minute,
	}
	switch options.Mode {
	case gormdb.ModeSingle, gormdb.ModeClusterEndpoint:
		if strings.TrimSpace(request.DSN) != "" {
			options.DSN = strings.TrimSpace(request.DSN)
		} else {
			var err error
			options.DSN, err = structuredDatabaseDSN(request)
			if err != nil {
				return gormdb.Options{}, err
			}
		}
	case gormdb.ModeReadWrite:
		options.PrimaryDSN = strings.TrimSpace(request.PrimaryDSN)
		options.ReplicaDSNs = compactStrings(request.ReplicaDSNs)
	default:
		return gormdb.Options{}, errors.New("unsupported database mode")
	}
	if err := options.Validate(); err != nil {
		return gormdb.Options{}, errors.New("invalid database options")
	}
	return options, nil
}

func structuredDatabaseDSN(request installer.DatabaseConnection) (string, error) {
	if request.Port <= 0 || request.Port > 65535 || strings.TrimSpace(request.Host) == "" || strings.TrimSpace(request.Database) == "" {
		return "", errors.New("database host, port, and database are required")
	}
	if containsControl(request.Host) || containsControl(request.Database) || containsControl(request.Username) || containsControl(request.Password) {
		return "", errors.New("database connection fields contain invalid characters")
	}
	switch gormdb.NormalizeDriver(request.Driver) {
	case "mysql":
		config := mysqldriver.Config{
			User:                 request.Username,
			Passwd:               request.Password,
			Net:                  "tcp",
			Addr:                 net.JoinHostPort(request.Host, strconv.Itoa(request.Port)),
			DBName:               request.Database,
			ParseTime:            true,
			AllowNativePasswords: true,
		}
		return config.FormatDSN(), nil
	case "postgres":
		dsn := url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(request.Username, request.Password),
			Host:   net.JoinHostPort(request.Host, strconv.Itoa(request.Port)),
			Path:   "/" + request.Database,
		}
		query := url.Values{}
		if strings.TrimSpace(request.TLSMode) != "" {
			query.Set("sslmode", request.TLSMode)
		}
		dsn.RawQuery = query.Encode()
		return dsn.String(), nil
	default:
		return "", errors.New("unsupported database driver")
	}
}

func redisOptionsFromRequest(request installer.RedisConnection) (rediscache.Config, error) {
	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	if mode == "" {
		mode = rediscache.ModeSingle
	}
	options := rediscache.Config{
		Mode:       mode,
		Addr:       strings.TrimSpace(request.Addr),
		Addrs:      compactStrings(request.Addrs),
		MasterName: strings.TrimSpace(request.MasterName),
		Username:   request.Username,
		Password:   request.Password,
		DB:         request.DB,
		Namespace:  "app:v1",
	}
	if options.DB < 0 || containsControl(options.Addr) || containsControl(options.MasterName) {
		return rediscache.Config{}, errors.New("invalid redis connection fields")
	}
	return options, nil
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}

func containsControl(value string) bool {
	return strings.ContainsAny(value, "\r\n\x00")
}

func elapsedMilliseconds(start time.Time) int64 {
	value := time.Since(start).Milliseconds()
	if value < 1 {
		return 1
	}
	return value
}
