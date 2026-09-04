package file

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	objects map[string]Object
}

func TestUploadUsesUTCDatePartitionedObjectKey(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store, Config{
		AllowedMIMEs: []string{"text/plain"},
		Clock:        func() time.Time { return time.Date(2026, 9, 5, 1, 0, 0, 0, time.FixedZone("fixture", 8*60*60)) },
	})
	item, err := svc.Upload(context.Background(), UploadInput{Name: "note.txt", MIME: "text/plain", Size: 4, Data: []byte("test")})
	if err != nil {
		t.Fatal(err)
	}
	// The configured clock is converted to UTC before partitioning: 01:00 at
	// UTC+08 is still the previous UTC day.
	if !regexp.MustCompile(`^2026/0904/[A-Za-z0-9_-]+\.txt$`).MatchString(item.ObjectKey) {
		t.Fatalf("object key = %q", item.ObjectKey)
	}
	if strings.HasPrefix(item.ObjectKey, "v1/") {
		t.Fatalf("legacy version prefix remains in new key %q", item.ObjectKey)
	}
	if _, ok := store.objects[item.ObjectKey]; !ok {
		t.Fatalf("stored object missing key %q", item.ObjectKey)
	}
}

func (f *fakeStore) Put(_ context.Context, object Object) error {
	if f.objects == nil {
		f.objects = map[string]Object{}
	}
	f.objects[object.Key] = object
	return nil
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

func (f *fakeStore) SignURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://objects.example/" + key, nil
}

func TestServiceRejectsOversizedAndDisallowedMIME(t *testing.T) {
	svc := NewService(&fakeStore{}, Config{MaxBytes: 10, AllowedMIMEs: []string{"image/png"}})
	if _, err := svc.Upload(context.Background(), UploadInput{Name: "a.png", MIME: "image/png", Size: 11}); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("size error = %v", err)
	}
	if _, err := svc.Upload(context.Background(), UploadInput{Name: "a.exe", MIME: "application/octet-stream", Size: 1, Data: []byte("x")}); !errors.Is(err, ErrMIMETypeNotAllowed) {
		t.Fatalf("mime error = %v", err)
	}
}

func TestServiceSignsURLAndEnforcesACL(t *testing.T) {
	svc := NewService(NewMemoryStore("https://objects.example/files"), Config{MaxBytes: 100, AllowedMIMEs: []string{"text/plain"}})
	item, err := svc.Upload(context.Background(), UploadInput{Name: "note.txt", MIME: "text/plain", Size: 4, Data: []byte("test"), OwnerID: "u1", ACL: ACLPrivate})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SignedURL(context.Background(), item.ID, "u2", time.Minute); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("private access error = %v", err)
	}
	url, err := svc.SignedURL(context.Background(), item.ID, "u1", time.Minute)
	if err != nil || url == "" {
		t.Fatalf("signed url = %q, err = %v", url, err)
	}
}

func TestServiceRejectsLegacyUnverifiableSignedURLProvider(t *testing.T) {
	svc := NewService(&fakeStore{}, Config{MaxBytes: 100, AllowedMIMEs: []string{"text/plain"}})
	item, err := svc.Upload(context.Background(), UploadInput{Name: "note.txt", MIME: "text/plain", Size: 4, Data: []byte("test"), ACL: ACLPublicRead})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SignedURL(context.Background(), item.ID, "", time.Minute); !errors.Is(err, ErrSignedURLUnsupported) {
		t.Fatalf("legacy signer error = %v", err)
	}
}

func TestPublicReadDoesNotGrantMutationPermission(t *testing.T) {
	store := NewMemoryStore("http://memory.invalid/files")
	svc := NewService(store, Config{AllowedMIMEs: []string{"text/plain"}})
	item, err := svc.Upload(context.Background(), UploadInput{Name: "public.txt", MIME: "text/plain", Size: 5, Data: []byte("hello"), OwnerID: "owner", TenantID: "tenant", ACL: ACLPublicRead})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteFile(context.Background(), item.ID, "", "tenant", ""); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("anonymous public delete = %v", err)
	}
	if err := svc.DeleteFile(context.Background(), item.ID, "other", "tenant", ""); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("non-owner public delete = %v", err)
	}
	if err := svc.DeleteFile(context.Background(), item.ID, "owner", "tenant", ""); err != nil {
		t.Fatalf("owner public delete = %v", err)
	}
}

