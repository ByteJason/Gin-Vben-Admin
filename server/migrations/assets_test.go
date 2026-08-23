package migrations_test

import (
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/migrations"
)

func TestEmbeddedMigrationsHaveMatchedUpAndDownFiles(t *testing.T) {
	t.Parallel()

	for _, driver := range []string{"mysql", "postgres"} {
		entries, err := fs.ReadDir(migrations.FS, driver)
		if err != nil {
			t.Fatalf("read %s migration assets: %v", driver, err)
		}

		up := make(map[string]struct{})
		down := make(map[string]struct{})
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			switch {
			case strings.HasSuffix(entry.Name(), ".up.sql"):
				up[strings.TrimSuffix(entry.Name(), ".up.sql")] = struct{}{}
			case strings.HasSuffix(entry.Name(), ".down.sql"):
				down[strings.TrimSuffix(entry.Name(), ".down.sql")] = struct{}{}
			}
		}

		if len(up) == 0 {
			t.Fatalf("%s has no up migrations", driver)
		}
		if len(up) != len(down) {
			t.Fatalf("%s migration pairs: up=%d down=%d", driver, len(up), len(down))
		}
		for version := range up {
			if _, ok := down[version]; !ok {
				t.Fatalf("%s missing down migration for %s", driver, version)
			}
		}
	}
}

func TestFirstMigrationCreatesVersionedMetadata(t *testing.T) {
	t.Parallel()

	for _, driver := range []string{"mysql", "postgres"} {
		contents, err := fs.ReadFile(migrations.FS, driver+"/000001_app_metadata.up.sql")
		if err != nil {
			t.Fatalf("read %s first migration: %v", driver, err)
		}
		sql := strings.ToLower(string(contents))
		for _, token := range []string{"create table", "app_metadata", "metadata_key", "metadata_value", "version", "created_at", "updated_at", "insert"} {
			if !strings.Contains(sql, token) {
				t.Fatalf("%s migration missing %q", driver, token)
			}
		}
	}
}

func TestMigrationVersionsAreAlignedAcrossDrivers(t *testing.T) {
	t.Parallel()

	versions := make(map[string][]string)
	for _, driver := range []string{"mysql", "postgres"} {
		entries, err := fs.ReadDir(migrations.FS, driver)
		if err != nil {
			t.Fatalf("read %s: %v", driver, err)
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".up.sql") {
				versions[driver] = append(versions[driver], strings.TrimSuffix(entry.Name(), ".up.sql"))
			}
		}
		sort.Strings(versions[driver])
	}

	if strings.Join(versions["mysql"], ",") != strings.Join(versions["postgres"], ",") {
		t.Fatalf("driver versions differ: mysql=%v postgres=%v", versions["mysql"], versions["postgres"])
	}
}
