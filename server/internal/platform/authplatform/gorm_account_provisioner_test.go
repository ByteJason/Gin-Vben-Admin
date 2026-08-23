package authplatform

import (
	"context"
	"errors"
	"testing"

	appauth "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestGORMUserRepositoryCreatesUserAndUpdatesPassword(t *testing.T) {
	repo := NewGORMUserRepository(newTestStoreWithDialect(t, queryResult{}, "mysql"))
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"})
	if err := repo.CreateUser(ctx, authdomain.User{
		Identifier: "alice", PasswordHash: "$2a$hash", Active: true,
	}); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if err := repo.UpdatePassword(ctx, "alice", "$2a$replacement"); err != nil {
		t.Fatalf("UpdatePassword() error = %v", err)
	}
}

func TestGORMUserRepositoryMapsDuplicateUsername(t *testing.T) {
	repo := NewGORMUserRepository(newTestStoreWithDialect(t, queryResult{
		execErr: &mysqldriver.MySQLError{Number: 1062, Message: "duplicate entry"},
	}, "mysql"))
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"})
	err := repo.CreateUser(ctx, authdomain.User{
		Identifier: "alice", PasswordHash: "$2a$hash", Active: true,
	})
	if !errors.Is(err, authdomain.ErrUserAlreadyExists) {
		t.Fatalf("CreateUser() error = %v, want ErrUserAlreadyExists", err)
	}
}

var _ appauth.AccountProvisioner = (*GORMUserRepository)(nil)
