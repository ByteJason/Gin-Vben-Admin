// Command migrate creates, drops, or inspects the single fresh-install GORM
// schema registered from the shared persistence models. Future versioned
// upgrades remain explicit CLI operations rather than startup-time AutoMigrate.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/migration"
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

	switch command.action {
	case actionUp:
		err = runner.Up()
	case actionDown:
		err = runner.Down(command.steps)
	case actionStatus:
		// Status is read below without applying a migration.
	}
	if err != nil {
		writeFailure(output, "operation")
		return 1
	}

	status, err := runner.Status()
	if err != nil {
		writeFailure(output, "status")
		return 1
	}
	writeSuccess(output, command.action, cfg.Database.Driver, status)
	return 0
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
