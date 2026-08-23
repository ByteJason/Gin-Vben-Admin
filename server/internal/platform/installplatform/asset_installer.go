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

// assetTransactionManifest is a rollback receipt for the selected build. UI
// source movement belongs exclusively to admin/scripts/init.mjs; the server
// preserves the selected package directory and package name.
type assetTransactionManifest struct {
	SchemaVersion int               `json:"schema_version"`
	Reference     string            `json:"reference"`
	SelectedUI    installstate.UI   `json:"selected_ui"`
	Mode          installstate.Mode `json:"mode"`
	ArtifactHash  string            `json:"artifact_hash"`
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
	if err != nil || !validAssetSelection(plan.SelectedUI, plan.Mode) || !selectedProfileLayout(root, plan.SelectedUI) {
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
		SchemaVersion: 1,
		Reference:     reference,
		SelectedUI:    plan.SelectedUI,
		Mode:          plan.Mode,
		ArtifactHash:  artifactHash,
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
	return installer.AssetReceipt{
		ArtifactHash: artifactHash,
		ManifestHash: hex.EncodeToString(digest[:]),
		Reference:    reference,
	}, nil
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
	_, backupRoot, err := s.validatedRoots()
	if err != nil {
		return ErrAssetInstallation
	}
	transactionDir := filepath.Join(backupRoot, receipt.Reference)
	manifestPath := filepath.Join(transactionDir, "transaction.json")
	encoded, exists, err := readRegularFile(manifestPath, maxAssetManifestBytes)
	if err != nil || !exists || digestBytes(encoded) != receipt.ManifestHash {
		return ErrAssetInstallation
	}
	var manifest assetTransactionManifest
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || ensureJSONEnd(decoder) != nil || !validAssetManifest(manifest, receipt) {
		return ErrAssetInstallation
	}
	entries, err := os.ReadDir(transactionDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "transaction.json" || !entries[0].Type().IsRegular() {
		return ErrAssetInstallation
	}
	if err := os.Remove(manifestPath); err != nil {
		return ErrAssetInstallation
	}
	if err := os.Remove(transactionDir); err != nil {
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

func selectedProfileLayout(root string, selected installstate.UI) bool {
	selectedPath := filepath.Join(root, "admin", "apps", "web-"+string(selected))
	if err := requirePlainDirectory(selectedPath); err != nil {
		return false
	}
	for _, candidate := range []installstate.UI{installstate.UIAntd, installstate.UIEle, installstate.UINaive} {
		if candidate == selected {
			continue
		}
		_, err := os.Lstat(filepath.Join(root, "admin", "apps", "web-"+string(candidate)))
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			return false
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "admin", "apps", "web")); err == nil || !errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
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
	return manifest.SchemaVersion == 1 &&
		manifest.Reference == receipt.Reference &&
		manifest.ArtifactHash == receipt.ArtifactHash &&
		validAssetSelection(manifest.SelectedUI, manifest.Mode)
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
