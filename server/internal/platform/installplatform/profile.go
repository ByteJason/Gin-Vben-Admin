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
	root                     string
	profilePath              string
	localProfilePath         string
	workspaceManifestPath    string
	transactionPath          string
	workspaceTransactionPath string
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
		root:                     filepath.Clean(canonical),
		profilePath:              filepath.Join(canonical, "admin", ".ui-profile.json"),
		localProfilePath:         filepath.Join(canonical, "admin", ".ui-profile.local.json"),
		workspaceManifestPath:    filepath.Join(canonical, "admin", "pnpm-workspace.yaml"),
		transactionPath:          filepath.Join(filepath.Clean(stateDirectory), "transaction.json"),
		workspaceTransactionPath: filepath.Join(filepath.Clean(stateDirectory), "workspace-transaction.json"),
	}, nil
}

func (p *FileProfileProvider) Profile(ctx context.Context) (installer.InstallationProfile, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return installer.InstallationProfile{}, false, err
	}
	if p == nil || p.root == "" || p.profilePath == "" || p.localProfilePath == "" || p.workspaceManifestPath == "" || p.transactionPath == "" || p.workspaceTransactionPath == "" {
		return installer.InstallationProfile{}, false, ErrWorkspaceRootInvalid
	}
	workspaceMode, manifestStateOK := workspaceManifestState(p.workspaceManifestPath)
	if !manifestStateOK {
		return installer.InstallationProfile{}, true, nil
	}

	// Resolve the durable selector once so the workspace journal, server apply
	// journal, and final status all observe the same bytes. The ignored local
	// selector is strict and takes precedence over the tracked legacy profile.
	localContents, localExists, localErr := readRegularFile(p.localProfilePath, maxUIProfileBytes)
	if localErr != nil {
		if ctx.Err() != nil {
			return installer.InstallationProfile{}, false, ctx.Err()
		}
		return installer.InstallationProfile{}, true, nil
	}
	profileContents := localContents
	profileExists := localExists
	independentUISelection := localExists
	if localExists && !workspaceMode {
		return installer.InstallationProfile{}, true, nil
	}
	if !localExists {
		var trackedErr error
		profileContents, profileExists, trackedErr = readRegularFile(p.profilePath, maxUIProfileBytes)
		if trackedErr != nil {
			if ctx.Err() != nil {
				return installer.InstallationProfile{}, false, ctx.Err()
			}
			return installer.InstallationProfile{}, true, nil
		}
	}
	var profile uiProfileDefinition
	profileValid := false
	if profileExists {
		profile, profileValid = decodeTrackedUIProfile(profileContents)
		if profileValid {
			switch {
			case localExists:
				profileValid = p.validTemplateLayout(profile, true)
			case workspaceMode && p.validTemplateLayout(profile, true):
				// A tracked compatibility profile can coexist with the new
				// all-template checkout until each clone writes its local choice.
				independentUISelection = true
			default:
				// Old installed checkouts used the same pnpm workspace manifest
				// but intentionally retained only the selected template.
				profileValid = p.validTemplateLayout(profile, false)
			}
		}
	}

	// The workspace selector uses a separate, zero-move journal. Keeping it
	// separate from the historical source-move journal lets old transactions
	// resume without changing their validation contract.
	workspaceContents, workspaceExists, workspaceErr := readRegularFile(p.workspaceTransactionPath, maxUIProfileBytes)
	if workspaceErr != nil {
		if ctx.Err() != nil {
			return installer.InstallationProfile{}, false, ctx.Err()
		}
		return installer.InstallationProfile{}, true, nil
	}
	if workspaceExists {
		transaction, ok := decodeWorkspaceTransaction(workspaceContents)
		if !ok {
			return installer.InstallationProfile{}, true, nil
		}
		definition := uiProfileDefinitions[string(transaction.SelectedUI)]
		if !p.validTemplateLayout(definition, true) {
			return installer.InstallationProfile{}, true, nil
		}
		profileMismatch := transaction.Phase == "dependencies_pending" && profile.ui != transaction.SelectedUI
		if profileExists && (!profileValid || profileMismatch) {
			return installer.InstallationProfile{}, true, nil
		}
		return installer.InstallationProfile{
			SelectedUI:             transaction.SelectedUI,
			Installing:             true,
			PreparingUI:            true,
			UIAction:               installer.UIPreparationActionPrepare,
			IndependentUISelection: true,
		}, true, nil
	}
	if workspaceMode && !profileExists && !p.validTemplateLayout(uiProfileDefinitions["antd"], true) {
		return installer.InstallationProfile{}, true, nil
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

	if !profileExists {
		if serverTransaction != nil {
			return installer.InstallationProfile{}, true, nil
		}
		return installer.InstallationProfile{}, false, nil
	}
	if !profileValid {
		return installer.InstallationProfile{}, true, nil
	}
	if serverTransaction != nil && serverTransaction.SelectedUI != profile.ui {
		return installer.InstallationProfile{}, true, nil
	}
	return installer.InstallationProfile{
		SelectedUI:             profile.ui,
		Installing:             serverTransaction != nil,
		IndependentUISelection: independentUISelection,
	}, true, nil
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

type workspaceInitTransaction struct {
	Schema     int             `json:"schema"`
	Owner      string          `json:"owner"`
	ID         string          `json:"id"`
	SelectedUI installstate.UI `json:"selectedUi"`
	Phase      string          `json:"phase"`
	Moves      []adminInitMove `json:"moves"`
}

func decodeWorkspaceTransaction(contents []byte) (workspaceInitTransaction, bool) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var transaction workspaceInitTransaction
	if err := decoder.Decode(&transaction); err != nil || ensureJSONEnd(decoder) != nil {
		return workspaceInitTransaction{}, false
	}
	if transaction.Schema != 1 || transaction.Owner != "admin-init-workspace" || !validAdminInitTransactionID(transaction.ID) || (transaction.Phase != "dependencies_pending" && transaction.Phase != "switching_ui") || len(transaction.Moves) != 0 {
		return workspaceInitTransaction{}, false
	}
	if _, ok := uiProfileDefinitions[string(transaction.SelectedUI)]; !ok {
		return workspaceInitTransaction{}, false
	}
	return transaction, true
}

