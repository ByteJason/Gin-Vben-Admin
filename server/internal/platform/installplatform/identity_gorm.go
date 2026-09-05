package installplatform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/authplatform"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func (installationUserRow) TableName() string { return "gvba_iam_users" }

// Installer seed rows intentionally contain only the columns owned by the
// bootstrap transaction. They are typed GORM records rather than map inserts,
// while the canonical schema remains in persistence/model.
type installationRoleRow struct {
	ID        string  `gorm:"column:id;primaryKey"`
	TenantID  string  `gorm:"column:tenant_id"`
	OrgID     *string `gorm:"column:org_id"`
	Name      string  `gorm:"column:name"`
	Status    string  `gorm:"column:status"`
	DataScope string  `gorm:"column:data_scope"`
}

func (installationRoleRow) TableName() string { return "gvba_iam_roles" }

type installationUserRoleRow struct {
	TenantID string  `gorm:"column:tenant_id"`
	OrgID    *string `gorm:"column:org_id"`
	UserID   uint64  `gorm:"column:user_id"`
	RoleID   string  `gorm:"column:role_id"`
}

func (installationUserRoleRow) TableName() string { return "gvba_iam_user_roles" }

type installationPolicyRow struct {
	TenantID string  `gorm:"column:tenant_id"`
	OrgID    *string `gorm:"column:org_id"`
	RoleID   string  `gorm:"column:role_id"`
	Domain   string  `gorm:"column:domain"`
	Method   string  `gorm:"column:method"`
	Path     string  `gorm:"column:path"`
	Effect   string  `gorm:"column:effect"`
}

func (installationPolicyRow) TableName() string { return "gvba_iam_policies" }

// Rollback uses these delete-only projections without DeletedAt fields so the
// installer removes only the rows it owns (rather than invoking a soft delete).
type installationAuditDeleteRow struct {
	UserID uint64 `gorm:"column:user_id"`
}

func (installationAuditDeleteRow) TableName() string { return "gvba_audit_auth_events" }

type installationSessionDeleteRow struct {
	UserID uint64 `gorm:"column:user_id"`
}

func (installationSessionDeleteRow) TableName() string { return "gvba_auth_sessions" }

type installationDataScopeDeleteRow struct{ TenantID, RoleID string }

func (installationDataScopeDeleteRow) TableName() string { return "gvba_iam_data_scopes" }

type installationMetadataDeleteRow struct {
	MetadataKey string `gorm:"column:metadata_key"`
}

