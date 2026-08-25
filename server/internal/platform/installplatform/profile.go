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

type adminInitTransaction struct {
	Schema     int             `json:"schema"`
	Owner      string          `json:"owner"`
	ID         string          `json:"id"`
	SelectedUI installstate.UI `json:"selectedUi"`
	Phase      string          `json:"phase"`
	Moves      []adminInitMove `json:"moves"`
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

	var serverTransaction *installer.ApplyTransaction
	transactionContents, transactionExists, transactionErr := readRegularFile(p.transactionPath, maxApplyTransactionBytes)
	if transactionErr != nil {
		if ctx.Err() != nil {
			return installer.InstallationProfile{}, false, ctx.Err()
		}
		return installer.InstallationProfile{}, true, nil
	}
	if transactionExists {
		var envelope struct {
			Owner string `json:"owner"`
		}
		if json.Unmarshal(transactionContents, &envelope) != nil {
			return installer.InstallationProfile{}, true, nil
		}
		switch envelope.Owner {
		case "admin-init":
			transaction, ok := decodeAdminInitTransaction(transactionContents)
			if !ok {
				return installer.InstallationProfile{}, true, nil
			}
			action := installer.UIPreparationActionPrepare
			if transaction.Phase == "resetting_ui" {
				action = installer.UIPreparationActionReset
			}
			return installer.InstallationProfile{
				SelectedUI: transaction.SelectedUI,
				Installing: true, PreparingUI: true, UIAction: action,
			}, true, nil
		case installer.ApplyTransactionOwner:
			journal := NewFileTransactionJournal(p.transactionPath, p.root)
			transaction, exists, err := journal.Load(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return installer.InstallationProfile{}, false, ctx.Err()
				}
				return installer.InstallationProfile{}, true, nil
			}
			if !exists {
				return installer.InstallationProfile{}, true, nil
			}
			serverTransaction = &transaction
		default:
			return installer.InstallationProfile{}, true, nil
		}
	}

	contents, exists, err := readRegularFile(p.profilePath, maxUIProfileBytes)
	if !exists && err == nil {
		if serverTransaction != nil {
			return installer.InstallationProfile{}, true, nil
		}
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
	if serverTransaction != nil && serverTransaction.SelectedUI != profile.ui {
		return installer.InstallationProfile{}, true, nil
	}
	return installer.InstallationProfile{SelectedUI: profile.ui}, true, nil
}

func decodeAdminInitTransaction(contents []byte) (adminInitTransaction, bool) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var transaction adminInitTransaction
	if err := decoder.Decode(&transaction); err != nil || ensureJSONEnd(decoder) != nil {
		return adminInitTransaction{}, false
	}
	if transaction.Schema != 1 || transaction.Owner != "admin-init" || !validAdminInitTransactionID(transaction.ID) {
		return adminInitTransaction{}, false
	}
	if _, ok := uiProfileDefinitions[string(transaction.SelectedUI)]; !ok {
		return adminInitTransaction{}, false
	}
	switch transaction.Phase {
	case "moving_ui", "dependencies_pending", "resetting_ui":
	default:
		return adminInitTransaction{}, false
	}
	if !equalAdminInitMoves(transaction.Moves, expectedAdminInitMoves(transaction.SelectedUI)) {
		return adminInitTransaction{}, false
	}
	return transaction, true
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
