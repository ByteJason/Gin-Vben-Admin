// Package backup contains the local encrypted-artifact adapter for the backup
// application port. It intentionally writes only to the local filesystem;
// remote object storage is a later provider and is not constructed here.
package backup

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	appbackup "example.com/gin-vben-admin/server/internal/application/backup"
)

var (
	ErrArtifactExists  = errors.New("backup artifact already exists")
	ErrArtifactCorrupt = errors.New("backup artifact is corrupt")
	ErrArtifactKey     = errors.New("backup artifact encryption key is invalid")
	ErrArtifactState   = errors.New("backup artifact writer is not active")
)

const (
	envelopeMagic       = "GVABKP01"
	envelopeVersion     = byte(1)
	defaultChunkSize    = 1 << 20
	nonceSize           = 12
	maxChunkCipherBytes = defaultChunkSize + 16
)

// LocalArtifactStore publishes a chunked AES-GCM envelope and a credential-
// free JSON sidecar. A chunked envelope keeps memory bounded while retaining
// authenticated encryption for every portion of a logical dump.
type LocalArtifactStore struct {
	clock     func() time.Time
	chunkSize int
}

var _ appbackup.ArtifactStore = (*LocalArtifactStore)(nil)

func NewLocalArtifactStore(clocks ...func() time.Time) *LocalArtifactStore {
	clock := time.Now
	if len(clocks) > 0 && clocks[0] != nil {
		clock = clocks[0]
	}
	return &LocalArtifactStore{clock: clock, chunkSize: defaultChunkSize}
}

func (s *LocalArtifactStore) Create(ctx context.Context, request appbackup.ArtifactRequest) (appbackup.ArtifactSink, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.New("local artifact store is not initialized")
	}
	request.Source.Driver = appbackup.Driver(strings.ToLower(strings.TrimSpace(string(request.Source.Driver))))
	if request.Source.Driver != appbackup.DriverMySQL && request.Source.Driver != appbackup.DriverPostgres {
		return nil, fmt.Errorf("unsupported artifact driver %q", request.Source.Driver)
	}
	if len(request.EncryptionKey) == 0 {
		return nil, ErrArtifactKey
	}
	if request.TargetRPO <= 0 || request.TargetRTO <= 0 {
		return nil, errors.New("artifact RPO/RTO targets must be positive")
	}
	destination, err := absoluteArtifactPath(request.Destination)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(destination); err == nil {
		return nil, ErrArtifactExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect artifact destination: %w", err)
	}
	if _, err := os.Lstat(metadataPath(destination)); err == nil {
		return nil, ErrArtifactExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect artifact metadata destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return nil, fmt.Errorf("create artifact directory: %w", err)
	}
	plain, err := os.CreateTemp(filepath.Dir(destination), ".gva-backup-plain-*")
	if err != nil {
		return nil, fmt.Errorf("create artifact staging file: %w", err)
	}
	if err := plain.Chmod(0o600); err != nil {
		_ = plain.Close()
		_ = os.Remove(plain.Name())
		return nil, fmt.Errorf("protect artifact staging file: %w", err)
	}
	created := request.CreatedAt.UTC()
	if created.IsZero() {
		created = s.clock().UTC()
	}
	return &artifactSink{
		store: s,
		request: appbackup.ArtifactRequest{
			Destination:   destination,
			Source:        request.Source,
			EncryptionKey: append([]byte(nil), request.EncryptionKey...),
			TargetRPO:     request.TargetRPO,
			TargetRTO:     request.TargetRTO,
			CreatedAt:     created,
		},
		plain:     plain,
		plainPath: plain.Name(),
		hash:      sha256.New(),
	}, nil
}

