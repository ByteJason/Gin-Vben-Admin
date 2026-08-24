package installplatform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

const maxAdminInitReceiptBytes = 16 << 10

type adminInitMove struct {
	Source string `json:"source"`
	Backup string `json:"backup"`
}

type adminInitReceipt struct {
	Schema            int             `json:"schema"`
	Owner             string          `json:"owner"`
	TransactionID     string          `json:"transactionId"`
	SelectedUI        installstate.UI `json:"selectedUi"`
	DependenciesReady bool            `json:"dependenciesReady"`
	Moves             []adminInitMove `json:"moves"`
}

// CleanupCompleted removes one unambiguous admin-init backup only after a
// valid installed marker exists. Invalid, unknown, ambiguous, or symlinked
// entries are preserved. The ui-backup root itself is never removed.
func (s *FileTransactionJournal) CleanupCompleted(ctx context.Context, marker installstate.Marker) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.workspaceRoot == "" || marker.Validate() != nil {
		return nil
	}
	provider, err := NewFileProfileProvider(s.workspaceRoot)
	if err != nil {
		return nil
	}
	profile, exists, err := provider.Profile(ctx)
	if err != nil || !exists || profile.SelectedUI != marker.SelectedUI {
		return nil
	}
	backupRoot := filepath.Join(filepath.Dir(s.path), "ui-backup")
	rootInfo, err := os.Lstat(backupRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	canonicalRoot, err := filepath.EvalSymlinks(backupRoot)
	if err != nil {
		return nil
	}
	canonicalRoot, err = filepath.Abs(canonicalRoot)
	if err != nil {
		return nil
	}
	stateDir := filepath.Dir(s.path)
	release, err := acquireProcessLeaseWithGuard(
		filepath.Join(stateDir, "ui-backup.cleanup.lock"),
		filepath.Join(stateDir, "process.guard"),
	)
	if errors.Is(err, errProcessLeaseBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()

	if err := cleanupAdminInitTombstones(backupRoot, canonicalRoot, marker.SelectedUI); err != nil {
		return err
	}
	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		return err
	}
	matches := make([]string, 0, 1)
	for _, entry := range entries {
		candidate := filepath.Join(backupRoot, entry.Name())
		if validAdminInitBackup(candidate, canonicalRoot, entry.Name(), marker.SelectedUI) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return nil
	}
	transactionID := filepath.Base(matches[0])
	tombstone := filepath.Join(backupRoot, adminInitCleanupTombstoneName(marker.SelectedUI, transactionID))
	if _, err := os.Lstat(tombstone); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(matches[0], tombstone); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := syncDirectory(backupRoot); err != nil {
		return err
	}
	if !validAdminInitCleanupTombstone(tombstone, canonicalRoot, filepath.Base(tombstone), marker.SelectedUI) {
		return nil
	}
	if err := removeTreeWithoutFollowing(tombstone, canonicalRoot); err != nil {
		return err
	}
	return syncDirectory(backupRoot)
}

func cleanupAdminInitTombstones(backupRoot, canonicalRoot string, selectedUI installstate.UI) error {
	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		return err
	}
	removed := false
	for _, entry := range entries {
		candidate := filepath.Join(backupRoot, entry.Name())
		if !validAdminInitCleanupTombstone(candidate, canonicalRoot, entry.Name(), selectedUI) {
			continue
		}
		if err := removeTreeWithoutFollowing(candidate, canonicalRoot); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		return syncDirectory(backupRoot)
	}
	return nil
}

func adminInitCleanupTombstoneName(selectedUI installstate.UI, transactionID string) string {
	return ".cleanup-" + string(selectedUI) + "-" + transactionID
}

func parseAdminInitCleanupTombstone(name string) (installstate.UI, string, bool) {
	for _, ui := range []installstate.UI{installstate.UIAntd, installstate.UIEle, installstate.UINaive} {
		prefix := ".cleanup-" + string(ui) + "-"
		if strings.HasPrefix(name, prefix) {
			transactionID := strings.TrimPrefix(name, prefix)
			return ui, transactionID, validAdminInitTransactionID(transactionID)
		}
	}
	return "", "", false
}

