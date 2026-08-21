package installplatform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	installstate "example.com/gin-vben-admin/server/internal/domain/installstate"
)

type assetCommandRunner func(context.Context, string, ...string) ([]byte, error)

// SystemAssetBuilder is the source-install bridge to the root build entry.
// It accepts only a fixed script and fixed enum arguments; it never invokes a
// shell and never accepts a browser-provided command.
type SystemAssetBuilder struct {
	root string
	node string
	run  assetCommandRunner
}

func NewSystemAssetBuilder(root string) *SystemAssetBuilder {
	return newSystemAssetBuilder(root, "node", nil)
}

func newSystemAssetBuilder(root, node string, runner assetCommandRunner) *SystemAssetBuilder {
	if runner == nil {
		runner = func(ctx context.Context, command string, args ...string) ([]byte, error) {
			if _, err := exec.LookPath(command); err != nil {
				return nil, err
			}
			process := exec.CommandContext(ctx, command, args...)
			process.Dir = root
			return process.CombinedOutput()
		}
	}
	return &SystemAssetBuilder{root: filepath.Clean(root), node: node, run: runner}
}

func (s *SystemAssetBuilder) Build(ctx context.Context, ui installstate.UI, mode installstate.Mode) (string, error) {
	if s == nil || s.run == nil || strings.TrimSpace(s.root) == "" || strings.TrimSpace(s.node) == "" {
		return "", ErrAssetInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !validAssetSelection(ui, mode) {
		return "", ErrAssetInstallation
	}
	script := filepath.Join(s.root, "scripts", "build.mjs")
	info, err := os.Lstat(script)
	if err != nil || !info.Mode().IsRegular() {
		return "", ErrAssetInstallation
	}
	output, err := s.run(ctx, s.node, "scripts/build.mjs", "--mode", string(mode), "--ui", string(ui))
	if err != nil || !strings.Contains(string(output), "BUILD_OK") {
		return "", ErrAssetInstallation
	}
	artifact, err := parseBuildArtifact(string(output))
	if err != nil {
		if mode == installstate.ModeDev {
			digest := sha256.Sum256(output)
			return hex.EncodeToString(digest[:]), nil
		}
		return "", ErrAssetInstallation
	}
	absolute, err := s.boundedArtifact(artifact)
	if err != nil {
		return "", ErrAssetInstallation
	}
	info, err = os.Lstat(absolute)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrAssetInstallation
	}
	if info.IsDir() {
		manifest := filepath.Join(absolute, "artifact-manifest.json")
		manifestInfo, manifestErr := os.Lstat(manifest)
		if manifestErr != nil || !manifestInfo.Mode().IsRegular() {
			return "", ErrAssetInstallation
		}
		return digestFile(manifest)
	}
	if !info.Mode().IsRegular() {
		return "", ErrAssetInstallation
	}
	return digestFile(absolute)
}

func parseBuildArtifact(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), "BUILD_ARTIFACT="); found && strings.TrimSpace(value) != "" {
			return filepath.FromSlash(strings.TrimSpace(value)), nil
		}
	}
	return "", errors.New("build artifact was not reported")
}

func (s *SystemAssetBuilder) boundedArtifact(value string) (string, error) {
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(filepath.Join(root, value))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("build artifact escapes workspace")
	}
	portable := filepath.ToSlash(relative)
	if !strings.HasPrefix(portable, ".runtime/build/") && !strings.HasPrefix(portable, "server/bin/") {
		return "", errors.New("build artifact is outside runtime build directory")
	}
	return absolute, nil
}

func digestFile(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read build manifest: %w", err)
	}
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:]), nil
}