func (s *LocalArtifactStore) Open(ctx context.Context, path string, key []byte) (appbackup.ArtifactReader, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.New("local artifact store is not initialized")
	}
	if len(key) == 0 {
		return nil, ErrArtifactKey
	}
	destination, err := absoluteArtifactPath(path)
	if err != nil {
		return nil, err
	}
	metadataBytes, err := os.ReadFile(metadataPath(destination))
	if err != nil {
		return nil, fmt.Errorf("read artifact metadata: %w", err)
	}
	var artifact appbackup.Artifact
	if err := json.Unmarshal(metadataBytes, &artifact); err != nil {
		return nil, fmt.Errorf("decode artifact metadata: %w", err)
	}
	if artifact.Path != destination || artifact.ID == "" || artifact.Encryption != appbackup.EncryptionAES256GCM {
		return nil, ErrArtifactCorrupt
	}
	if artifact.Driver != appbackup.DriverMySQL && artifact.Driver != appbackup.DriverPostgres {
		return nil, ErrArtifactCorrupt
	}
	input, err := os.Open(destination)
	if err != nil {
		return nil, fmt.Errorf("open backup artifact: %w", err)
	}
	decrypted, err := decryptToTemp(ctx, input, key, artifact, s.chunkSize)
	closeErr := input.Close()
	if err != nil {
		if closeErr != nil {
			return nil, fmt.Errorf("decrypt artifact: %v; close artifact: %w", err, closeErr)
		}
		return nil, fmt.Errorf("decrypt artifact: %w", err)
	}
	if closeErr != nil {
		_ = decrypted.Close()
		return nil, fmt.Errorf("close artifact: %w", closeErr)
	}
	return &artifactReader{File: decrypted, artifact: artifact}, nil
}

type artifactSink struct {
	mu        sync.Mutex
	store     *LocalArtifactStore
	request   appbackup.ArtifactRequest
	plain     *os.File
	plainPath string
	hash      hash.Hash
	bytes     int64
	state     sinkState
}

type sinkState uint8

const (
	sinkActive sinkState = iota
	sinkCommitted
	sinkAborted
)

func (s *artifactSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != sinkActive || s.plain == nil {
		return 0, ErrArtifactState
	}
	if len(p) == 0 {
		return 0, nil
	}
	n, err := s.plain.Write(p)
	if n > 0 {
		_, _ = s.hash.Write(p[:n])
		s.bytes += int64(n)
	}
	return n, err
}

func (s *artifactSink) Commit(ctx context.Context) (appbackup.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != sinkActive || s.plain == nil {
		return appbackup.Artifact{}, ErrArtifactState
	}
	if err := contextError(ctx); err != nil {
		return appbackup.Artifact{}, err
	}
	if err := s.plain.Sync(); err != nil {
		return appbackup.Artifact{}, fmt.Errorf("sync artifact staging file: %w", err)
	}
	if err := s.plain.Close(); err != nil {
		return appbackup.Artifact{}, fmt.Errorf("close artifact staging file: %w", err)
	}
	s.plain = nil
	artifact, err := s.store.publishFromPath(ctx, s.request, s.plainPath, s.bytes, hex.EncodeToString(s.hash.Sum(nil)))
	_ = os.Remove(s.plainPath)
	if err != nil {
		return appbackup.Artifact{}, err
	}
	s.state = sinkCommitted
	return artifact, nil
}

func (s *artifactSink) Abort() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == sinkAborted {
		return nil
	}
	if s.state == sinkCommitted {
		return ErrArtifactState
	}
	s.state = sinkAborted
	if s.plain != nil {
		_ = s.plain.Close()
		s.plain = nil
	}
	if s.plainPath != "" {
		_ = os.Remove(s.plainPath)
	}
	return nil
}

