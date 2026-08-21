package installplatform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	installstate "example.com/gin-vben-admin/server/internal/domain/installstate"
)

func TestSystemAssetBuilderUsesAllowlistedModeAndReturnsManifestDigest(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "build.mjs"), []byte("// fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, ".runtime", "build", "standalone", "artifact-manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := []byte(`{"schema":1,"mode":"standalone","ui":"antd"}`)
	if err := os.WriteFile(manifestPath, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	var got []string
	runner := func(_ context.Context, command string, args ...string) ([]byte, error) {
		got = append([]string{command}, args...)
		return []byte("BUILD_ARTIFACT=.runtime/build/standalone\nBUILD_OK\n"), nil
	}
	builder := newSystemAssetBuilder(root, "node", runner)
	digest, err := builder.Build(context.Background(), installstate.UIAntd, installstate.ModeStandalone)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	wantBytes := sha256.Sum256(contents)
	if digest != hex.EncodeToString(wantBytes[:]) {
		t.Fatalf("digest = %q, want manifest digest", digest)
	}
	if strings.Join(got, " ") != "node scripts/build.mjs --mode standalone --ui antd" {
		t.Fatalf("command = %q", strings.Join(got, " "))
	}
}

func TestSystemAssetBuilderRejectsUntrustedArtifactOutput(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "build.mjs"), []byte("// fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("BUILD_ARTIFACT=../outside\nBUILD_OK\n"), nil
	}
	builder := newSystemAssetBuilder(root, "node", runner)
	if _, err := builder.Build(context.Background(), installstate.UIAntd, installstate.ModeStandalone); err == nil {
		t.Fatal("Build() error = nil, want bounded artifact failure")
	}
}
