package file

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLocalStoreStreamingSecurityAndAtomicity(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocalStore(root, "http://localhost/files", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutReader(context.Background(), "v1/ab/item.bin", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	if err := s.PutReader(context.Background(), "v1/ab/item.bin", strings.NewReader("other"), 5); !errors.Is(err, ErrObjectExists) {
		t.Fatalf("duplicate = %v", err)
	}
	f, err := s.Open(context.Background(), "v1/ab/item.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, _ := io.ReadAll(f)
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	info, _ := os.Stat(filepath.Join(root, "v1", "ab", "item.bin"))
	if info.Mode().Perm() != 0600 {
		t.Fatalf("file mode %o", info.Mode().Perm())
	}
	dinfo, _ := os.Stat(filepath.Join(root, "v1", "ab"))
	if dinfo.Mode().Perm() != 0700 {
		t.Fatalf("dir mode %o", dinfo.Mode().Perm())
	}
	for _, key := range []string{"../x", "/abs", `C:\\x`, `..\\x`} {
		if err := s.PutReader(context.Background(), key, strings.NewReader("x"), 1); !errors.Is(err, ErrInvalidUpload) {
			t.Errorf("key %q accepted: %v", key, err)
		}
	}
	u, err := s.SignURL(context.Background(), "v1/ab/item.bin", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.VerifySignedURL(u); err != nil {
		t.Fatalf("verify: %v", err)
	}
	tampered := strings.Replace(u, "item.bin", "other.bin", 1)
	if err := s.VerifySignedURL(tampered); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("tampered URL accepted: %v", err)
	}
}

func TestLocalStoreOpaqueSignedURLUsesDefaultFileRoute(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocalStore(root, "", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	id := "0123456789abcdef0123456789abcdef"
	key := "v1/01/23/" + id + ".txt"
	if err := s.PutReader(context.Background(), key, strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	u, err := s.SignURLForID(context.Background(), id, key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/api/admin/v1/files/"+id+"/download" {
		t.Fatalf("opaque path = %q", parsed.Path)
	}
	if err := s.VerifyIDURL(u, id, key); err != nil {
		t.Fatalf("default-route verification: %v", err)
	}
}

type durableFileRepoFixture struct {
	mu        sync.RWMutex
	items     map[string]File
	failReady bool
}

type failingPutStore struct{}

func (failingPutStore) Put(context.Context, Object) error    { return errors.New("provider write failed") }
func (failingPutStore) Delete(context.Context, string) error { return nil }
func (failingPutStore) SignURL(context.Context, string, time.Duration) (string, error) {
	return "", errors.New("provider signing unavailable")
}

func (r *durableFileRepoFixture) Create(_ context.Context, item File) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.items == nil {
		r.items = map[string]File{}
	}
	if _, exists := r.items[item.ID]; exists {
		return ErrObjectExists
	}
	r.items[item.ID] = item
	return nil
}
func (r *durableFileRepoFixture) Get(_ context.Context, id string) (File, error) {
	r.mu.RLock()
	item, ok := r.items[id]
	r.mu.RUnlock()
	if !ok {
		return File{}, ErrFileNotFound
	}
	return item, nil
}
func (r *durableFileRepoFixture) List(_ context.Context, filter ListFilter) (Page, error) {
	r.mu.RLock()
	items := make([]File, 0, len(r.items))
	for _, item := range r.items {
		if filter.TenantID != "" && item.TenantID != filter.TenantID {
			continue
		}
		items = append(items, item)
	}
	r.mu.RUnlock()
	return Page{Items: items, Total: len(items), Limit: filter.Limit, Offset: filter.Offset}, nil
}
func (r *durableFileRepoFixture) Update(_ context.Context, item File) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[item.ID]; !ok {
		return ErrFileNotFound
	}
	if item.Status == MediaReady && r.failReady {
		r.failReady = false
		return errors.New("simulated metadata update failure")
	}
	r.items[item.ID] = item
	return nil
}
func (r *durableFileRepoFixture) MarkStatus(_ context.Context, id string, status MediaStatus, reason string, deletedAt *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[id]
	if !ok {
		return ErrFileNotFound
	}
	item.Status, item.FailureReason, item.DeletedAt = status, reason, deletedAt
	r.items[id] = item
	return nil
}
func (r *durableFileRepoFixture) ListByStatus(_ context.Context, status MediaStatus, limit int) ([]File, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]File, 0)
	for _, item := range r.items {
		if item.Status == status {
			items = append(items, item)
			if len(items) == limit {
				break
			}
		}
	}
	return items, nil
}

func (r *durableFileRepoFixture) only() File {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, item := range r.items {
		return item
	}
	return File{}
}

var _ StatusRepository = (*durableFileRepoFixture)(nil)

func TestServiceRepositoryMetadataSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root, "")
	if err != nil {
		t.Fatal(err)
	}
	repo := &durableFileRepoFixture{}
	first := NewService(store, Config{MaxBytes: 64, AllowedMIMEs: []string{"text/plain"}, Repository: repo})
	item, err := first.Upload(context.Background(), UploadInput{Reader: strings.NewReader("persisted"), Size: 9, Name: "persisted.bin", MIME: "text/plain", ACL: ACLPublicRead, TenantID: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	second := NewService(store, Config{Repository: repo})
	page, err := second.List(context.Background(), ListFilter{TenantID: "tenant-a"})
	if err != nil || page.Total != 1 || page.Items[0].ID != item.ID || page.Items[0].Status != MediaReady {
		t.Fatalf("restart list=%+v err=%v", page, err)
	}
}

func TestServiceReconcilesPublishedObjectAfterReadyWriteFailure(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root, "")
	if err != nil {
		t.Fatal(err)
	}
	repo := &durableFileRepoFixture{failReady: true}
	first := NewService(store, Config{MaxBytes: 64, AllowedMIMEs: []string{"text/plain"}, Repository: repo})
	item, err := first.Upload(context.Background(), UploadInput{Reader: strings.NewReader("recover"), Size: 7, Name: "recover.txt", MIME: "text/plain", ACL: ACLPublicRead, TenantID: "tenant-a"})
	if err == nil || item.ID != "" {
		t.Fatalf("upload after metadata failure = item=%+v err=%v", item, err)
	}
	pending := repo.only()
	if pending.ID == "" || pending.Status != MediaPending {
		t.Fatalf("published row should remain pending: %+v", pending)
	}
	second := NewService(store, Config{Repository: repo})
	if recovered, reconcileErr := second.ReconcilePending(context.Background(), 10); reconcileErr != nil || recovered != 1 {
		t.Fatalf("reconcile recovered=%d err=%v", recovered, reconcileErr)
	}
	page, listErr := second.List(context.Background(), ListFilter{TenantID: "tenant-a"})
	if listErr != nil || page.Total != 1 || page.Items[0].Status != MediaReady {
		t.Fatalf("reconciled list=%+v err=%v", page, listErr)
	}
}

