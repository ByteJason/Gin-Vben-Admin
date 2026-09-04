package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServiceListsDownloadsAndDeletesWithinTenant(t *testing.T) {
	store := NewMemoryStore("https://files.example.local")
	svc := NewService(store, Config{MaxBytes: 100, AllowedMIMEs: []string{"text/plain"}})
	item, err := svc.Upload(context.Background(), UploadInput{
		Name: "note.txt", MIME: "text/plain", Size: 5, OwnerID: "u1", TenantID: "tenant-a", Data: []byte("hello"),
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := svc.List(context.Background(), ListFilter{TenantID: "tenant-a"})
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("list = %#v, err = %v", page, err)
	}
	if _, _, err = svc.Download(context.Background(), item.ID, "u1", "tenant-b", ""); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("cross-tenant download error = %v", err)
	}
	metadata, object, err := svc.Download(context.Background(), item.ID, "u1", "tenant-a", "")
	if err != nil || metadata.SHA256 == "" || string(object.Data) != "hello" {
		t.Fatalf("download = %#v, %#v, err = %v", metadata, object, err)
	}
	if err = svc.DeleteFile(context.Background(), item.ID, "u1", "tenant-a", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.List(context.Background(), ListFilter{TenantID: "tenant-a"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.Download(context.Background(), item.ID, "u1", "tenant-a", ""); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("deleted download error = %v", err)
	}
}

func TestLocalStoreRejectsTraversalAndPersistsBytes(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root, "http://localhost:8080/files")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put(context.Background(), Object{Key: "../escape", Data: []byte("x")}); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("traversal error = %v", err)
	}
	object := Object{Key: "tenant-a/object-1", Name: "a.txt", MIME: "text/plain", Size: 5, Data: []byte("hello")}
	if err = store.Put(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), object.Key)
	if err != nil || string(got.Data) != "hello" {
		t.Fatalf("get = %#v, err = %v", got, err)
	}
	if _, err = os.Stat(filepath.Join(root, "tenant-a", "object-1")); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SignURL(context.Background(), object.Key, time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestServiceCleanupDryRunDoesNotDelete(t *testing.T) {
	clock := func() time.Time { return time.Unix(1000, 0) }
	store := NewMemoryStore("")
	svc := NewService(store, Config{MaxBytes: 100, Clock: clock})
	item, err := svc.Upload(context.Background(), UploadInput{Name: "old.txt", MIME: "text/plain", Size: 3, TenantID: "tenant-a", ACL: ACLPublicRead, Data: []byte("old")})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetClock(func() time.Time { return time.Unix(1200, 0) })
	report, err := svc.CleanupDryRun(context.Background(), 100*time.Second)
	if err != nil || report.MatchingCount != 1 || report.Bytes != 3 {
		t.Fatalf("dry run = %#v, err = %v", report, err)
	}
	if _, _, err = svc.Download(context.Background(), item.ID, "", "tenant-a", ""); err != nil {
		t.Fatalf("dry run deleted object: %v", err)
	}
}

func TestServiceRejectsMismatchedDeclaredSize(t *testing.T) {
	svc := NewService(NewMemoryStore(""), Config{MaxBytes: 100})
	_, err := svc.Upload(context.Background(), UploadInput{Name: "x.txt", MIME: "text/plain", Size: 3, Data: []byte("too long")})
	if !errors.Is(err, ErrInvalidUpload) || !strings.Contains(err.Error(), "size") {
		t.Fatalf("mismatched size error = %v", err)
	}
}