func TestDeleteQueuesProviderRemovalAndRetainsTombstone(t *testing.T) {
	store := NewMemoryStore("http://memory.invalid/files")
	svc := NewService(store, Config{AllowedMIMEs: []string{"text/plain"}})
	item, err := svc.Upload(context.Background(), UploadInput{Name: "queued.txt", MIME: "text/plain", Size: 5, Data: []byte("hello"), OwnerID: "owner", TenantID: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteFile(context.Background(), item.ID, "owner", "tenant", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), item.ObjectKey); err != nil {
		t.Fatalf("object removed before worker: %v", err)
	}
	if _, err := svc.Get(context.Background(), item.ID, "owner", "tenant", ""); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("tombstone lookup = %v", err)
	}
	if removed, err := svc.ProcessDeleting(context.Background(), 10); err != nil || removed != 1 {
		t.Fatalf("process deleting removed=%d err=%v", removed, err)
	}
	if _, err := store.Get(context.Background(), item.ObjectKey); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("provider object after worker = %v", err)
	}
}

func TestServiceCleanupDeletesExpiredObjects(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store, Config{MaxBytes: 100, AllowedMIMEs: []string{"text/plain"}, Clock: func() time.Time { return time.Unix(100, 0) }})
	old, err := svc.Upload(context.Background(), UploadInput{Name: "old.txt", MIME: "text/plain", Size: 1, Data: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	svc.clock = func() time.Time { return time.Unix(200, 0) }
	if err = svc.Cleanup(context.Background(), 50*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SignedURL(context.Background(), old.ID, "", time.Minute); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("cleanup lookup error = %v", err)
	}
}

func TestMemoryStoreProvidesLocalStoreContract(t *testing.T) {
	store := NewMemoryStore("https://objects.example/files")
	object := Object{Key: "k1", Name: "a.txt", MIME: "text/plain", Size: 1, Data: []byte("x")}
	if err := store.Put(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	url, err := store.SignURL(context.Background(), object.Key, time.Minute)
	if err != nil || url == "" {
		t.Fatalf("memory signed URL = %q, err = %v", url, err)
	}
	if err := store.Delete(context.Background(), object.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SignURL(context.Background(), object.Key, time.Minute); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("deleted object error = %v", err)
	}
}

func TestCategoriesEnforceScopeAndRejectCyclesOrNonEmptyDelete(t *testing.T) {
	svc := NewService(&fakeStore{}, Config{})
	root, err := svc.CreateCategory(context.Background(), CategoryInput{Name: "Root"}, "t1", "o1")
	if err != nil {
		t.Fatal(err)
	}
	child, err := svc.CreateCategory(context.Background(), CategoryInput{Name: "Child", ParentID: root.ID}, "t1", "o1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CreateCategory(context.Background(), CategoryInput{Name: "Cross", ParentID: root.ID}, "t2", "o1"); !errors.Is(err, ErrCategoryAccessDenied) {
		t.Fatalf("cross-scope parent err=%v", err)
	}
	if _, err = svc.UpdateCategory(context.Background(), root.ID, CategoryInput{ParentID: child.ID}, "t1", "o1"); !errors.Is(err, ErrInvalidCategory) {
		t.Fatalf("cycle err=%v", err)
	}
	if err = svc.DeleteCategory(context.Background(), root.ID, "t1", "o1"); !errors.Is(err, ErrCategoryNotEmpty) {
		t.Fatalf("non-empty delete err=%v", err)
	}
}

func TestUploadCategoryScopeAndListFilter(t *testing.T) {
	svc := NewService(&fakeStore{}, Config{})
	cat, err := svc.CreateCategory(context.Background(), CategoryInput{Name: "Images"}, "t1", "o1")
	if err != nil {
		t.Fatal(err)
	}
	item, err := svc.Upload(context.Background(), UploadInput{Name: "a", Size: 1, TenantID: "t1", OrgID: "o1", CategoryID: cat.ID, Data: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	if item.CategoryID != cat.ID {
		t.Fatalf("category id=%q", item.CategoryID)
	}
	if _, err = svc.Upload(context.Background(), UploadInput{Name: "x", Size: 1, TenantID: "t2", OrgID: "o1", CategoryID: cat.ID, Data: []byte("x")}); !errors.Is(err, ErrCategoryAccessDenied) {
		t.Fatalf("upload scope err=%v", err)
	}
	page, err := svc.List(context.Background(), ListFilter{TenantID: "t1", OrgID: "o1", CategoryID: cat.ID})
	if err != nil || page.Total != 1 || page.Items[0].ID != item.ID {
		t.Fatalf("filtered page=%+v err=%v", page, err)
	}
	if err = svc.DeleteCategory(context.Background(), cat.ID, "t1", "o1"); !errors.Is(err, ErrCategoryNotEmpty) {
		t.Fatalf("file-backed delete err=%v", err)
	}
}