// workspaceManifestState distinguishes a real workspace manifest from a
// missing compatibility marker. A symlink or unreadable entry is an existing
// but invalid repository state and is surfaced as an inconsistent profile.
func workspaceManifestState(path string) (workspace bool, valid bool) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, true
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, false
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return true, false
	}
	for _, line := range strings.Split(string(contents), "\n") {
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSpace(line)
		if line == "- apps/*" || line == "- 'apps/*'" || line == `- "apps/*"` {
			return true, true
		}
	}
	return true, false
}

func (p *FileProfileProvider) validTemplateLayout(selected uiProfileDefinition, requireAll bool) bool {
	for name, candidate := range uiProfileDefinitions {
		path := filepath.Join(p.root, "admin", filepath.FromSlash(candidate.appDirectory))
		info, err := os.Lstat(path)
		if name == string(selected.ui) {
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return false
			}
			if !requireAll {
				continue
			}
			manifestPath := filepath.Join(path, "package.json")
			manifestInfo, manifestErr := os.Lstat(manifestPath)
			if manifestErr != nil || !manifestInfo.Mode().IsRegular() || manifestInfo.Mode()&os.ModeSymlink != 0 {
				return false
			}
			contents, readErr := os.ReadFile(manifestPath)
			var manifest struct {
				Name string `json:"name"`
			}
			if readErr != nil || json.Unmarshal(contents, &manifest) != nil || manifest.Name != candidate.packageName {
				return false
			}
			continue
		}
		if errors.Is(err, os.ErrNotExist) && !requireAll {
			continue
		}
		if errors.Is(err, os.ErrNotExist) {
			return false
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		manifestPath := filepath.Join(path, "package.json")
		manifestInfo, manifestErr := os.Lstat(manifestPath)
		if requireAll {
			if manifestErr != nil || !manifestInfo.Mode().IsRegular() || manifestInfo.Mode()&os.ModeSymlink != 0 {
				return false
			}
			contents, readErr := os.ReadFile(manifestPath)
			var manifest struct {
				Name string `json:"name"`
			}
			if readErr != nil || json.Unmarshal(contents, &manifest) != nil || manifest.Name != candidate.packageName {
				return false
			}
		} else if !errors.Is(manifestErr, os.ErrNotExist) {
			// A fast-forward pull may restore only changed tracked files below an
			// unselected UI path. Without package.json it is not a pnpm workspace
			// or a runnable template, so it must not invalidate the selected UI.
			return false
		}
	}
	return true
}