func (s *LocalArtifactStore) publishFromPath(ctx context.Context, request appbackup.ArtifactRequest, plainPath string, plainBytes int64, digest string) (appbackup.Artifact, error) {
	if err := contextError(ctx); err != nil {
		return appbackup.Artifact{}, err
	}
	input, err := os.Open(plainPath)
	if err != nil {
		return appbackup.Artifact{}, fmt.Errorf("open artifact staging file: %w", err)
	}
	defer input.Close()
	chunkSize := s.chunkSize
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}
	gcm, err := newGCM(request.EncryptionKey)
	if err != nil {
		return appbackup.Artifact{}, err
	}
	var baseNonce [nonceSize]byte
	if _, err := io.ReadFull(rand.Reader, baseNonce[:]); err != nil {
		return appbackup.Artifact{}, fmt.Errorf("generate artifact nonce: %w", err)
	}
	destination := request.Destination
	if _, err := os.Lstat(destination); err == nil {
		return appbackup.Artifact{}, ErrArtifactExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return appbackup.Artifact{}, fmt.Errorf("inspect artifact destination: %w", err)
	}
	if _, err := os.Lstat(metadataPath(destination)); err == nil {
		return appbackup.Artifact{}, ErrArtifactExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return appbackup.Artifact{}, fmt.Errorf("inspect artifact metadata destination: %w", err)
	}
	envelope, err := os.CreateTemp(filepath.Dir(destination), ".gva-backup-envelope-*")
	if err != nil {
		return appbackup.Artifact{}, fmt.Errorf("create artifact envelope: %w", err)
	}
	envelopePath := envelope.Name()
	cleanupEnvelope := func() {
		_ = envelope.Close()
		_ = os.Remove(envelopePath)
	}
	if err := envelope.Chmod(0o600); err != nil {
		cleanupEnvelope()
		return appbackup.Artifact{}, fmt.Errorf("protect artifact envelope: %w", err)
	}
	if err := envelopeHeader(envelope, uint32(chunkSize), baseNonce[:]); err != nil {
		cleanupEnvelope()
		return appbackup.Artifact{}, fmt.Errorf("write artifact envelope header: %w", err)
	}
	buffer := make([]byte, chunkSize)
	var index uint32
	var ciphertextBytes int64
	for {
		if err := contextError(ctx); err != nil {
			cleanupEnvelope()
			return appbackup.Artifact{}, err
		}
		n, readErr := io.ReadFull(input, buffer)
		if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
			cleanupEnvelope()
			return appbackup.Artifact{}, fmt.Errorf("read artifact staging file: %w", readErr)
		}
		if n == 0 {
			break
		}
		sealed := gcm.Seal(nil, chunkNonce(baseNonce[:], index), buffer[:n], []byte(envelopeMagic))
		if err := writeUint32(envelope, uint32(len(sealed))); err != nil {
			cleanupEnvelope()
			return appbackup.Artifact{}, fmt.Errorf("write artifact chunk length: %w", err)
		}
		if _, err := envelope.Write(sealed); err != nil {
			cleanupEnvelope()
			return appbackup.Artifact{}, fmt.Errorf("write artifact chunk: %w", err)
		}
		ciphertextBytes += int64(len(sealed))
		index++
		if errors.Is(readErr, io.ErrUnexpectedEOF) || errors.Is(readErr, io.EOF) {
			break
		}
	}
	if err := writeUint32(envelope, 0); err != nil {
		cleanupEnvelope()
		return appbackup.Artifact{}, fmt.Errorf("write artifact terminator: %w", err)
	}
	if err := envelope.Sync(); err != nil {
		cleanupEnvelope()
		return appbackup.Artifact{}, fmt.Errorf("sync artifact envelope: %w", err)
	}
	if err := envelope.Close(); err != nil {
		_ = os.Remove(envelopePath)
		return appbackup.Artifact{}, fmt.Errorf("close artifact envelope: %w", err)
	}
	if _, err := os.Lstat(destination); err == nil {
		_ = os.Remove(envelopePath)
		return appbackup.Artifact{}, ErrArtifactExists
	}
	if err := os.Rename(envelopePath, destination); err != nil {
		_ = os.Remove(envelopePath)
		return appbackup.Artifact{}, fmt.Errorf("publish artifact envelope: %w", err)
	}
	now := s.clock().UTC()
	artifact := appbackup.Artifact{
		ID: newArtifactID(), Driver: request.Source.Driver, Path: destination,
		CreatedAt: request.CreatedAt.UTC(), CompletedAt: now,
		PlaintextBytes: plainBytes, CiphertextBytes: ciphertextBytes,
		SHA256: digest, Encryption: appbackup.EncryptionAES256GCM,
		KeyID: keyID(request.EncryptionKey), TargetRPO: request.TargetRPO, TargetRTO: request.TargetRTO,
	}
	metadata, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		_ = os.Remove(destination)
		return appbackup.Artifact{}, fmt.Errorf("encode artifact metadata: %w", err)
	}
	metaTemp, err := os.CreateTemp(filepath.Dir(destination), ".gva-backup-meta-*")
	if err != nil {
		_ = os.Remove(destination)
		return appbackup.Artifact{}, fmt.Errorf("create artifact metadata: %w", err)
	}
	metaPath := metaTemp.Name()
	cleanupMeta := func() {
		_ = metaTemp.Close()
		_ = os.Remove(metaPath)
	}
	if err := metaTemp.Chmod(0o600); err != nil {
		cleanupMeta()
		_ = os.Remove(destination)
		return appbackup.Artifact{}, fmt.Errorf("protect artifact metadata: %w", err)
	}
	if _, err := metaTemp.Write(metadata); err != nil {
		cleanupMeta()
		_ = os.Remove(destination)
		return appbackup.Artifact{}, fmt.Errorf("write artifact metadata: %w", err)
	}
	if err := metaTemp.Sync(); err != nil {
		cleanupMeta()
		_ = os.Remove(destination)
		return appbackup.Artifact{}, fmt.Errorf("sync artifact metadata: %w", err)
	}
	if err := metaTemp.Close(); err != nil {
		_ = os.Remove(metaPath)
		_ = os.Remove(destination)
		return appbackup.Artifact{}, fmt.Errorf("close artifact metadata: %w", err)
	}
	if err := os.Rename(metaPath, metadataPath(destination)); err != nil {
		_ = os.Remove(metaPath)
		_ = os.Remove(destination)
		return appbackup.Artifact{}, fmt.Errorf("publish artifact metadata: %w", err)
	}
	return artifact, nil
}