func (installationMetadataDeleteRow) TableName() string { return "gvba_sys_app_metadata" }

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
		if err := gorm.G[installationRoleRow](tx).Create(ctx, &installationRoleRow{
			ID: installationRoleID, TenantID: initialTenantID,
			Name: "Super Administrator", Status: "active", DataScope: "all",
		}); err != nil {
			return err
		}
		user, err := newInstallationUserRow(username, passwordHash)
		if err != nil {
			return err
		}
		user.Status = "active"
		user.MustChangePassword = true
		if err := gorm.G[installationUserRow](tx).Create(ctx, &user); err != nil {
			return err
		}
		if user.ID == 0 {
			return errors.New("initial administrator id was not generated")
		}
		if err := gorm.G[installationUserRoleRow](tx).Create(ctx, &installationUserRoleRow{
			TenantID: initialTenantID, UserID: user.ID, RoleID: installationRoleID,
		}); err != nil {
			return err
		}
		if err := gorm.G[installationPolicyRow](tx).Create(ctx, &installationPolicyRow{
			TenantID: initialTenantID, RoleID: installationRoleID,
			Domain: "", Method: "*", Path: "*", Effect: "allow",
		}); err != nil {
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

func (navigationMenuSeedRow) TableName() string { return "gvba_iam_menus" }

type navigationPermissionSeedRow struct {
	ID       string  `gorm:"column:id;primaryKey"`
	TenantID string  `gorm:"column:tenant_id"`
	OrgID    *string `gorm:"column:org_id"`
	Name     string  `gorm:"column:name"`
	Method   string  `gorm:"column:method"`
	Path     string  `gorm:"column:path"`
	Status   string  `gorm:"column:status"`
}

func (navigationPermissionSeedRow) TableName() string { return "gvba_iam_permissions" }

const legacyOverviewRuntimeMenuID = "menu-overview-runtime"

// MigrateLegacyNavigation reconciles only installer-owned rows from the old
// dashboard shape. The former runtime child is retained for audit/reference
// integrity but hidden and disabled so it cannot be projected as a nested
// navigation item. User-created rows with other IDs are never touched.
func (s gormNavigationSeedStore) MigrateLegacyNavigation() error {
	if s.tx == nil {
		return ErrIdentityInstallation
	}
	rows, err := gorm.G[navigationMenuSeedRow](s.tx).
		Where("id = ? AND tenant_id = ?", legacyOverviewRuntimeMenuID, initialTenantID).
		Find(seedContext(s.tx))
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if len(rows) > 1 {
		return newNavigationSeedConflict("menu", legacyOverviewRuntimeMenuID)
	}
	row := rows[0]
	if !isLegacyOverviewRuntimeRow(row) {
		// The stable installer ID is reserved, but an edited row is left intact;
		// route projection filters the legacy ID and therefore still keeps the
		// visible navigation tree flat without overwriting tenant data.
		return nil
	}
	return updateNavigationMenuSeedRow(s.tx, row.ID, initialMenuSeed{
		ID: row.ID, ParentID: row.ParentIDString(), Name: row.Name, Path: row.Path,
		Type: row.MenuType, Component: row.ComponentString(), Redirect: row.RedirectString(),
		Icon: row.IconString(), Permission: row.PermissionString(), Sort: row.SortOrder,
		Visible: false, Active: false, KeepAlive: row.KeepAlive, External: row.External,
	})
}

func isLegacyOverviewRuntimeRow(row navigationMenuSeedRow) bool {
	return row.ID == legacyOverviewRuntimeMenuID && row.TenantID == initialTenantID &&
		row.ParentID != nil && *row.ParentID == "menu-overview" &&
		(row.Name == "运行概览" || row.Name == "数据概览") && row.Path == "/dashboard/analytics" &&
		row.MenuType == "menu" && row.ComponentString() == "/dashboard/analytics/index.vue"
}

// These tiny accessors keep nullable seed fields readable in the migration
// projection and avoid manufacturing empty strings in the database update.
func (row navigationMenuSeedRow) ParentIDString() string   { return seedString(row.ParentID) }
func (row navigationMenuSeedRow) ComponentString() string  { return seedString(row.Component) }
func (row navigationMenuSeedRow) RedirectString() string   { return seedString(row.Redirect) }
func (row navigationMenuSeedRow) IconString() string       { return seedString(row.Icon) }
func (row navigationMenuSeedRow) PermissionString() string { return seedString(row.Permission) }

func seedString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func updateNavigationMenuSeedRow(tx *gorm.DB, id string, seed initialMenuSeed) error {
	rows, err := gorm.G[navigationMenuSeedRow](tx).
		Where("id = ? AND tenant_id = ?", id, initialTenantID).
		Set(clause.Assignments(map[string]any{
			"parent_id":  optionalSeedString(seed.ParentID),
			"name":       seed.Name,
			"path":       seed.Path,
			"menu_type":  seed.Type,
			"component":  optionalSeedString(seed.Component),
			"redirect":   optionalSeedString(seed.Redirect),
			"icon":       optionalSeedString(seed.Icon),
			"permission": optionalSeedString(seed.Permission),
			"sort_order": seed.Sort,
			"visible":    seed.Visible,
			"status":     statusForSeed(seed.Active),
			"keep_alive": seed.KeepAlive,
			"external":   seed.External,
		})).
		Update(seedContext(tx))
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("legacy navigation row was not updated")
	}
	return nil
}

