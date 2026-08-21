package installplatform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	installer "example.com/gin-vben-admin/server/internal/application/installer"
	"example.com/gin-vben-admin/server/internal/platform/authplatform"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
)

const (
	installationMetadataKey = "installation"
	installationRoleID      = "role-super-admin"
)

var ErrIdentityChanged = errors.New("installation identity belongs to a different transaction")

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
	InstallationID string `json:"installation_id"`
	State          string `json:"state"`
	UserID         uint64 `json:"user_id"`
	Username       string `json:"username"`
	RoleID         string `json:"role_id"`
}

type installationUserRow struct {
	ID                 uint64 `gorm:"column:id;primaryKey"`
	Username           string `gorm:"column:username"`
	PasswordHash       string `gorm:"column:password_hash"`
	Status             string `gorm:"column:status"`
	MustChangePassword bool   `gorm:"column:must_change_password"`
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
			"id": installationRoleID, "name": "Super Administrator", "status": "active", "data_scope": "all",
		}).Error; err != nil {
			return err
		}
		user := installationUserRow{
			Username: username, PasswordHash: passwordHash, Status: "active", MustChangePassword: true,
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		if user.ID == 0 {
			return errors.New("initial administrator id was not generated")
		}
		if err := tx.Table("user_roles").Create(map[string]any{"user_id": user.ID, "role_id": installationRoleID}).Error; err != nil {
			return err
		}
		if err := tx.Table("iam_policies").Create(map[string]any{
			"role_id": installationRoleID, "domain": "", "method": "*", "path": "*", "effect": "allow",
		}).Error; err != nil {
			return err
		}
		metadata.UserID = user.ID
		metadata.State = "installed"
		return updateInstallationMetadata(tx, s.driver, metadata)
	})
	if err != nil {
		return ErrIdentityInstallation
	}
	return nil
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
		for _, operation := range []struct {
			query string
			args  []any
		}{
			{query: "DELETE FROM auth_audit_events WHERE user_id = ?", args: []any{metadata.UserID}},
			{query: "DELETE FROM auth_sessions WHERE user_id = ?", args: []any{metadata.UserID}},
			{query: "DELETE FROM user_roles WHERE user_id = ? AND role_id = ?", args: []any{metadata.UserID, metadata.RoleID}},
			{query: "DELETE FROM iam_data_scopes WHERE role_id = ?", args: []any{metadata.RoleID}},
			{query: "DELETE FROM iam_policies WHERE role_id = ?", args: []any{metadata.RoleID}},
			{query: "DELETE FROM users WHERE id = ? AND username = ?", args: []any{metadata.UserID, metadata.Username}},
			{query: "DELETE FROM roles WHERE id = ?", args: []any{metadata.RoleID}},
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
