package file

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	objects map[string]Object
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
	if _, err := svc.Upload(context.Background(), UploadInput{Name: "a.exe", MIME: "application/octet-stream", Size: 1}); !errors.Is(err, ErrMIMETypeNotAllowed) {
		t.Fatalf("mime error = %v", err)
	}
}

func TestServiceSignsURLAndEnforcesACL(t *testing.T) {
	svc := NewService(&fakeStore{}, Config{MaxBytes: 100, AllowedMIMEs: []string{"text/plain"}})
	item, err := svc.Upload(context.Background(), UploadInput{Name: "note.txt", MIME: "text/plain", Size: 4, OwnerID: "u1", ACL: ACLPrivate})
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

func TestServiceCleanupDeletesExpiredObjects(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store, Config{MaxBytes: 100, AllowedMIMEs: []string{"text/plain"}, Clock: func() time.Time { return time.Unix(100, 0) }})
	old, err := svc.Upload(context.Background(), UploadInput{Name: "old.txt", MIME: "text/plain", Size: 1})
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
	object := Object{Key: "k1", Name: "a.txt", MIME: "text/plain", Size: 1}
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
