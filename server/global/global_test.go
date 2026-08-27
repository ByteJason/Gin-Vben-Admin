package global

import "testing"

func TestDriverAliasesArePublishedCanonically(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	SetDriver(" pgsql ")
	if got := GetDriver(); got != "postgres" {
		t.Fatalf("GetDriver() = %q, want postgres", got)
	}
	SetDriver("MYSQL")
	if got := GetDriver(); got != "mysql" {
		t.Fatalf("GetDriver() = %q, want mysql", got)
	}
}
