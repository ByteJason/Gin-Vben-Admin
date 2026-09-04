package bootstrap

import (
	"context"
	"errors"
	"strings"

	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
)

// settingsCacheAdapter keeps the settings application contract independent of
// Redis key layout. Only redacted invalidation/revision markers are stored;
// current values are always reconstructed from the database.
type settingsCacheAdapter struct{ client *rediscache.Client }

func (a settingsCacheAdapter) Invalidate(ctx context.Context, key string) error {
	if a.client == nil {
		return errors.New("settings redis cache is not initialized")
	}
	key = strings.TrimSpace(key)
	if key == "" || strings.Contains(key, ":") {
		return rediscache.ErrInvalidKey
	}
	physical, err := a.client.SettingsValueKey(ctx, key)
	if err != nil {
		return err
	}
	return a.client.Delete(ctx, physical)
}

func (a settingsCacheAdapter) InvalidateModule(ctx context.Context, module string, revision int64) error {
	if a.client == nil {
		return errors.New("settings redis cache is not initialized")
	}
	return a.client.InvalidateModule(ctx, module, revision)
}