type artifactReader struct {
	*os.File
	artifact appbackup.Artifact
	once     sync.Once
	closeErr error
}

func (r *artifactReader) Artifact() appbackup.Artifact { return r.artifact }

func (r *artifactReader) Close() error {
	if r == nil {
		return nil
	}
	r.once.Do(func() {
		if r.File != nil {
			r.closeErr = r.File.Close()
			name := r.File.Name()
			if err := os.Remove(name); r.closeErr == nil && err != nil && !errors.Is(err, os.ErrNotExist) {
				r.closeErr = err
			}
		}
	})
	return r.closeErr
}

func decryptToTemp(ctx context.Context, input *os.File, key []byte, artifact appbackup.Artifact, chunkSize int) (*os.File, error) {
	magic := make([]byte, len(envelopeMagic))
	if _, err := io.ReadFull(input, magic); err != nil || string(magic) != envelopeMagic {
		return nil, ErrArtifactCorrupt
	}
	version, err := readByte(input)
	if err != nil || version != envelopeVersion {
		return nil, ErrArtifactCorrupt
	}
	encodedSize, err := readUint32(input)
	if err != nil || encodedSize == 0 || encodedSize > 16<<20 {
		return nil, ErrArtifactCorrupt
	}
	if chunkSize <= 0 {
		chunkSize = int(encodedSize)
	}
	baseNonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(input, baseNonce); err != nil {
		return nil, ErrArtifactCorrupt
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	temp, err := os.CreateTemp("", "gva-backup-restore-*")
	if err != nil {
		return nil, fmt.Errorf("create restore staging file: %w", err)
	}
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return nil, fmt.Errorf("protect restore staging file: %w", err)
	}
	var index uint32
	var plaintextBytes, ciphertextBytes int64
	digest := sha256.New()
	for {
		if err := contextError(ctx); err != nil {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
			return nil, err
		}
		length, err := readUint32(input)
		if err != nil {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
			return nil, ErrArtifactCorrupt
		}
		if length == 0 {
			break
		}
		if length > uint32(maxChunkCipherBytes) || int(length) < gcm.Overhead() {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
			return nil, ErrArtifactCorrupt
		}
		ciphertext := make([]byte, int(length))
		if _, err := io.ReadFull(input, ciphertext); err != nil {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
			return nil, ErrArtifactCorrupt
		}
		nonce := chunkNonce(baseNonce, index)
		plain, err := gcm.Open(nil, nonce, ciphertext, []byte(envelopeMagic))
		if err != nil {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
			return nil, ErrArtifactKey
		}
		if len(plain) > int(encodedSize) {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
			return nil, ErrArtifactCorrupt
		}
		if _, err := temp.Write(plain); err != nil {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
			return nil, fmt.Errorf("write restore staging file: %w", err)
		}
		_, _ = digest.Write(plain)
		plaintextBytes += int64(len(plain))
		ciphertextBytes += int64(len(ciphertext))
		index++
	}
	if extra, err := input.Read(make([]byte, 1)); err != nil && !errors.Is(err, io.EOF) {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return nil, ErrArtifactCorrupt
	} else if err == nil || extra > 0 {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return nil, ErrArtifactCorrupt
	}
	if plaintextBytes != artifact.PlaintextBytes || ciphertextBytes != artifact.CiphertextBytes || hex.EncodeToString(digest.Sum(nil)) != artifact.SHA256 {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return nil, ErrArtifactCorrupt
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		return nil, fmt.Errorf("rewind restore staging file: %w", err)
	}
	return temp, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) == 0 {
		return nil, ErrArtifactKey
	}
	digest := sha256.Sum256(key)
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return nil, fmt.Errorf("initialize artifact encryption: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize artifact envelope: %w", err)
	}
	return gcm, nil
}

