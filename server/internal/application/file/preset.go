package file

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

//go:embed assets/system-logo-default.svg
var presetAssets embed.FS

var (
	ErrPresetInvalid  = errors.New("invalid media preset")
	ErrPresetConflict = errors.New("media preset hash conflict")
)

// PresetAsset is a deterministic system-owned resource that can be
// reconciled during installation or startup. The manifest hash is checked
// before any write, making repeated reconciliation a no-op.
type PresetAsset struct {
	Key        string
	Name       string
	MIME       string
	Data       []byte
	SHA256     string
	CategoryID CategoryID
}

// DefaultPresetAsset returns the bundled image 1 used by the Logo picker.
func DefaultPresetAsset() PresetAsset {
	data, _ := presetAssets.ReadFile("assets/system-logo-default.svg")
	sum := sha256.Sum256(data)
	return PresetAsset{
		Key: "system.logo.default", Name: "system-logo-default.svg",
		MIME: "image/svg+xml", Data: data, SHA256: hex.EncodeToString(sum[:]),
	}
}

// ReconcilePreset creates the system resource once and returns its opaque ID.
// A platform-admin context is required because the resource is system-owned.
func (c *CatalogAdapter) ReconcilePreset(ctx context.Context, asset PresetAsset) (ResourceRef, error) {
	if c == nil || c.service == nil {
		return ResourceRef{}, ErrFileNotFound
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return ResourceRef{}, err
	}
	if !scope.PlatformAdmin {
		return ResourceRef{}, ErrAccessDenied
	}
	asset.Key = strings.TrimSpace(asset.Key)
	asset.Name = strings.TrimSpace(asset.Name)
	asset.MIME = strings.TrimSpace(asset.MIME)
	if asset.Key == "" || asset.Name == "" || asset.MIME == "" || len(asset.Data) == 0 || !validPresetKey(asset.Key) {
		return ResourceRef{}, ErrPresetInvalid
	}
	sum := sha256.Sum256(asset.Data)
	actual := hex.EncodeToString(sum[:])
	if strings.TrimSpace(asset.SHA256) == "" {
		asset.SHA256 = actual
	}
	if !strings.EqualFold(strings.TrimSpace(asset.SHA256), actual) {
		return ResourceRef{}, ErrPresetConflict
	}
	// Reconcile may run from more than one bootstrap hook (or concurrently in
	// tests). Serialize the whole read/create/update sequence so two callers
	// cannot upload duplicate system objects.
	c.presetMu.Lock()
	defer c.presetMu.Unlock()
	c.mu.Lock()
	if c.presets == nil {
		c.presets = make(map[string]ResourceRef)
	}
	if c.metadata == nil {
		c.metadata = make(map[string]map[string]string)
	}
	if c.status == nil {
		c.status = make(map[string]MediaStatus)
	}
	if c.updated == nil {
		c.updated = make(map[string]time.Time)
	}
	if existing, ok := c.presets[asset.Key]; ok {
		c.mu.Unlock()
		if existing.SHA256 != actual {
			return ResourceRef{}, ErrPresetConflict
		}
		return cloneResource(existing), nil
	}
	c.mu.Unlock()
	// Recover a matching resource after a process restart by inspecting the
	// provider-independent legacy index before creating a duplicate.
	page, listErr := c.service.List(ctx, ListFilter{})
	if listErr == nil {
		for _, item := range page.Items {
			if item.TenantID == "" && strings.EqualFold(item.SHA256, actual) && strings.EqualFold(item.Name, asset.Name) {
				ref := c.toResource(item, scope)
				ref.ReconcileKey = asset.Key
				c.mu.Lock()
				c.presets[asset.Key] = cloneResource(ref)
				c.metadata[item.ID] = map[string]string{"reconcile_key": asset.Key}
				c.status[item.ID] = MediaReady
				c.mu.Unlock()
				return ref, nil
			}
		}
	}
	created, err := c.service.Upload(ctx, UploadInput{
		Data: append([]byte(nil), asset.Data...), Size: int64(len(asset.Data)), Name: asset.Name,
		MIME: asset.MIME, ACL: ACLPublicRead, CategoryID: asset.CategoryID,
	})
	if err != nil {
		return ResourceRef{}, err
	}
	ref := c.toResource(created, scope)
	ref.ReconcileKey = asset.Key
	c.mu.Lock()
	c.presets[asset.Key] = cloneResource(ref)
	c.metadata[created.ID] = map[string]string{"reconcile_key": asset.Key}
	c.status[created.ID] = MediaReady
	c.updated[created.ID] = c.now()
	c.mu.Unlock()
	return ref, nil
}

func validPresetKey(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
