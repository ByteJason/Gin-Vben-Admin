package webassets

import "testing"

func TestOrdinaryBuildDoesNotExposeEmbeddedAssets(t *testing.T) {
	assets, available := Static()
	if available || assets != nil {
		t.Fatalf("Static() = (%T, %t), want (nil, false)", assets, available)
	}
}
