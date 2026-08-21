package i18n

import "testing"

func TestCatalogResolvesWeightedLanguageAndFallsBack(t *testing.T) {
	catalog, err := NewCatalog("zh-CN", map[string]map[string]string{
		"zh-CN": {"auth.invalid": "凭据无效"},
		"en-US": {"auth.invalid": "Invalid credentials"},
	})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	if got := catalog.Resolve("fr-FR;q=0.9,en-US;q=0.8"); got != "en-US" {
		t.Fatalf("Resolve() = %q, want en-US", got)
	}
	if got := catalog.Translate("fr-FR", "auth.invalid"); got != "凭据无效" {
		t.Fatalf("fallback translation = %q", got)
	}
	if got := catalog.Translate("zh-CN", "missing.key"); got != "missing.key" {
		t.Fatalf("missing key = %q", got)
	}
}

func TestCatalogRejectsUnsupportedFallback(t *testing.T) {
	if _, err := NewCatalog("fr-FR", map[string]map[string]string{"zh-CN": {}}); err == nil {
		t.Fatal("expected unsupported fallback error")
	}
}
