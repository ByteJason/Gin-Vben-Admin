package migrations

import (
	"bytes"
	"log"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPostgresTableCommentUsesParserSafeLiteral(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer sqlDB.Close()

	mock.ExpectExec(regexp.QuoteMeta(`COMMENT ON TABLE "users" IS '用''户'`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	database, err := gorm.Open(
		gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
		&gorm.Config{DisableAutomaticPing: true},
	)
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	if err := commentPostgresTable(database, "users", "用'户"); err != nil {
		t.Fatalf("commentPostgresTable() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("PostgreSQL driver SQL = %v", err)
	}
}

func TestCreateTableDDLHasCommentsAndNoForeignKeys(t *testing.T) {
	tests := []struct {
		name      string
		dialector func(*testing.T) (gorm.Dialector, func())
		wantTable string
		wantField string
	}{
		{
			name: "mysql",
			dialector: func(t *testing.T) (gorm.Dialector, func()) {
				sqlDB, _, err := sqlmock.New()
				if err != nil {
					t.Fatalf("sqlmock.New() error = %v", err)
				}
				return gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), func() { _ = sqlDB.Close() }
			},
			wantTable: "COMMENT='用户'",
			wantField: "COMMENT '用户标识'",
		},
		{
			name: "postgres",
			dialector: func(t *testing.T) (gorm.Dialector, func()) {
				sqlDB, _, err := sqlmock.New()
				if err != nil {
					t.Fatalf("sqlmock.New() error = %v", err)
				}
				return gormpostgres.New(gormpostgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), func() { _ = sqlDB.Close() }
			},
			wantTable: `COMMENT ON TABLE "users" IS '用户'`,
			wantField: `COMMENT ON COLUMN "users"."id" IS '用户标识'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialector, closeDatabase := tt.dialector(t)
			defer closeDatabase()
			var output bytes.Buffer
			database, err := gorm.Open(dialector, &gorm.Config{
				DryRun:                                   true,
				DisableAutomaticPing:                     true,
				DisableForeignKeyConstraintWhenMigrating: true,
				Logger: logger.New(log.New(&output, "", 0), logger.Config{
					LogLevel: logger.Info,
				}),
			})
			if err != nil {
				t.Fatalf("gorm.Open() error = %v", err)
			}
			for _, model := range Models() {
				if err := createTable(database, model); err != nil {
					t.Fatalf("createTable(%T) error = %v", model, err)
				}
			}

			ddl := output.String()
			if got := strings.Count(ddl, "CREATE TABLE"); got != len(Models()) {
				t.Fatalf("CREATE TABLE count = %d, want %d", got, len(Models()))
			}
			for _, fragment := range []string{tt.wantTable, tt.wantField} {
				if !strings.Contains(ddl, fragment) {
					t.Fatalf("DDL is missing %q", fragment)
				}
			}
			upper := strings.ToUpper(ddl)
			for _, forbidden := range []string{"FOREIGN KEY", "REFERENCES "} {
				if strings.Contains(upper, forbidden) {
					t.Fatalf("DDL contains %q", forbidden)
				}
			}
		})
	}
}
