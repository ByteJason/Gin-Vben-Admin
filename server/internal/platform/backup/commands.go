package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	appbackup "example.com/gin-vben-admin/server/internal/application/backup"
	mysqldriver "github.com/go-sql-driver/mysql"
)

// Command is a structured process invocation. Arguments are passed directly to
// exec.Command; no shell is involved. Secrets are supplied through Env so
// they do not appear in argv or ordinary command logs.
type Command struct {
	Program string
	Args    []string
	Env     []string
}

type Runner interface {
	Run(context.Context, Command, io.Reader, io.Writer, io.Writer) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, command Command, stdin io.Reader, stdout, stderr io.Writer) error {
	if strings.TrimSpace(command.Program) == "" {
		return errors.New("command program is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	process := exec.CommandContext(ctx, command.Program, command.Args...)
	process.Env = append(os.Environ(), command.Env...)
	process.Stdin = stdin
	process.Stdout = stdout
	process.Stderr = stderr
	return process.Run()
}

// DatabaseCommands adapts database-native dump/restore binaries to the
// application backup ports. It is deliberately independent of artifact
// encryption and can be replaced by a fake runner in unit tests.
type DatabaseCommands struct {
	runner Runner
}

var (
	_ appbackup.Dumper   = (*DatabaseCommands)(nil)
	_ appbackup.Restorer = (*DatabaseCommands)(nil)
)

func NewDatabaseCommands(runner Runner) *DatabaseCommands {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &DatabaseCommands{runner: runner}
}

func (c *DatabaseCommands) Dump(ctx context.Context, source appbackup.Source, dst io.Writer) error {
	if c == nil || c.runner == nil {
		return errors.New("database command runner is not configured")
	}
	if dst == nil {
		return errors.New("database dump destination is required")
	}
	command, err := buildCommand(source, false)
	if err != nil {
		return err
	}
	if err := c.runner.Run(ctx, command, nil, dst, io.Discard); err != nil {
		return fmt.Errorf("run %s: %w", command.Program, err)
	}
	return nil
}

func (c *DatabaseCommands) Restore(ctx context.Context, source appbackup.Source, src io.Reader) error {
	if c == nil || c.runner == nil {
		return errors.New("database command runner is not configured")
	}
	if src == nil {
		return errors.New("database restore source is required")
	}
	command, err := buildCommand(source, true)
	if err != nil {
		return err
	}
	if err := c.runner.Run(ctx, command, src, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("run %s: %w", command.Program, err)
	}
	return nil
}

func buildCommand(source appbackup.Source, restore bool) (Command, error) {
	driver := appbackup.Driver(strings.ToLower(strings.TrimSpace(string(source.Driver))))
	if strings.TrimSpace(source.DSN) == "" {
		return Command{}, errors.New("database DSN is required")
	}
	switch driver {
	case appbackup.DriverMySQL:
		return buildMySQLCommand(source.DSN, restore)
	case appbackup.DriverPostgres:
		return buildPostgresCommand(source.DSN, restore)
	default:
		return Command{}, fmt.Errorf("unsupported backup database driver %q", source.Driver)
	}
}

func buildMySQLCommand(dsn string, restore bool) (Command, error) {
	cfg, err := mysqldriver.ParseDSN(strings.TrimSpace(dsn))
	if err != nil {
		return Command{}, fmt.Errorf("parse mysql backup DSN: %w", err)
	}
	if strings.TrimSpace(cfg.DBName) == "" {
		return Command{}, errors.New("mysql backup DSN database is required")
	}
	args := make([]string, 0, 12)
	if cfg.Net == "unix" {
		args = append(args, "--socket="+cfg.Addr)
	} else {
		host, port := splitAddress(cfg.Addr, "3306")
		args = append(args, "--host="+host, "--port="+port)
	}
	if cfg.User != "" {
		args = append(args, "--user="+cfg.User)
	}
	env := make([]string, 0, 1)
	if cfg.Passwd != "" {
		env = append(env, "MYSQL_PWD="+cfg.Passwd)
	}
	program := "mysql"
	if !restore {
		program = "mysqldump"
		args = append([]string{"--single-transaction", "--routines", "--triggers"}, args...)
	}
	args = append(args, cfg.DBName)
	return Command{Program: program, Args: args, Env: env}, nil
}

func buildPostgresCommand(dsn string, restore bool) (Command, error) {
	u, err := url.Parse(strings.TrimSpace(dsn))
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return Command{}, errors.New("postgres backup DSN must be a postgres URL")
	}
	database := strings.TrimPrefix(u.Path, "/")
	if database == "" {
		return Command{}, errors.New("postgres backup DSN database is required")
	}
	host := u.Hostname()
	if host == "" {
		return Command{}, errors.New("postgres backup DSN host is required")
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	args := []string{"--host=" + host, "--port=" + port}
	if u.User != nil {
		if user := u.User.Username(); user != "" {
			args = append(args, "--username="+user)
		}
	}
	env := make([]string, 0, 1)
	if u.User != nil {
		if password, ok := u.User.Password(); ok && password != "" {
			env = append(env, "PGPASSWORD="+password)
		}
	}
	program := "psql"
	if !restore {
		program = "pg_dump"
		args = append([]string{"--format=plain", "--no-owner", "--no-privileges"}, args...)
	} else {
		args = append([]string{"--set=ON_ERROR_STOP=1"}, args...)
	}
	args = append(args, "--dbname="+database)
	return Command{Program: program, Args: args, Env: env}, nil
}

func splitAddress(address, defaultPort string) (string, string) {
	address = strings.TrimSpace(address)
	if host, port, err := net.SplitHostPort(address); err == nil {
		if port == "" {
			port = defaultPort
		}
		return host, port
	}
	if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		return strings.Trim(address, "[]"), defaultPort
	}
	if strings.Count(address, ":") == 1 {
		parts := strings.SplitN(address, ":", 2)
		if _, err := strconv.Atoi(parts[1]); err == nil {
			return parts[0], parts[1]
		}
	}
	if address == "" {
		address = "127.0.0.1"
	}
	return address, defaultPort
}
