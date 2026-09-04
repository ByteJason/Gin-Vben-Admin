// Command migrate creates, drops, or inspects the single fresh-install GORM
// schema registered from the shared persistence models. Future versioned
// upgrades remain explicit CLI operations rather than startup-time AutoMigrate.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/migration"
	adminmigrations "github.com/ByteJason/Gin-Vben-Admin/server/migrations/versions/admin"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, output io.Writer) int {
	command, err := parseCommand(args)
	if err != nil {
		writeFailure(output, "invalid_command")
		return 2
	}

	cfg, err := config.Load(command.configPath)
	if err != nil {
		writeFailure(output, "configuration")
		return 1
	}
	dsn, err := cfg.Database.MigrationDSN()
	if err != nil {
		writeFailure(output, "database_configuration")
		return 1
	}

	runner, err := migration.New(cfg.Database.Driver, dsn)
	if err != nil {
		writeFailure(output, "initialize")
		return 1
	}
	defer func() { _ = runner.Close() }()

	var cleanupReport *adminmigrations.CleanupReport
	var redisClient *rediscache.Client
	switch command.action {
	case actionUp:
		err = runner.Up()
	case actionDown:
		err = runner.Down(command.steps)
	case actionStatus:
		// Status is read below without applying a migration.
	case actionSettingsMailCleanup:
		// The cleanup action is explicit and never runs during a normal `up`.
		// When Redis is enabled in the same deployment config, pass the real
		// namespaced client so retired settings-cache entries are removed after
		// the database transaction. With Redis disabled the database cleanup is
		// still fully executable and remains safe to retry.
		var cleaner adminmigrations.LegacyMailCacheCleaner
		if cfg.Redis.Enabled {
			options, optionsErr := migrationRedisOptions(cfg.Redis)
			if optionsErr != nil {
				writeFailure(output, "cache_configuration")
				return 1
			}
			redisClient, err = rediscache.New(options)
			if err != nil {
				writeFailure(output, "cache_initialize")
				return 1
			}
			defer func() { _ = redisClient.Close() }()
			cleaner = redisClient
		}
		report, cleanupErr := runner.ApplySettingsMailCleanup(context.Background(), cleaner)
		cleanupReport = &report
		err = cleanupErr
	}
	if err != nil {
		if cleanupReport != nil {
			writeCleanupFailure(output, "operation", *cleanupReport)
			return 1
		}
		writeFailure(output, "operation")
		return 1
	}

	status, err := runner.Status()
	if err != nil {
		if cleanupReport != nil {
			writeCleanupFailure(output, "status", *cleanupReport)
			return 1
		}
		writeFailure(output, "status")
		return 1
	}
	writeSuccess(output, command.action, cfg.Database.Driver, status)
	if cleanupReport != nil {
		writeCleanupReport(output, *cleanupReport)
	}
	return 0
}

func migrationRedisOptions(cfg config.RedisConfig) (rediscache.Config, error) {
	options := rediscache.Config{
		Mode:         cfg.Mode,
		Addr:         cfg.Addr,
		Addrs:        append([]string(nil), cfg.Addrs...),
		MasterName:   cfg.MasterName,
		AddressMap:   cloneStringMap(cfg.AddressMap),
		Username:     cfg.Username,
		Password:     cfg.Password,
		DB:           cfg.DB,
		Namespace:    cfg.Namespace,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}
	if options.Mode == "" {
		options.Mode = rediscache.ModeSingle
	}
	return options, nil
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func writeSuccess(output io.Writer, action action, driver string, status migration.Status) {
	_, _ = fmt.Fprintf(output,
		"MIGRATION_ACTION=%s\nMIGRATION_DRIVER=%s\nMIGRATION_VERSION=%d\nMIGRATION_DIRTY=%t\nMIGRATION_APPLIED=%t\nMIGRATION_STATUS=OK\n",
		strings.ToUpper(string(action)),
		strings.ToUpper(strings.TrimSpace(driver)),
		status.Version,
		status.Dirty,
		status.Applied,
	)
}

func writeFailure(output io.Writer, category string) {
	_, _ = fmt.Fprintf(output, "MIGRATION_STATUS=ERROR\nMIGRATION_ERROR=%s\n", category)
}

func writeCleanupReport(output io.Writer, report adminmigrations.CleanupReport) {
	_, _ = fmt.Fprintf(output,
		"MIGRATION_CLEANUP_VERSION=%s\nMIGRATION_CLEANUP_SETTING_ROWS=%d\nMIGRATION_CLEANUP_AUDIT_ROWS=%d\nMIGRATION_CLEANUP_PERMISSION_ROWS=%d\nMIGRATION_CLEANUP_POLICY_ROWS=%d\nMIGRATION_CLEANUP_CACHE_CLEANED=%t\n",
		adminmigrations.V003Version,
		report.SettingRows, report.AuditRows, report.PermissionRows, report.PolicyRows, report.CacheCleaned,
	)
}

func writeCleanupFailure(output io.Writer, category string, report adminmigrations.CleanupReport) {
	writeFailure(output, category)
	writeCleanupReport(output, report)
}