func chunkNonce(base []byte, index uint32) []byte {
	nonce := append([]byte(nil), base...)
	binary.BigEndian.PutUint32(nonce[len(nonce)-4:], index)
	return nonce
}

func envelopeHeader(w io.Writer, chunkSize uint32, baseNonce []byte) error {
	if _, err := io.WriteString(w, envelopeMagic); err != nil {
		return err
	}
	if err := writeByte(w, envelopeVersion); err != nil {
		return err
	}
	if err := writeUint32(w, chunkSize); err != nil {
		return err
	}
	_, err := w.Write(baseNonce)
	return err
}

func writeByte(w io.Writer, value byte) error {
	_, err := w.Write([]byte{value})
	return err
}

func readByte(r io.Reader) (byte, error) {
	var value [1]byte
	_, err := io.ReadFull(r, value[:])
	return value[0], err
}

func writeUint32(w io.Writer, value uint32) error {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	_, err := w.Write(encoded[:])
	return err
}

func readUint32(r io.Reader) (uint32, error) {
	var encoded [4]byte
	if _, err := io.ReadFull(r, encoded[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(encoded[:]), nil
}

func absoluteArtifactPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("artifact path is required")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve artifact path: %w", err)
	}
	if abs == string(filepath.Separator) {
		return "", errors.New("artifact path must name a file")
	}
	return abs, nil
}

func metadataPath(path string) string { return path + ".meta.json" }

func newArtifactID() string {
	var raw [16]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		// Randomness failures are exceptionally rare and the artifact itself is
		// still protected by the envelope key; make the identifier deterministic
		// only as a last-resort collision-resistant fallback.
		digest := sha256.Sum256([]byte(time.Now().UTC().String()))
		return hex.EncodeToString(digest[:16])
	}
	return hex.EncodeToString(raw[:])
}

func keyID(key []byte) string {
	digest := sha256.Sum256(key)
	return hex.EncodeToString(digest[:8])
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