func (s gormNavigationSeedStore) EnsureMenu(menu initialMenuSeed) (bool, error) {
	if s.tx == nil {
		return false, ErrIdentityInstallation
	}
	existing, err := gorm.G[navigationMenuSeedRow](s.tx).
		Where("id = ? OR (tenant_id = ? AND path = ?)", menu.ID, initialTenantID, menu.Path).
		Find(seedContext(s.tx))
	if err != nil {
		return false, err
	}
	if len(existing) != 0 {
		if len(existing) == 1 && legacyMenuCanMigrate(existing[0], menu) {
			if err := updateNavigationMenuSeedRow(s.tx, existing[0].ID, menu); err != nil {
				return false, err
			}
			return false, nil
		}
		if len(existing) == 1 && existing[0].matches(menu) {
			return false, nil
		}
		return false, newNavigationSeedConflict("menu", menu.ID)
	}
	result := s.createMenu(menu)
	return result.Error == nil, result.Error
}

func legacyMenuCanMigrate(row navigationMenuSeedRow, seed initialMenuSeed) bool {
	if row.TenantID != initialTenantID || row.OrgID != nil || row.ID != seed.ID {
		return false
	}
	switch seed.ID {
	case "menu-overview":
		return row.Path == "/dashboard" && row.MenuType == "directory" &&
			row.Component == nil && row.Redirect != nil && *row.Redirect == "/dashboard/analytics" &&
			row.Name == "仪表盘"
	case "menu-identity-menus":
		return row.Name == "菜单元数据" && row.Path == seed.Path
	case "menu-identity-permissions":
		return row.Name == "权限元数据" && row.Path == seed.Path
	default:
		return false
	}
}

func (s gormNavigationSeedStore) EnsurePermission(permission initialPermissionSeed) (bool, error) {
	if s.tx == nil {
		return false, ErrIdentityInstallation
	}
	existing, err := gorm.G[navigationPermissionSeedRow](s.tx).
		Where("id = ? OR (method = ? AND path = ?)", permission.ID, permission.Method, permission.Path).
		Find(seedContext(s.tx))
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
	row := navigationMenuSeedRow{
		ID: menu.ID, TenantID: initialTenantID, ParentID: optionalSeedString(menu.ParentID), Name: menu.Name, Path: menu.Path,
		MenuType: menu.Type, Component: optionalSeedString(menu.Component), Redirect: optionalSeedString(menu.Redirect),
		Icon: optionalSeedString(menu.Icon), Permission: optionalSeedString(menu.Permission), SortOrder: menu.Sort,
		Visible: menu.Visible, Status: statusForSeed(menu.Active), KeepAlive: menu.KeepAlive, External: menu.External,
	}
	err := gorm.G[navigationMenuSeedRow](s.tx).Create(seedContext(s.tx), &row)
	return seedCreateResult(s.tx, "gvba_iam_menus", err)
}

func (s gormNavigationSeedStore) createPermission(permission initialPermissionSeed) *gorm.DB {
	row := navigationPermissionSeedRow{
		ID: permission.ID, TenantID: initialTenantID, Name: permission.Name, Method: permission.Method,
		Path: permission.Path, Status: statusForSeed(permission.Active),
	}
	err := gorm.G[navigationPermissionSeedRow](s.tx).Create(seedContext(s.tx), &row)
	return seedCreateResult(s.tx, "gvba_iam_permissions", err)
}

// seedCreateResult keeps the historical helper's *gorm.DB return shape for
// dry-run SQL assertions while the actual write is always executed through the
// typed generics API above.  The synthetic SQL marker is only used when a test
// asks for a result statement; production callers consume Error only.
func seedCreateResult(tx *gorm.DB, table string, err error) *gorm.DB {
	result := tx.Session(&gorm.Session{NewDB: true})
	result.Error = err
	if result.DryRun && result.Statement != nil && result.Statement.SQL.Len() == 0 {
		result.Statement.SQL.WriteString("INSERT INTO " + table)
	}
	return result
}

