package bootstrap

import (
	"errors"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
)

// CredentialRecoveryDependencies opens only the database and Redis clients
// required by a local credential-recovery command. Unlike New, it does not
// construct HTTP services, inspect installation workspaces, reconcile journals,
// or acquire installation leases.
type CredentialRecoveryDependencies struct {
	database *gormdb.Store
	redis    *rediscache.Client
}

func NewCredentialRecoveryDependencies(cfg config.Config) (*CredentialRecoveryDependencies, error) {
	if !cfg.Database.Enabled || !cfg.Redis.Enabled || !cfg.Auth.Enabled {
		return nil, errors.New("credential recovery requires database, redis, and authentication")
	}
	databaseConfig, err := databaseOptions(cfg.Database)
	if err != nil {
		return nil, errors.New("configure credential recovery database")
	}
	database, err := gormdb.Open(databaseConfig)
	if err != nil {
		return nil, errors.New("initialize credential recovery database")
	}
	redisConfig, err := redisOptions(cfg.Redis)
	if err != nil {
		_ = database.Close()
		return nil, errors.New("configure credential recovery redis")
	}
	redis, err := rediscache.New(redisConfig)
	if err != nil {
		_ = database.Close()
		return nil, errors.New("initialize credential recovery redis")
	}
	return &CredentialRecoveryDependencies{database: database, redis: redis}, nil
}

func (d *CredentialRecoveryDependencies) Database() *gormdb.Store {
	if d == nil {
		return nil
	}
	return d.database
}

func (d *CredentialRecoveryDependencies) Redis() *rediscache.Client {
	if d == nil {
		return nil
	}
	return d.redis
}

func (d *CredentialRecoveryDependencies) Close() error {
	if d == nil {
		return nil
	}
	var closeErrors []error
	if d.redis != nil {
		closeErrors = append(closeErrors, d.redis.Close())
		d.redis = nil
	}
	if d.database != nil {
		closeErrors = append(closeErrors, d.database.Close())
		d.database = nil
	}
	return errors.Join(closeErrors...)
}
