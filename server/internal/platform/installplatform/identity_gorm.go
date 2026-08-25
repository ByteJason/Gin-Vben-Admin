package installplatform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/authplatform"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
)

const (
	installationMetadataKey = "installation"
	installationRoleID      = "role-super-admin"
)

var ErrIdentityChanged = installer.ErrIdentityNotOwned

var errNavigationSeedConflict = errors.New("navigation seed conflicts with an existing resource")

type navigationSeedConflictError struct {
	resourceKind string
	resourceID   string
}

func newNavigationSeedConflict(resourceKind, resourceID string) error {
	return &navigationSeedConflictError{resourceKind: resourceKind, resourceID: resourceID}
}

func (e *navigationSeedConflictError) Error() string { return errNavigationSeedConflict.Error() }
func (e *navigationSeedConflictError) Unwrap() error { return errNavigationSeedConflict }
func (e *navigationSeedConflictError) InstallationFailureDiagnostic() installer.FailureDiagnostic {
	diagnostic := installer.FailureDiagnostic{Reason: "navigation_seed_conflict", Operation: "apply"}
	if (e.resourceKind == "menu" || e.resourceKind == "permission") && validNavigationResourceID(e.resourceID) {
		diagnostic.ResourceKind = e.resourceKind
		diagnostic.ResourceID = e.resourceID
	}
	return diagnostic
}

func validNavigationResourceID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, character := range id {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != ':' && character != '.' &&
			character != '_' && character != '-' {
			return false
		}
	}
	return true
}

type GORMIdentityStore struct {
	database *gormdb.Store
	driver   string
}

func NewGORMIdentityStore(request installer.DatabaseConnection) (*GORMIdentityStore, error) {
	options, err := databaseOptionsFromRequest(request)
	if err != nil {
		return nil, ErrIdentityInstallation
	}
	database, err := gormdb.Open(options)
	if err != nil {
		return nil, ErrIdentityInstallation
	}
	return &GORMIdentityStore{database: database, driver: options.Driver}, nil
}

func NewSystemIdentityInstaller() *IdentityInstaller {
	return NewIdentityInstaller(func(request installer.DatabaseConnection) (IdentityStore, error) {
		return NewGORMIdentityStore(request)
	}, authplatform.BcryptHasher{Cost: 12}, nil)
}

type installationIdentityMetadata struct {
	InstallationID    string   `json:"installation_id"`
	State             string   `json:"state"`
	UserID            uint64   `json:"user_id"`
	Username          string   `json:"username"`
	RoleID            string   `json:"role_id"`
	SeedMenuIDs       []string `json:"seed_menu_ids,omitempty"`
	SeedPermissionIDs []string `json:"seed_permission_ids,omitempty"`
}

type installationUserRow struct {
	ID                 uint64  `gorm:"column:id;primaryKey"`
	TenantID           string  `gorm:"column:tenant_id"`
	OrgID              *string `gorm:"column:org_id"`
	Username           string  `gorm:"column:username"`
	UsernameNormalized *string `gorm:"column:username_normalized"`
	PasswordHash       string  `gorm:"column:password_hash"`
	Status             string  `gorm:"column:status"`
	MustChangePassword bool    `gorm:"column:must_change_password"`
}

func (installationUserRow) TableName() string { return "users" }