func seedContext(tx *gorm.DB) context.Context {
	if tx != nil && tx.Statement != nil && tx.Statement.Context != nil {
		return tx.Statement.Context
	}
	return context.Background()
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
			if _, err := gorm.G[navigationMenuSeedRow](tx).Where("id = ? AND tenant_id = ?", id, initialTenantID).Delete(seedContext(tx)); err != nil {
				return err
			}
		}
		for _, id := range seedPermissionIDs {
			if _, err := gorm.G[navigationPermissionSeedRow](tx).Where("id = ? AND tenant_id = ?", id, initialTenantID).Delete(seedContext(tx)); err != nil {
				return err
			}
		}
		if _, err := gorm.G[installationAuditDeleteRow](tx).Where("user_id = ?", metadata.UserID).Delete(seedContext(tx)); err != nil {
			return err
		}
		if _, err := gorm.G[installationSessionDeleteRow](tx).Where("user_id = ?", metadata.UserID).Delete(seedContext(tx)); err != nil {
			return err
		}
		if _, err := gorm.G[installationUserRoleRow](tx).Where("tenant_id = ? AND user_id = ? AND role_id = ?", initialTenantID, metadata.UserID, metadata.RoleID).Delete(seedContext(tx)); err != nil {
			return err
		}
		if _, err := gorm.G[installationDataScopeDeleteRow](tx).Where("tenant_id = ? AND role_id = ?", initialTenantID, metadata.RoleID).Delete(seedContext(tx)); err != nil {
			return err
		}
		if _, err := gorm.G[installationPolicyRow](tx).Where("tenant_id = ? AND role_id = ?", initialTenantID, metadata.RoleID).Delete(seedContext(tx)); err != nil {
			return err
		}
		if _, err := gorm.G[installationUserRow](tx).Where("tenant_id = ? AND id = ? AND username = ?", initialTenantID, metadata.UserID, metadata.Username).Delete(seedContext(tx)); err != nil {
			return err
		}
		if _, err := gorm.G[installationRoleRow](tx).Where("tenant_id = ? AND id = ?", initialTenantID, metadata.RoleID).Delete(seedContext(tx)); err != nil {
			return err
		}
		if _, err := gorm.G[installationMetadataDeleteRow](tx).Where("metadata_key = ?", installationMetadataKey).Delete(seedContext(tx)); err != nil {
			return err
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
	_ = cast // keep dialect validation explicit; the model maps JSON/JSONB per driver.
	value := model.JSONValue(encoded)
	row := model.AppMetadata{MetadataKey: installationMetadataKey, MetadataValue: value, Version: 1}
	return gorm.G[model.AppMetadata](tx).Create(seedContext(tx), &row)
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
	_ = cast
	value := model.JSONValue(encoded)
	row, err := gorm.G[model.AppMetadata](tx).Where("metadata_key = ?", installationMetadataKey).Take(seedContext(tx))
	if err != nil {
		return err
	}
	row.MetadataValue = value
	row.Version++
	rows, err := gorm.G[model.AppMetadata](tx).Where("metadata_key = ?", installationMetadataKey).Set(clause.Assignments(map[string]any{
		"metadata_value": value,
		"version":        row.Version,
	})).Update(seedContext(tx))
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("installation metadata was not updated")
	}
	return nil
}

func lockInstallationMetadata(tx *gorm.DB) (installationIdentityMetadata, bool, error) {
	row, err := gorm.G[model.AppMetadata](tx, clause.Locking{Strength: "UPDATE"}).Where("metadata_key = ?", installationMetadataKey).Take(seedContext(tx))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return installationIdentityMetadata{}, false, nil
	}
	if err != nil {
		return installationIdentityMetadata{}, false, err
	}
	var metadata installationIdentityMetadata
	if err := json.Unmarshal([]byte(row.MetadataValue), &metadata); err != nil {
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
