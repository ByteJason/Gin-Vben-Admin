package installplatform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

var ErrAssetInstallation = errors.New("management UI asset installation failed")

const maxAssetManifestBytes = 64 << 10

type AssetBuilder interface {
	Build(context.Context, installstate.UI, installstate.Mode) (string, error)
}

type AssetInstaller struct {
	root       string
	backupRoot string
	builder    AssetBuilder
	random     io.Reader
	mutex      sync.Mutex
}

type assetTransactionManifest struct {
	SchemaVersion int               `json:"schema_version"`
	Reference     string            `json:"reference"`
	SelectedUI    installstate.UI   `json:"selected_ui"`
	Mode          installstate.Mode `json:"mode"`
	ArtifactHash  string            `json:"artifact_hash"`
	SelectedFrom  string            `json:"selected_from"`
	SelectedTo    string            `json:"selected_to"`
	Staged        []assetStagedPath `json:"staged"`
}

type assetStagedPath struct {
	Source string `json:"source"`
	Backup string `json:"backup"`
}

func NewAssetInstaller(root, backupRoot string, builder AssetBuilder, randomSource io.Reader) *AssetInstaller {
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &AssetInstaller{
		root: filepath.Clean(root), backupRoot: filepath.Clean(backupRoot), builder: builder, random: randomSource,
	}
}

func (s *AssetInstaller) Prepare(ctx context.Context, plan installer.Plan) (installer.AssetReceipt, error) {
	if s == nil || s.builder == nil || s.random == nil || !plan.CanCleanup || !plan.CanBuild || !plan.CanWriteEnv {
		return installer.AssetReceipt{}, ErrAssetInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return installer.AssetReceipt{}, err
	}
	root, backupRoot, err := s.validatedRoots()
	if err != nil {
		return installer.AssetReceipt{}, ErrAssetInstallation
	}
	if !validAssetSelection(plan.SelectedUI, plan.Mode) {
		return installer.AssetReceipt{}, ErrAssetInstallation
	}
	selected := "admin/apps/web-" + string(plan.SelectedUI)
	target := "admin/apps/web"
	staged := make([]assetStagedPath, 0, 2)
	for _, candidate := range []string{"antd", "ele", "naive"} {
		relative := "admin/apps/web-" + candidate
		if err := requirePlainDirectory(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			return installer.AssetReceipt{}, ErrAssetInstallation
		}
		if relative != selected {
			staged = append(staged, assetStagedPath{Source: relative, Backup: relative})
		}
	}
	if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(target))); err == nil || !errors.Is(err, os.ErrNotExist) {
		return installer.AssetReceipt{}, ErrAssetInstallation
	}

	artifactHash, err := s.builder.Build(ctx, plan.SelectedUI, plan.Mode)
	if err != nil || !validAssetDigest(artifactHash) {
		return installer.AssetReceipt{}, ErrAssetInstallation
	}
	s.mutex.Lock()
	reference, randomErr := randomHex(s.random, 16)
	s.mutex.Unlock()
	if randomErr != nil || !validAssetReference(reference) {
		return installer.AssetReceipt{}, ErrAssetInstallation
	}
	transactionDir := filepath.Join(backupRoot, reference)
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return installer.AssetReceipt{}, ErrAssetInstallation
	}
	if err := os.Mkdir(transactionDir, 0o700); err != nil {
		return installer.AssetReceipt{}, ErrAssetInstallation
	}
	manifest := assetTransactionManifest{
		SchemaVersion: 1, Reference: reference, SelectedUI: plan.SelectedUI, Mode: plan.Mode,
		ArtifactHash: artifactHash, SelectedFrom: selected, SelectedTo: target, Staged: staged,
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = os.Remove(transactionDir)
		return installer.AssetReceipt{}, ErrAssetInstallation
	}
	encoded = append(encoded, '\n')
	manifestPath := filepath.Join(transactionDir, "transaction.json")
	if err := publishPrivateFile(manifestPath, encoded); err != nil {
		_ = os.Remove(transactionDir)
		return installer.AssetReceipt{}, ErrAssetInstallation
	}
	digest := sha256.Sum256(encoded)
	receipt := installer.AssetReceipt{
		ArtifactHash: artifactHash, ManifestHash: hex.EncodeToString(digest[:]), Reference: reference,
	}

	if err := stageAssetTransaction(root, transactionDir, manifest); err != nil {
		_ = restoreAssetTransaction(root, transactionDir, manifest)
		return installer.AssetReceipt{}, ErrAssetInstallation
	}
	return receipt, nil
}