func (s *GORMIdentityStore) Initialize(ctx context.Context, reference, username, passwordHash string) error {
	if s == nil || s.database == nil || !validIdentityInput(reference, username, passwordHash) {
		return ErrIdentityInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := s.database.WithinTransaction(ctx, func(tx *gorm.DB) error {
		metadata := installationIdentityMetadata{
			InstallationID: reference, State: "initializing", Username: username, RoleID: installationRoleID,
		}
		if err := createInstallationMetadata(tx, s.driver, metadata); err != nil {
			return err
		}
		if err := tx.Table("roles").Create(map[string]any{
			"id": installationRoleID, "tenant_id": initialTenantID, "org_id": nil,
			"name": "Super Administrator", "status": "active", "data_scope": "all",
		}).Error; err != nil {
			return err
		}
		user, err := newInstallationUserRow(username, passwordHash)
		if err != nil {
			return err
		}
		user.Status = "active"
		user.MustChangePassword = true
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if user.ID == 0 {
			return errors.New("initial administrator id was not generated")
		}
		if err := tx.Table("user_roles").Create(map[string]any{
			"tenant_id": initialTenantID, "org_id": nil, "user_id": user.ID, "role_id": installationRoleID,
		}).Error; err != nil {
			return err
		}
		if err := tx.Table("iam_policies").Create(map[string]any{
			"tenant_id": initialTenantID, "org_id": nil, "role_id": installationRoleID,
			"domain": "", "method": "*", "path": "*", "effect": "allow",
		}).Error; err != nil {
			return err
		}
		seedReceipt, err := seedInitialNavigation(gormNavigationSeedStore{tx: tx})
		if err != nil {
			return err
		}
		metadata.UserID = user.ID
		metadata.SeedMenuIDs = append([]string(nil), seedReceipt.MenuIDs...)
		metadata.SeedPermissionIDs = append([]string(nil), seedReceipt.PermissionIDs...)
		metadata.State = "installed"
		return updateInstallationMetadata(tx, s.driver, metadata)
	})
	if err != nil {
		var diagnostic installer.FailureDiagnosticProvider
		if errors.As(err, &diagnostic) {
			return err
		}
		return ErrIdentityInstallation
	}
	return nil
}

type gormNavigationSeedStore struct{ tx *gorm.DB }

type navigationMenuSeedRow struct {
	ID         string  `gorm:"column:id;primaryKey"`
	TenantID   string  `gorm:"column:tenant_id"`
	OrgID      *string `gorm:"column:org_id"`
	ParentID   *string `gorm:"column:parent_id"`
	Name       string  `gorm:"column:name"`
	Path       string  `gorm:"column:path"`
	MenuType   string  `gorm:"column:menu_type"`
	Component  *string `gorm:"column:component"`
	Redirect   *string `gorm:"column:redirect"`
	Icon       *string `gorm:"column:icon"`
	Permission *string `gorm:"column:permission"`
	SortOrder  int     `gorm:"column:sort_order"`
	Visible    bool    `gorm:"column:visible"`
	Status     string  `gorm:"column:status"`
	KeepAlive  bool    `gorm:"column:keep_alive"`
	External   bool    `gorm:"column:external"`
}

type navigationPermissionSeedRow struct {
	ID       string  `gorm:"column:id;primaryKey"`
	TenantID string  `gorm:"column:tenant_id"`
	OrgID    *string `gorm:"column:org_id"`
	Name     string  `gorm:"column:name"`
	Method   string  `gorm:"column:method"`
	Path     string  `gorm:"column:path"`
	Status   string  `gorm:"column:status"`
}

func (s gormNavigationSeedStore) EnsureMenu(menu initialMenuSeed) (bool, error) {
	if s.tx == nil {
		return false, ErrIdentityInstallation
	}
	var existing []navigationMenuSeedRow
	err := s.tx.Table("menus").
		Where("id = ? OR (tenant_id = ? AND path = ?)", menu.ID, initialTenantID, menu.Path).
		Find(&existing).Error
	if err != nil {
		return false, err
	}
	if len(existing) != 0 {
		if len(existing) == 1 && existing[0].matches(menu) {
			return false, nil
		}
		return false, newNavigationSeedConflict("menu", menu.ID)
	}
	result := s.createMenu(menu)
	return result.Error == nil, result.Error
}

func (s gormNavigationSeedStore) EnsurePermission(permission initialPermissionSeed) (bool, error) {
	if s.tx == nil {
		return false, ErrIdentityInstallation
	}
	var existing []navigationPermissionSeedRow
	err := s.tx.Table("permissions").
		Where("id = ? OR (method = ? AND path = ?)", permission.ID, permission.Method, permission.Path).
		Find(&existing).Error
	if err != nil {
		return false, err
	}
	if len(existing) != 0 {
		if len(existing) == 1 && existing[0].matches(permission) {
			return false, nil
		}
		return false, newNavigationSeedConflict("permission", permission.ID)
	}
	result := s.createPermission(permission)
	return result.Error == nil, result.Error
}

func (s gormNavigationSeedStore) createMenu(menu initialMenuSeed) *gorm.DB {
	return s.tx.Table("menus").Create(&navigationMenuSeedRow{
		ID: menu.ID, TenantID: initialTenantID, ParentID: optionalSeedString(menu.ParentID), Name: menu.Name, Path: menu.Path,
		MenuType: menu.Type, Component: optionalSeedString(menu.Component), Redirect: optionalSeedString(menu.Redirect),
		Icon: optionalSeedString(menu.Icon), Permission: optionalSeedString(menu.Permission), SortOrder: menu.Sort,
		Visible: menu.Visible, Status: statusForSeed(menu.Active), KeepAlive: menu.KeepAlive, External: menu.External,
	})
}

func (s gormNavigationSeedStore) createPermission(permission initialPermissionSeed) *gorm.DB {
	return s.tx.Table("permissions").Create(&navigationPermissionSeedRow{
		ID: permission.ID, TenantID: initialTenantID, Name: permission.Name, Method: permission.Method,
		Path: permission.Path, Status: statusForSeed(permission.Active),
	})
}

func (row navigationMenuSeedRow) matches(seed initialMenuSeed) bool {
	return row.ID == seed.ID && row.TenantID == initialTenantID && row.OrgID == nil &&
		nullableSeedStringMatches(row.ParentID, seed.ParentID) && row.Name == seed.Name && row.Path == seed.Path &&
		row.MenuType == seed.Type && nullableSeedStringMatches(row.Component, seed.Component) &&
		nullableSeedStringMatches(row.Redirect, seed.Redirect) && nullableSeedStringMatches(row.Icon, seed.Icon) &&
		nullableSeedStringMatches(row.Permission, seed.Permission) && row.SortOrder == seed.Sort &&
		row.Visible == seed.Visible && row.Status == statusForSeed(seed.Active) && row.KeepAlive == seed.KeepAlive && row.External == seed.External
}

func (row navigationPermissionSeedRow) matches(seed initialPermissionSeed) bool {
	return row.ID == seed.ID && row.TenantID == initialTenantID && row.OrgID == nil &&
		row.Name == seed.Name && row.Method == seed.Method && row.Path == seed.Path && row.Status == statusForSeed(seed.Active)
}

func nullableSeedStringMatches(actual *string, expected string) bool {
	if expected == "" {
		return actual == nil
	}
	return actual != nil && *actual == expected
}

func statusForSeed(active bool) string {
	if active {
		return "active"
	}
	return "disabled"
}

func optionalSeedString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	return &trimmed
}

