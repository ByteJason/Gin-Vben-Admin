package installplatform

import (
	iamapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/iam"
)

const initialTenantID = "default"

type initialMenuSeed struct {
	ID, ParentID, Name, Path string
	Type, Component          string
	Redirect, Icon           string
	Permission               string
	Sort                     int
	Visible, Active          bool
	KeepAlive, External      bool
}

type initialPermissionSeed struct {
	ID, Name, Method, Path string
	Active                 bool
}

type initialNavigationSeedStore interface {
	EnsureMenu(initialMenuSeed) (bool, error)
	EnsurePermission(initialPermissionSeed) (bool, error)
}

// initialNavigationSeedMigrator is implemented by stores that can reconcile
// installer-owned navigation rows from the previous catalog shape. Keeping it
// optional preserves the small in-memory seam used by unit tests and custom
// installers.
type initialNavigationSeedMigrator interface {
	MigrateLegacyNavigation() error
}

type initialNavigationSeedReceipt struct {
	MenuIDs       []string
	PermissionIDs []string
}

// seedInitialNavigation is intentionally idempotent at the store seam. The
// production adapter runs it inside the same transaction as administrator
// creation, while tests can exercise the catalog without a database driver.
func seedInitialNavigation(store initialNavigationSeedStore) (initialNavigationSeedReceipt, error) {
	if store == nil {
		return initialNavigationSeedReceipt{}, ErrIdentityInstallation
	}
	menus, permissions := initialNavigationSeeds()
	if migrator, ok := store.(initialNavigationSeedMigrator); ok {
		if err := migrator.MigrateLegacyNavigation(); err != nil {
			return initialNavigationSeedReceipt{}, err
		}
	}
	receipt := initialNavigationSeedReceipt{
		MenuIDs: make([]string, 0, len(menus)), PermissionIDs: make([]string, 0, len(permissions)),
	}
	for _, permission := range permissions {
		created, err := store.EnsurePermission(permission)
		if err != nil {
			return initialNavigationSeedReceipt{}, err
		}
		if created {
			receipt.PermissionIDs = append(receipt.PermissionIDs, permission.ID)
		}
	}
	for _, menu := range menus {
		created, err := store.EnsureMenu(menu)
		if err != nil {
			return initialNavigationSeedReceipt{}, err
		}
		if created {
			receipt.MenuIDs = append(receipt.MenuIDs, menu.ID)
		}
	}
	return receipt, nil
}

// filterKnownInitialNavigationSeedIDs bounds rollback to installer-owned,
// stable seed IDs even if persisted recovery metadata was tampered with.
func filterKnownInitialNavigationSeedIDs(menuIDs, permissionIDs []string) ([]string, []string) {
	menus, permissions := initialNavigationSeeds()
	knownMenus := make(map[string]struct{}, len(menus))
	knownPermissions := make(map[string]struct{}, len(permissions))
	for _, menu := range menus {
		knownMenus[menu.ID] = struct{}{}
	}
	for _, permission := range permissions {
		knownPermissions[permission.ID] = struct{}{}
	}
	return filterKnownSeedIDs(menuIDs, knownMenus), filterKnownSeedIDs(permissionIDs, knownPermissions)
}

func filterKnownSeedIDs(values []string, known map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := known[value]; !ok {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func initialNavigationSeeds() ([]initialMenuSeed, []initialPermissionSeed) {
	catalogMenus := iamapp.ProductionMenuCatalog()
	menus := make([]initialMenuSeed, 0, len(catalogMenus))
	for _, menu := range catalogMenus {
		menus = append(menus, initialMenuSeed{
			ID: menu.ID, ParentID: menu.ParentID, Name: menu.Name, Path: menu.Path,
			Type: string(menu.Type), Component: menu.Component, Redirect: menu.Redirect, Icon: menu.Icon,
			Permission: menu.Permission, Sort: menu.Sort, Visible: menu.Visible, Active: menu.Active,
			KeepAlive: menu.KeepAlive, External: menu.External,
		})
	}
	catalogPermissions := iamapp.ProductionPermissionCatalog()
	permissions := make([]initialPermissionSeed, 0, len(catalogPermissions))
	for _, permission := range catalogPermissions {
		permissions = append(permissions, initialPermissionSeed{
			ID: permission.ID, Name: permission.Name, Method: permission.Method, Path: permission.Path, Active: permission.Active,
		})
	}
	return menus, permissions
}