func (s *AssetInstaller) Rollback(ctx context.Context, receipt installer.AssetReceipt) error {
	if s == nil || !validAssetReference(receipt.Reference) || !validAssetDigest(receipt.ArtifactHash) || !validAssetDigest(receipt.ManifestHash) {
		return ErrAssetInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, backupRoot, err := s.validatedRoots()
	if err != nil {
		return ErrAssetInstallation
	}
	transactionDir := filepath.Join(backupRoot, receipt.Reference)
	encoded, exists, err := readRegularFile(filepath.Join(transactionDir, "transaction.json"), maxAssetManifestBytes)
	if err != nil || !exists || digestBytes(encoded) != receipt.ManifestHash {
		return ErrAssetInstallation
	}
	var manifest assetTransactionManifest
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || !validAssetManifest(manifest, receipt) {
		return ErrAssetInstallation
	}
	if err := restoreAssetTransaction(root, transactionDir, manifest); err != nil {
		return ErrAssetInstallation
	}
	return nil
}

func (s *AssetInstaller) validatedRoots() (string, string, error) {
	root, err := filepath.Abs(s.root)
	if err != nil || root == string(filepath.Separator) {
		return "", "", errors.New("invalid workspace root")
	}
	backupRoot, err := filepath.Abs(s.backupRoot)
	if err != nil {
		return "", "", errors.New("invalid backup root")
	}
	relative, err := filepath.Rel(root, backupRoot)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", errors.New("backup root must be inside the workspace")
	}
	return root, backupRoot, nil
}

func stageAssetTransaction(root, transactionDir string, manifest assetTransactionManifest) error {
	for _, entry := range manifest.Staged {
		source := filepath.Join(root, filepath.FromSlash(entry.Source))
		backup := filepath.Join(transactionDir, filepath.FromSlash(entry.Backup))
		if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
			return err
		}
		if err := os.Rename(source, backup); err != nil {
			return err
		}
	}
	return os.Rename(
		filepath.Join(root, filepath.FromSlash(manifest.SelectedFrom)),
		filepath.Join(root, filepath.FromSlash(manifest.SelectedTo)),
	)
}

func restoreAssetTransaction(root, transactionDir string, manifest assetTransactionManifest) error {
	selectedFrom := filepath.Join(root, filepath.FromSlash(manifest.SelectedFrom))
	selectedTo := filepath.Join(root, filepath.FromSlash(manifest.SelectedTo))
	if err := restoreRenamedDirectory(selectedTo, selectedFrom); err != nil {
		return err
	}
	for index := len(manifest.Staged) - 1; index >= 0; index-- {
		entry := manifest.Staged[index]
		backup := filepath.Join(transactionDir, filepath.FromSlash(entry.Backup))
		source := filepath.Join(root, filepath.FromSlash(entry.Source))
		if err := restoreRenamedDirectory(backup, source); err != nil {
			return err
		}
	}
	if err := os.Remove(filepath.Join(transactionDir, "transaction.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.RemoveAll(transactionDir); err != nil {
		return err
	}
	return nil
}

func restoreRenamedDirectory(from, to string) error {
	fromInfo, fromErr := os.Lstat(from)
	toInfo, toErr := os.Lstat(to)
	fromExists := fromErr == nil
	toExists := toErr == nil
	if fromExists && (!fromInfo.IsDir() || fromInfo.Mode()&os.ModeSymlink != 0) {
		return errors.New("restore source is not a plain directory")
	}
	if toExists && (!toInfo.IsDir() || toInfo.Mode()&os.ModeSymlink != 0) {
		return errors.New("restore target is not a plain directory")
	}
	switch {
	case fromExists && !toExists:
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		return os.Rename(from, to)
	case !fromExists && toExists:
		return nil
	case fromExists && toExists:
		return errors.New("both restore source and target exist")
	default:
		return errors.New("both restore source and target are missing")
	}
}

func requirePlainDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path is not a plain directory")
	}
	return nil
}

func validAssetSelection(ui installstate.UI, mode installstate.Mode) bool {
	switch ui {
	case installstate.UIAntd, installstate.UIEle, installstate.UINaive:
	default:
		return false
	}
	switch mode {
	case installstate.ModeEmbedded, installstate.ModeStandalone, installstate.ModeAPIOnly, installstate.ModeDev:
		return true
	default:
		return false
	}
}

func validAssetManifest(manifest assetTransactionManifest, receipt installer.AssetReceipt) bool {
	if manifest.SchemaVersion != 1 || manifest.Reference != receipt.Reference || manifest.ArtifactHash != receipt.ArtifactHash || !validAssetSelection(manifest.SelectedUI, manifest.Mode) {
		return false
	}
	expectedSelected := "admin/apps/web-" + string(manifest.SelectedUI)
	if manifest.SelectedFrom != expectedSelected || manifest.SelectedTo != "admin/apps/web" || len(manifest.Staged) != 2 {
		return false
	}
	allowed := map[string]bool{"admin/apps/web-antd": true, "admin/apps/web-ele": true, "admin/apps/web-naive": true}
	for _, entry := range manifest.Staged {
		if !allowed[entry.Source] || entry.Backup != entry.Source || entry.Source == expectedSelected {
			return false
		}
		delete(allowed, entry.Source)
	}
	return true
}

func validAssetReference(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validAssetDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
