package gormdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	mysqldriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/dbresolver"
)

type Store struct {
	database *gorm.DB
	closers  []*sql.DB
}

// Open constructs the configured topology without probing the network. Ping is
// deliberately a separate readiness operation so a recovered dependency can
// make an already-running process ready again.
func Open(options Options) (*Store, error) {
	options.applyDefaults()
	if err := options.Validate(); err != nil {
		return nil, err
	}

	primaryDSN := options.DSN
	if options.Mode == ModeReadWrite {
		primaryDSN = options.PrimaryDSN
	}
	primarySQL, primaryDialector, err := openDialector(options.Driver, primaryDSN, options)
	if err != nil {
		return nil, err
	}
	store := &Store{closers: []*sql.DB{primarySQL}}
	closeOnError := func(cause error) (*Store, error) {
		return nil, errors.Join(cause, store.Close())
	}

	database, err := gorm.Open(primaryDialector, &gorm.Config{
		Logger:               logger.Default.LogMode(logger.Silent),
		DisableAutomaticPing: true,
	})
	if err != nil {
		return closeOnError(fmt.Errorf("initialize %s database: %w", options.Driver, err))
	}
	store.database = database

	if options.Mode == ModeReadWrite {
		replicas := make([]gorm.Dialector, 0, len(options.ReplicaDSNs))
		for _, replicaDSN := range options.ReplicaDSNs {
			replicaSQL, replicaDialector, openErr := openDialector(options.Driver, replicaDSN, options)
			if openErr != nil {
				return closeOnError(openErr)
			}
			store.closers = append(store.closers, replicaSQL)
			replicas = append(replicas, replicaDialector)
		}

		resolver := dbresolver.Register(dbresolver.Config{
			Replicas: replicas,
			Policy:   resolverPolicy(options.ReadPolicy),
		}).
			SetMaxOpenConns(options.MaxOpenConns).
			SetMaxIdleConns(options.MaxIdleConns).
			SetConnMaxLifetime(options.ConnMaxLifetime).
			SetConnMaxIdleTime(options.ConnMaxIdleTime)
		if err := database.Use(resolver); err != nil {
			return closeOnError(fmt.Errorf("initialize database read/write resolver: %w", err))
		}
	}

	return store, nil
}

func (s *Store) Name() string { return "database" }

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || len(s.closers) == 0 {
		return errors.New("database store is not initialized")
	}
	for index, connection := range s.closers {
		if err := connection.PingContext(ctx); err != nil {
			return fmt.Errorf("database endpoint %d is unavailable: %w", index+1, err)
		}
	}
	return nil
}

// RuntimeStats returns non-sensitive SQL pool counters for the operations
// snapshot. It deliberately exposes no DSN, driver credentials, or query
// text. Read/write topologies are aggregated across their configured pools.
func (s *Store) RuntimeStats(context.Context) (open, idle, max int, keyspace int64, err error) {
	if s == nil || len(s.closers) == 0 {
		return 0, 0, 0, 0, errors.New("database store is not initialized")
	}
	for _, connection := range s.closers {
		stats := connection.Stats()
		open += stats.OpenConnections
		idle += stats.Idle
		if stats.MaxOpenConnections > max {
			max = stats.MaxOpenConnections
		}
	}
	return open, idle, max, 0, nil
}

// Read returns a GORM session explicitly routed to a configured replica when
// read/write mode is active. Callers that require read-your-write use Write.
func (s *Store) Read(ctx context.Context) *gorm.DB {
	return s.database.WithContext(ctx).Clauses(dbresolver.Read)
}

// Write returns a GORM session pinned to the primary/write endpoint.
func (s *Store) Write(ctx context.Context) *gorm.DB {
	return s.database.WithContext(ctx).Clauses(dbresolver.Write)
}

// WithinTransaction always begins on the write endpoint and leaves commit or
// rollback to GORM based on the callback error.
func (s *Store) WithinTransaction(ctx context.Context, operation func(*gorm.DB) error) error {
	if s == nil || s.database == nil {
		return errors.New("database store is not initialized")
	}
	if operation == nil {
		return errors.New("database transaction operation is required")
	}
	return s.Write(ctx).Transaction(operation)
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var closeErrors []error
	for index := len(s.closers) - 1; index >= 0; index-- {
		if err := s.closers[index].Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close database endpoint %d: %w", index+1, err))
		}
	}
	s.closers = nil
	s.database = nil
	return errors.Join(closeErrors...)
}

func openDialector(driver, dsn string, options Options) (*sql.DB, gorm.Dialector, error) {
	var (
		connection *sql.DB
		dialector  gorm.Dialector
	)
	switch driver {
	case "mysql":
		parsed, err := mysqldriver.ParseDSN(dsn)
		if err != nil {
			return nil, nil, errors.New("parse mysql database dsn")
		}
		parsed.ParseTime = true
		connection, err = sql.Open("mysql", parsed.FormatDSN())
		if err != nil {
			return nil, nil, fmt.Errorf("open mysql database: %w", err)
		}
		dialector = gormmysql.New(gormmysql.Config{Conn: connection, SkipInitializeWithVersion: true})
	case "postgres":
		var err error
		connection, err = sql.Open("pgx", dsn)
		if err != nil {
			return nil, nil, fmt.Errorf("open postgres database: %w", err)
		}
		dialector = gormpostgres.New(gormpostgres.Config{Conn: connection, PreferSimpleProtocol: true})
	default:
		return nil, nil, errors.New("unsupported database driver")
	}

	connection.SetMaxOpenConns(options.MaxOpenConns)
	connection.SetMaxIdleConns(options.MaxIdleConns)
	connection.SetConnMaxLifetime(options.ConnMaxLifetime)
	connection.SetConnMaxIdleTime(options.ConnMaxIdleTime)
	return connection, dialector, nil
}

func resolverPolicy(policy ReadPolicy) dbresolver.Policy {
	if policy == ReadPolicyRoundRobin {
		return dbresolver.StrictRoundRobinPolicy()
	}
	return dbresolver.RandomPolicy{}
}
