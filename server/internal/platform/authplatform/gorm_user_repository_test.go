package authplatform

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGORMUserRepositoryFindByIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		result     queryResult
		want       authdomain.User
		wantErr    error
		wantText   string
	}{
		{name: "active user", identifier: "alice", result: queryResult{rows: [][]driver.Value{{int64(7), "alice", "hash", "active"}}}, want: authdomain.User{ID: "7", Identifier: "alice", Username: "alice", PasswordHash: "hash", Active: true}},
		{name: "missing user", identifier: "nobody", result: queryResult{}, wantErr: authdomain.ErrInvalidCredentials},
		{name: "database error is sanitized", identifier: "error", result: queryResult{err: errors.New("pq: password leaked in SQL detail")}, wantErr: ErrUserLookup, wantText: "user lookup failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t, tt.result)
			repo := NewGORMUserRepository(store)
			got, err := repo.FindByIdentifier(tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"}), tt.identifier)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("FindByIdentifier() error = %v, want %v", err, tt.wantErr)
				}
				if tt.wantText != "" && err.Error() != tt.wantText {
					t.Fatalf("FindByIdentifier() error text = %q, want %q", err, tt.wantText)
				}
				if strings.Contains(err.Error(), "password leaked") {
					t.Fatalf("FindByIdentifier() leaked database detail: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("FindByIdentifier() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("FindByIdentifier() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestGORMUserRepositoryFindByIdentifierMySQLDialect(t *testing.T) {
	store := newTestStoreWithDialect(t, queryResult{rows: [][]driver.Value{{int64(8), "bob", "hash", "disabled"}}}, "mysql")
	got, err := NewGORMUserRepository(store).FindByIdentifier(tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"}), "bob")
	if err != nil {
		t.Fatalf("FindByIdentifier() error = %v", err)
	}
	want := authdomain.User{ID: "8", Identifier: "bob", Username: "bob", PasswordHash: "hash", Active: false}
	if got != want {
		t.Fatalf("FindByIdentifier() = %+v, want %+v", got, want)
	}
}

func TestGORMUserRepositoryFindsEmailAndReturnsProfileFields(t *testing.T) {
	store := newTestStoreWithDialect(t, queryResult{
		columns: []string{"id", "tenant_id", "username", "username_normalized", "email", "email_normalized", "nickname", "avatar", "phone", "password_hash", "status", "last_login_ip", "last_login_at", "password_changed_at", "org_id"},
		rows:    [][]driver.Value{{int64(9), "default", "Alice", "alice", "Alice@Example.TEST", "alice@example.test", "Alice A", "avatar-key", "+8613800138000", "hash", "active", "192.0.2.9", time.Unix(100, 0), time.Unix(90, 0), "org-1"}},
	}, "postgres")

	got, err := NewGORMUserRepository(store).FindByIdentifier(
		tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"}),
		"  ALICE@EXAMPLE.TEST ",
	)
	if err != nil {
		t.Fatalf("FindByIdentifier(email) error = %v", err)
	}
	want := authdomain.User{
		ID: "9", Identifier: "Alice", Username: "Alice", UsernameNormalized: "alice",
		Email: "Alice@Example.TEST", EmailNormalized: "alice@example.test",
		Nickname: "Alice A", Avatar: "avatar-key", Phone: "+8613800138000",
		PasswordHash: "hash", Active: true, LastLoginIP: "192.0.2.9",
		LastLoginAt: time.Unix(100, 0).UTC(), PasswordChangedAt: time.Unix(90, 0).UTC(),
		TenantID: "default", OrgID: "org-1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FindByIdentifier(email) = %+v, want %+v", got, want)
	}
}

func TestGORMUserRepositoryRejectsPhoneAsAuthenticationIdentifier(t *testing.T) {
	store := newTestStoreWithDialect(t, queryResult{err: errors.New("query should not run")}, "postgres")
	_, err := NewGORMUserRepository(store).FindByIdentifier(
		tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"}),
		"+8613800138000",
	)
	if !errors.Is(err, authdomain.ErrInvalidIdentifier) {
		t.Fatalf("FindByIdentifier(phone) error = %v, want ErrInvalidIdentifier", err)
	}
}

func TestUserRowFromDomainRejectsMalformedProfilePhone(t *testing.T) {
	_, err := userRowFromDomain(authdomain.User{Username: "alice", Phone: "13800138000", PasswordHash: "hash"}, "default")
	if !errors.Is(err, authdomain.ErrInvalidPhone) {
		t.Fatalf("userRowFromDomain() error = %v, want ErrInvalidPhone", err)
	}
}

func TestGORMUserRepositoryRequiresTenantContext(t *testing.T) {
	store := newTestStore(t, queryResult{rows: [][]driver.Value{{int64(7), "alice", "hash", "active"}}})
	_, err := NewGORMUserRepository(store).FindByIdentifier(context.Background(), "alice")
	if !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("FindByIdentifier() error = %v, want tenant context missing", err)
	}
}

var _ authdomain.UserRepository = (*GORMUserRepository)(nil)

type queryResult struct {
	columns []string
	rows    [][]driver.Value
	err     error
	execErr error
}

type testDriver struct{ result queryResult }
type testConn struct{ result queryResult }
type testRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (d testDriver) Open(string) (driver.Conn, error) { return testConn{result: d.result}, nil }
func (c testConn) Prepare(string) (driver.Stmt, error) {
	return testStmt{result: c.result}, nil
}
func (c testConn) Close() error              { return nil }
func (c testConn) Begin() (driver.Tx, error) { return testTx{}, nil }
func (c testConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	if c.result.err != nil {
		return nil, c.result.err
	}
	return &testRows{columns: resultColumns(c.result), rows: c.result.rows}, nil
}
func (c testConn) ExecContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	if c.result.execErr != nil {
		return nil, c.result.execErr
	}
	return testResult{}, nil
}

type testStmt struct{ result queryResult }

type testTx struct{}

func (testTx) Commit() error   { return nil }
func (testTx) Rollback() error { return nil }

func (s testStmt) Close() error  { return nil }
func (s testStmt) NumInput() int { return -1 }
func (s testStmt) Exec([]driver.Value) (driver.Result, error) {
	if s.result.execErr != nil {
		return nil, s.result.execErr
	}
	return testResult{}, nil
}
func (s testStmt) Query([]driver.Value) (driver.Rows, error) {
	if s.result.err != nil {
		return nil, s.result.err
	}
	return &testRows{columns: resultColumns(s.result), rows: s.result.rows}, nil
}

func resultColumns(result queryResult) []string {
	if len(result.columns) > 0 {
		return result.columns
	}
	if len(result.rows) > 0 && len(result.rows[0]) >= 5 {
		return []string{"id", "tenant_id", "username", "password_hash", "status"}
	}
	return []string{"id", "username", "password_hash", "status"}
}

type testResult struct{}

func (testResult) LastInsertId() (int64, error)             { return 1, nil }
func (testResult) RowsAffected() (int64, error)             { return 1, nil }
func (c testConn) CheckNamedValue(*driver.NamedValue) error { return nil }
func (r *testRows) Columns() []string                       { return r.columns }
func (r *testRows) Close() error                            { return nil }
func (r *testRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

var testDriverID atomic.Uint64

func newTestStore(t *testing.T, result queryResult) *gormdb.Store {
	return newTestStoreWithDialect(t, result, "postgres")
}

func newTestStoreWithDialect(t *testing.T, result queryResult, dialect string) *gormdb.Store {
	t.Helper()
	id := testDriverID.Add(1)
	name := fmt.Sprintf("authplatform_test_%d", id)
	sql.Register(name, testDriver{result: result})
	dbSQL, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	var dialector gorm.Dialector
	if dialect == "mysql" {
		dialector = gormmysql.New(gormmysql.Config{Conn: dbSQL, SkipInitializeWithVersion: true})
	} else {
		dialector = gormpostgres.New(gormpostgres.Config{Conn: dbSQL, PreferSimpleProtocol: true})
	}
	gdb, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	store := &gormdb.Store{}
	field := reflect.ValueOf(store).Elem().FieldByName("database")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(gdb))
	t.Cleanup(func() { _ = dbSQL.Close() })
	return store
}
