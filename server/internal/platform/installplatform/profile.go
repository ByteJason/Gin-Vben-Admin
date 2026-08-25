package installplatform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

const maxUIProfileBytes = 8 << 10

type trackedUIProfile struct {
	Schema       int    `json:"schema"`
	SelectedUI   string `json:"selectedUi"`
	PackageName  string `json:"packageName"`
	AppDirectory string `json:"appDirectory"`
}

type uiProfileDefinition struct {
	ui           installstate.UI
	packageName  string
	appDirectory string
}

var uiProfileDefinitions = map[string]uiProfileDefinition{
	"antd":  {ui: installstate.UIAntd, packageName: "@vben/web-antd", appDirectory: "apps/web-antd"},
	"ele":   {ui: installstate.UIEle, packageName: "@vben/web-ele", appDirectory: "apps/web-ele"},
	"naive": {ui: installstate.UINaive, packageName: "@vben/web-naive", appDirectory: "apps/web-naive"},
}

// FileProfileProvider reads the deterministic, source-controlled UI profile
// written by admin/scripts/init.mjs. Invalid on-disk state is represented as
// exists=true with an empty profile so the public status becomes inconsistent
// rather than leaking a local filesystem failure.
type FileProfileProvider struct {
	root            string
	profilePath     string
	transactionPath string
}

func NewFileProfileProvider(workspaceRoot string) (*FileProfileProvider, error) {
	return newFileProfileProvider(workspaceRoot, "")
}

func NewFileProfileProviderWithStateDirectory(workspaceRoot, stateDirectory string) (*FileProfileProvider, error) {
	return newFileProfileProvider(workspaceRoot, stateDirectory)
}

func newFileProfileProvider(workspaceRoot, configuredStateDirectory string) (*FileProfileProvider, error) {
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil, ErrWorkspaceRootRequired
	}
	abs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, ErrWorkspaceRootInvalid
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return nil, ErrWorkspaceRootInvalid
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, ErrWorkspaceRootInvalid
	}
	stateDirectory := filepath.Join(canonical, ".runtime", "install")
	if strings.TrimSpace(configuredStateDirectory) != "" {
		stateDirectory, err = filepath.Abs(configuredStateDirectory)
		if err != nil {
			return nil, ErrWorkspaceRootInvalid
		}
	}
	return &FileProfileProvider{
		root:            filepath.Clean(canonical),
		profilePath:     filepath.Join(canonical, "admin", ".ui-profile.json"),
		transactionPath: filepath.Join(filepath.Clean(stateDirectory), "transaction.json"),
	}, nil
}

func (p *FileProfileProvider) Profile(ctx context.Context) (installer.InstallationProfile, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return installer.InstallationProfile{}, false, err
	}
	if p == nil || p.root == "" || p.profilePath == "" || p.transactionPath == "" {
		return installer.InstallationProfile{}, false, ErrWorkspaceRootInvalid
	}

	contents, exists, err := readRegularFile(p.profilePath, maxUIProfileBytes)
	if !exists && err == nil {
		return installer.InstallationProfile{}, false, nil
	}
	if err != nil {
		if _, inspectErr := os.Lstat(p.profilePath); errors.Is(inspectErr, os.ErrNotExist) {
			return installer.InstallationProfile{}, false, nil
		}
		return installer.InstallationProfile{}, true, nil
	}
	profile, ok := decodeTrackedUIProfile(contents)
	if !ok || !p.validTemplateLayout(profile) {
		return installer.InstallationProfile{}, true, nil
	}
	// A CLI-owned transaction means pnpm init has not completed dependency
	// preparation yet. Reporting ui_prepared here lets the browser enqueue an
	// apply job that can only fail immediately with installation_running. A
	// valid server-owned journal remains compatible with retry/recovery.
	journal := NewFileTransactionJournal(p.transactionPath, p.root)
	transaction, transactionExists, transactionErr := journal.Load(ctx)
	if transactionErr != nil {
		if ctx.Err() != nil {
			return installer.InstallationProfile{}, false, ctx.Err()
		}
		return installer.InstallationProfile{}, true, nil
	}
	if transactionExists && transaction.SelectedUI != profile.ui {
		return installer.InstallationProfile{}, true, nil
	}
	// An existing journal reaching here is a validated server-installer envelope.
	return installer.InstallationProfile{SelectedUI: profile.ui}, true, nil
}

func decodeTrackedUIProfile(contents []byte) (uiProfileDefinition, bool) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var value trackedUIProfile
	if err := decoder.Decode(&value); err != nil || ensureJSONEnd(decoder) != nil || value.Schema != 1 {
		return uiProfileDefinition{}, false
	}
	expected, ok := uiProfileDefinitions[value.SelectedUI]
	if !ok || value.PackageName != expected.packageName || value.AppDirectory != expected.appDirectory {
		return uiProfileDefinition{}, false
	}
	return expected, true
}

func (p *FileProfileProvider) validTemplateLayout(selected uiProfileDefinition) bool {
	for name, candidate := range uiProfileDefinitions {
		path := filepath.Join(p.root, "admin", filepath.FromSlash(candidate.appDirectory))
		info, err := os.Lstat(path)
		if name == string(selected.ui) {
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return false
			}
			continue
		}
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		// A fast-forward pull may restore only changed tracked files below an
		// unselected UI path. Without package.json it is not a pnpm workspace
		// or a runnable template, so it must not invalidate the selected UI.
		if _, manifestErr := os.Lstat(filepath.Join(path, "package.json")); !errors.Is(manifestErr, os.ErrNotExist) {
			return false
		}
	}
	return true
}