func TestServiceUploadFailureMarksRepositoryFailed(t *testing.T) {
	repo := &durableFileRepoFixture{}
	service := NewService(failingPutStore{}, Config{MaxBytes: 64, AllowedMIMEs: []string{"text/plain"}, Repository: repo})
	if _, err := service.Upload(context.Background(), UploadInput{Name: "failed.txt", MIME: "text/plain", Size: 4, Data: []byte("fail"), TenantID: "tenant-a"}); err == nil {
		t.Fatal("expected provider failure")
	}
	item := repo.only()
	if item.Status != MediaFailed || item.FailureReason == "" {
		t.Fatalf("failed repository row=%+v", item)
	}
}

func TestLocalStoreContextCancellationCleansStaging(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocalStore(root, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = s.PutReader(ctx, "x", strings.NewReader("data"), 4)
	if err == nil {
		t.Fatal("expected cancellation")
	}
	entries, _ := os.ReadDir(filepath.Join(root, ".staging"))
	if len(entries) != 0 {
		t.Fatalf("staging leak: %d", len(entries))
	}
}

func TestLocalStoreRejectsWindowsRootAndReservedStagingKeys(t *testing.T) {
	if _, err := NewLocalStore(`C:\\storage`, ""); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("windows root accepted: %v", err)
	}
	s, err := NewLocalStore(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutReader(context.Background(), ".staging/escape", strings.NewReader("x"), 1); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("reserved staging key accepted: %v", err)
	}
	if err := s.PutStaging(context.Background(), "escape", strings.NewReader("x"), Object{Key: "escape", Size: 1}); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("unscoped staging write accepted: %v", err)
	}
}

