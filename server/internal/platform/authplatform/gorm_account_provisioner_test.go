package authplatform

import (
	"context"
	"errors"
	"testing"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestGORMUserRepositoryCreatesUserAndUpdatesPassword(t *testing.T) {
	repo := NewGORMUserRepository(newTestStoreWithDialect(t, queryResult{}, "mysql"))
	if err := repo.CreateUser(context.Background(), authdomain.User{
		Identifier: "alice", PasswordHash: "$2a$hash", Active: true,
	}); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if err := repo.UpdatePassword(context.Background(), "alice", "$2a$replacement"); err != nil {
		t.Fatalf("UpdatePassword() error = %v", err)
	}
}

func TestGORMUserRepositoryMapsDuplicateUsername(t *testing.T) {
	repo := NewGORMUserRepository(newTestStoreWithDialect(t, queryResult{
		execErr: &mysqldriver.MySQLError{Number: 1062, Message: "duplicate entry"},
	}, "mysql"))
	err := repo.CreateUser(context.Background(), authdomain.User{
		Identifier: "alice", PasswordHash: "$2a$hash", Active: true,
	})
	if !errors.Is(err, authdomain.ErrUserAlreadyExists) {
		t.Fatalf("CreateUser() error = %v, want ErrUserAlreadyExists", err)
	}
}

var _ appauth.AccountProvisioner = (*GORMUserRepository)(nil)