func newInstallationUserRow(username, passwordHash string) (installationUserRow, error) {
	username = strings.TrimSpace(username)
	normalized, identifierType, err := authdomain.NormalizeIdentifier(username)
	if err != nil || identifierType != authdomain.IdentifierUsername || strings.TrimSpace(passwordHash) == "" {
		return installationUserRow{}, authdomain.ErrInvalidIdentifier
	}
	return installationUserRow{TenantID: initialTenantID, Username: username, UsernameNormalized: &normalized, PasswordHash: passwordHash}, nil
}

func (s *GORMIdentityStore) Rollback(ctx context.Context, reference string) error {
	if s == nil || s.database == nil || strings.TrimSpace(reference) == "" {
		return ErrIdentityInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := s.database.WithinTransaction(ctx, func(tx *gorm.DB) error {
		metadata, found, err := lockInstallationMetadata(tx)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if metadata.InstallationID != reference {
			return ErrIdentityChanged
		}
		if metadata.UserID == 0 || metadata.RoleID != installationRoleID {
			return errors.New("installation identity metadata is incomplete")
		}
		seedMenuIDs, seedPermissionIDs := filterKnownInitialNavigationSeedIDs(metadata.SeedMenuIDs, metadata.SeedPermissionIDs)
		for _, id := range seedMenuIDs {
			if err := tx.Exec("DELETE FROM menus WHERE id = ? AND tenant_id = ?", id, initialTenantID).Error; err != nil {
				return err
			}
		}
		for _, id := range seedPermissionIDs {
			if err := tx.Exec("DELETE FROM permissions WHERE id = ? AND tenant_id = ?", id, initialTenantID).Error; err != nil {
				return err
			}
		}
		for _, operation := range []struct {
			query string
			args  []any
		}{
			{query: "DELETE FROM auth_audit_events WHERE user_id = ?", args: []any{metadata.UserID}},
			{query: "DELETE FROM auth_sessions WHERE user_id = ?", args: []any{metadata.UserID}},
			{query: "DELETE FROM user_roles WHERE tenant_id = ? AND user_id = ? AND role_id = ?", args: []any{initialTenantID, metadata.UserID, metadata.RoleID}},
			{query: "DELETE FROM iam_data_scopes WHERE tenant_id = ? AND role_id = ?", args: []any{initialTenantID, metadata.RoleID}},
			{query: "DELETE FROM iam_policies WHERE tenant_id = ? AND role_id = ?", args: []any{initialTenantID, metadata.RoleID}},
			{query: "DELETE FROM users WHERE tenant_id = ? AND id = ? AND username = ?", args: []any{initialTenantID, metadata.UserID, metadata.Username}},
			{query: "DELETE FROM roles WHERE tenant_id = ? AND id = ?", args: []any{initialTenantID, metadata.RoleID}},
			{query: "DELETE FROM app_metadata WHERE metadata_key = ?", args: []any{installationMetadataKey}},
		} {
			if err := tx.Exec(operation.query, operation.args...).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, ErrIdentityChanged) {
		return ErrIdentityChanged
	}
	if err != nil {
		return ErrIdentityInstallation
	}
	return nil
}

func (s *GORMIdentityStore) Close() error {
	if s == nil || s.database == nil {
		return nil
	}
	err := s.database.Close()
	s.database = nil
	if err != nil {
		return ErrIdentityInstallation
	}
	return nil
}

func createInstallationMetadata(tx *gorm.DB, driver string, metadata installationIdentityMetadata) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	cast, err := jsonCast(driver)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("INSERT INTO app_metadata (metadata_key, metadata_value, version) VALUES (?, CAST(? AS %s), ?)", cast)
	return tx.Exec(query, installationMetadataKey, string(encoded), 1).Error
}

func updateInstallationMetadata(tx *gorm.DB, driver string, metadata installationIdentityMetadata) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	cast, err := jsonCast(driver)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("UPDATE app_metadata SET metadata_value = CAST(? AS %s), version = version + 1 WHERE metadata_key = ?", cast)
	result := tx.Exec(query, string(encoded), installationMetadataKey)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("installation metadata was not updated")
	}
	return nil
}

func lockInstallationMetadata(tx *gorm.DB) (installationIdentityMetadata, bool, error) {
	var raw string
	result := tx.Raw("SELECT metadata_value FROM app_metadata WHERE metadata_key = ? FOR UPDATE", installationMetadataKey).Scan(&raw)
	if result.Error != nil {
		return installationIdentityMetadata{}, false, result.Error
	}
	if result.RowsAffected == 0 {
		return installationIdentityMetadata{}, false, nil
	}
	var metadata installationIdentityMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return installationIdentityMetadata{}, false, err
	}
	return metadata, true, nil
}

func jsonCast(driver string) (string, error) {
	switch driver {
	case "mysql":
		return "JSON", nil
	case "postgres":
		return "JSONB", nil
	default:
		return "", errors.New("unsupported identity database driver")
	}
}

func validIdentityInput(reference, username, passwordHash string) bool {
	return strings.TrimSpace(reference) != "" && len(reference) <= 64 && strings.TrimSpace(username) != "" && len(username) <= 191 && strings.TrimSpace(passwordHash) != "" && len(passwordHash) <= 255 && !strings.ContainsAny(reference+username+passwordHash, "\x00\r\n")
}
