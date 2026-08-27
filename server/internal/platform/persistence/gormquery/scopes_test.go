package gormquery

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type defaultCreateProbe struct {
	ID      string `gorm:"column:id;primaryKey"`
	Enabled bool   `gorm:"column:enabled;default:true"`
	Weight  int    `gorm:"column:weight;default:1"`
}

func (defaultCreateProbe) TableName() string { return "default_create_probes" }

type scopeProbe struct {
	ID       uint   `gorm:"column:id;primaryKey"`
	TenantID string `gorm:"column:tenant_id"`
	OrgID    string `gorm:"column:org_id"`
	Status   string `gorm:"column:status"`
	Deleted  *int   `gorm:"column:deleted_at"`
}

func (scopeProbe) TableName() string { return "scope_probes" }

func TestScopesComposeWithGenerics(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery(`SELECT .* FROM "scope_probes" WHERE .*tenant_id.*org_id.*status.*deleted_at.*kind.*LIMIT`).
		WithArgs("tenant-a", "org-a", "active", "probe", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "org_id", "status", "deleted_at"}).AddRow(1, "tenant-a", "org-a", "active", nil))
	_, err = gorm.G[scopeProbe](db).
		Scopes(Tenant("tenant-a"), Organization("org-a"), Active(), NotDeleted()).
		Where("kind = ?", "probe").
		Take(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestScopeValuesAreParameterBound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	value := "tenant' OR 1=1"
	mock.ExpectQuery(`SELECT .* FROM "scope_probes" WHERE .*tenant_id.*LIMIT`).
		WithArgs(value, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "org_id", "status", "deleted_at"}).AddRow(1, value, "", "active", nil))
	if _, err := gorm.G[scopeProbe](db).Scopes(Tenant(value)).Take(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateValuesPreservesExplicitZeroValues(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "default_create_probes"`).WithArgs(false, "probe-1", 0).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	if err := CreateValues[defaultCreateProbe](context.Background(), db, map[string]any{"id": "probe-1", "enabled": false, "weight": 0}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
