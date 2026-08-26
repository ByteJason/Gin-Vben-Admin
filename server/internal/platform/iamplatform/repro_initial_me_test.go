package iamplatform

import (
	"context"
	"reflect"
	"regexp"
	"testing"
	"unsafe"

	iamapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/DATA-DOG/go-sqlmock"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestFindUserForAuthorizationResetsRelationQuerySession covers the bootstrap
// path used immediately after installation. The user lookup and role lookup
// previously reused one write session, which leaked Take's WHERE/LIMIT into the user_roles query.
func TestFindUserForAuthorizationResetsRelationQuerySession(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	gdb, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	store := &gormdb.Store{}
	field := reflect.ValueOf(store).Elem().FieldByName("database")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(gdb))
	persistence := NewGORMStore(store)
	userCols := []string{"id", "tenant_id", "org_id", "username", "username_normalized", "email", "email_normalized", "nickname", "avatar", "phone", "password_hash", "status", "last_login_ip", "last_login_at", "password_changed_at"}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users" WHERE tenant_id = $1 AND id = $2 LIMIT $3`)).WithArgs("default", 1, 1).WillReturnRows(sqlmock.NewRows(userCols).AddRow(1, "default", nil, "admin", "admin", nil, nil, "", "", nil, "hash", "active", "", nil, nil))
	mock.ExpectQuery(`SELECT .*role_id.*FROM "user_roles" WHERE tenant_id = \$1 AND user_id = \$2 ORDER BY role_id ASC`).WithArgs("default", 1).WillReturnRows(sqlmock.NewRows([]string{"role_id"}).AddRow("role-super-admin"))
	mock.ExpectQuery(`SELECT ur\.role_id FROM user_roles AS ur JOIN roles AS r .*`).WithArgs("default", 1, "active").WillReturnRows(sqlmock.NewRows([]string{"role_id"}).AddRow("role-super-admin"))
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"})
	service := iamapp.NewServiceWithRepositories(persistence, persistence, persistence, persistence, persistence, persistence)
	user, err := service.GetAuthorizationUser(ctx, "1")
	if err != nil {
		t.Fatalf("GetAuthorizationUser=%v", err)
	}
	subject, err := service.ResolveSubject(ctx, user)
	if err != nil {
		t.Fatalf("ResolveSubject=%v", err)
	}
	if len(subject.RoleIDs) != 1 || subject.RoleIDs[0] != "role-super-admin" {
		t.Fatalf("subject=%+v", subject)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations=%v", err)
	}
}