func TestServiceLocalUploadPublishesStagedObjectWithInferredSuffix(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root, "")
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, Config{MaxBytes: 64, AllowedMIMEs: []string{"text/plain"}})
	item, err := service.Upload(context.Background(), UploadInput{
		Reader: strings.NewReader("hello"), Size: 5, Name: "payload.exe", MIME: "text/plain", ACL: ACLPublicRead,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Key != item.ID || item.ObjectKey == "" || !strings.HasSuffix(item.ObjectKey, ".txt") {
		t.Fatalf("unexpected metadata: %+v", item)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(item.ObjectKey))); err != nil {
		t.Fatalf("published object missing: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, ".staging"))
	if len(entries) != 0 {
		t.Fatalf("staging files remain: %d", len(entries))
	}
}

func TestServiceReadsHistoricalObjectKeyThroughRepository(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root, "")
	if err != nil {
		t.Fatal(err)
	}
	legacyKey := "legacy/2024/archive.bin"
	if err := store.PutReader(context.Background(), legacyKey, strings.NewReader("old-bytes"), 9); err != nil {
		t.Fatal(err)
	}
	repo := &durableFileRepoFixture{items: map[string]File{"legacy-id": {
		ID: "legacy-id", Key: legacyKey, ObjectKey: legacyKey, Name: "archive.bin", MIME: "application/octet-stream", Size: 9,
		TenantID: "tenant-a", OwnerID: "owner", ACL: ACLPublicRead, Status: MediaReady,
	}}}
	service := NewService(store, Config{Repository: repo})
	_, reader, err := service.Open(context.Background(), "legacy-id", "", "tenant-a", "")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "old-bytes" {
		t.Fatalf("legacy read=%q err=%v", data, err)
	}
}

func TestServiceRejectsNonReadyDownload(t *testing.T) {
	repo := &durableFileRepoFixture{items: map[string]File{"pending-id": {
		ID: "pending-id", Key: "v1/pe/nd/pending-id.txt", ObjectKey: "v1/pe/nd/pending-id.txt", Name: "pending.txt", MIME: "text/plain", Size: 1,
		TenantID: "tenant-a", OwnerID: "owner", ACL: ACLPublicRead, Status: MediaPending,
	}}}
	service := NewService(NewMemoryStore(""), Config{Repository: repo})
	if _, _, err := service.Download(context.Background(), "pending-id", "", "tenant-a", ""); !errors.Is(err, ErrMediaNotReady) {
		t.Fatalf("pending download error=%v", err)
	}
}

func TestLocalStoreRejectsSymlinkEscapeAndUnsafeRoots(t *testing.T) {
	if _, err := NewLocalStore(string(filepath.Separator), ""); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("unsafe root error = %v", err)
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	store, err := NewLocalStore(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutReader(context.Background(), "link/escape", strings.NewReader("x"), 1); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("symlink escape accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "escape")); !os.IsNotExist(err) {
		t.Fatalf("symlink target was written: %v", err)
	}
	// Initialization must inspect existing parent components as well as the
	// final directory; otherwise a pre-created symlink could redirect all new
	// objects outside the configured root.
	if _, err := NewLocalStore(filepath.Join(root, "link", "objects"), ""); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("symlinked configured root accepted: %v", err)
	}
}
