package tasksplatform

import (
	"context"
	tasksapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/tasks"
	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
	"time"
)

// RedisLeaderLease adapts the platform Redis lock to the scheduler lease port.
type RedisLeaderLease struct{ Client *rediscache.Client }

func (l RedisLeaderLease) Acquire(ctx context.Context, key string, ttl time.Duration) (func() error, bool, error) {
	if l.Client == nil {
		return nil, false, nil
	}
	lock, err := l.Client.AcquireLock(ctx, key, ttl)
	if err != nil {
		if err == rediscache.ErrLockNotAcquired {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() error { return lock.Release(context.Background()) }, true, nil
}

var _ tasksapp.LeaderLease = RedisLeaderLease{}