// validAdminInitCleanupTombstone accepts every structural subset reachable
// when no-follow recursive deletion is interrupted. The reserved name binds
// the selected UI and transaction after receipt.json itself has been removed.
func validAdminInitCleanupTombstone(candidate, canonicalRoot, directoryName string, selectedUI installstate.UI) bool {
	tombstoneUI, transactionID, ok := parseAdminInitCleanupTombstone(directoryName)
	if !ok || tombstoneUI != selectedUI || !validDirectAdminInitDirectory(candidate, canonicalRoot) {
		return false
	}
	expectedMoves := expectedAdminInitMoves(selectedUI)
	expectedApps := make(map[string]struct{}, len(expectedMoves))
	for _, move := range expectedMoves {
		expectedApps[strings.TrimPrefix(move.Backup, "apps/")] = struct{}{}
	}
	top, err := os.ReadDir(candidate)
	if err != nil || len(top) > 2 {
		return false
	}
	for _, entry := range top {
		switch entry.Name() {
		case "receipt.json":
			if !validAdminInitReceipt(filepath.Join(candidate, entry.Name()), transactionID, selectedUI) {
				return false
			}
		case "apps":
			appsPath := filepath.Join(candidate, entry.Name())
			appsInfo, err := os.Lstat(appsPath)
			if err != nil || !appsInfo.IsDir() || appsInfo.Mode()&os.ModeSymlink != 0 {
				return false
			}
			apps, err := os.ReadDir(appsPath)
			if err != nil || len(apps) > len(expectedApps) {
				return false
			}
			for _, app := range apps {
				if _, expected := expectedApps[app.Name()]; !expected {
					return false
				}
				info, err := os.Lstat(filepath.Join(appsPath, app.Name()))
				if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

func validAdminInitBackup(candidate, canonicalRoot, directoryName string, selectedUI installstate.UI) bool {
	if !validAdminInitTransactionID(directoryName) {
		return false
	}
	if !validDirectAdminInitDirectory(candidate, canonicalRoot) {
		return false
	}
	top, err := os.ReadDir(candidate)
	if err != nil || len(top) != 2 || top[0].Name() != "apps" || top[1].Name() != "receipt.json" {
		return false
	}
	appsPath := filepath.Join(candidate, "apps")
	appsInfo, err := os.Lstat(appsPath)
	if err != nil || !appsInfo.IsDir() || appsInfo.Mode()&os.ModeSymlink != 0 {
		return false
	}
	expectedMoves := expectedAdminInitMoves(selectedUI)
	if !validAdminInitReceipt(filepath.Join(candidate, "receipt.json"), directoryName, selectedUI) {
		return false
	}
	appEntries, err := os.ReadDir(appsPath)
	if err != nil || len(appEntries) != len(expectedMoves) {
		return false
	}
	for index, move := range expectedMoves {
		name := strings.TrimPrefix(move.Backup, "apps/")
		if appEntries[index].Name() != name {
			return false
		}
		appPath := filepath.Join(appsPath, name)
		appInfo, err := os.Lstat(appPath)
		if err != nil || !appInfo.IsDir() || appInfo.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}

func validDirectAdminInitDirectory(candidate, canonicalRoot string) bool {
	info, err := os.Lstat(candidate)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	canonicalCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return false
	}
	canonicalCandidate, err = filepath.Abs(canonicalCandidate)
	return err == nil && filepath.Dir(canonicalCandidate) == canonicalRoot
}

func validAdminInitReceipt(path, transactionID string, selectedUI installstate.UI) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	contents, exists, err := readRegularFile(path, maxAdminInitReceiptBytes)
	if err != nil || !exists {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var receipt adminInitReceipt
	if err := decoder.Decode(&receipt); err != nil || ensureJSONEnd(decoder) != nil {
		return false
	}
	expectedMoves := expectedAdminInitMoves(selectedUI)
	return receipt.Schema == 1 && receipt.Owner == "admin-init" && receipt.TransactionID == transactionID &&
		receipt.SelectedUI == selectedUI && receipt.DependenciesReady && equalAdminInitMoves(receipt.Moves, expectedMoves)
}

func expectedAdminInitMoves(selected installstate.UI) []adminInitMove {
	result := make([]adminInitMove, 0, 2)
	for _, ui := range []installstate.UI{installstate.UIAntd, installstate.UIEle, installstate.UINaive} {
		if ui == selected {
			continue
		}
		path := "apps/web-" + string(ui)
		result = append(result, adminInitMove{Source: path, Backup: path})
	}
	return result
}

func equalAdminInitMoves(left, right []adminInitMove) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validAdminInitTransactionID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func removeTreeWithoutFollowing(path, canonicalBoundary string) error {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(canonical)
	if err != nil || filepath.Dir(absolute) != canonicalBoundary {
		return errors.New("cleanup target escapes ui-backup")
	}
	return removeEntryWithoutFollowing(absolute)
}

func removeEntryWithoutFollowing(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := removeEntryWithoutFollowing(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return os.Remove(path)
}
