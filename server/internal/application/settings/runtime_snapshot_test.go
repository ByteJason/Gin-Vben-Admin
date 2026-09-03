package settings

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

func TestRuntimeSnapshotPublishesImmutableValuesAndUnsubscribe(t *testing.T) {
	store := NewRuntimeSnapshotStore()
	var mu sync.Mutex
	var received []RuntimeSnapshot
	unsubscribe := store.Subscribe(func(_ context.Context, snapshot RuntimeSnapshot) {
		mu.Lock()
		received = append(received, snapshot)
		mu.Unlock()
	})
	first, err := store.Replace(context.Background(), map[string]json.RawMessage{"mail.caller": json.RawMessage(`"billing"`)})
	if err != nil || first.Generation != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	first.Values["mail.caller"][0] = 'x'
	if got := string(store.Snapshot().Values["mail.caller"]); got != `"billing"` {
		t.Fatalf("snapshot mutated through returned map: %s", got)
	}
	unsubscribe()
	if _, err := store.Update(context.Background(), "mail.caller", json.RawMessage(`"security"`)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	count := len(received)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("subscriber count=%d", count)
	}
}

func TestSettingsUpdatePublishesRuntimeSnapshot(t *testing.T) {
	repo := NewMemoryRepository()
	store := NewRuntimeSnapshotStore()
	service := NewService(repo, nil, nil, nil)
	service.SetRuntimeSnapshotStore(store)
	setting, err := service.Update(context.Background(), Actor{ID: "admin"}, UpdateInput{Key: "mail.port", Value: json.RawMessage(`2525`), ExpectedVersion: 0})
	if err != nil {
		t.Fatal(err)
	}
	if setting.Key != "mail.port" {
		t.Fatalf("setting=%+v", setting)
	}
	if got := string(store.Snapshot().Values["mail.port"]); got != "2525" {
		t.Fatalf("published value=%s", got)
	}
}

func TestBrandingSettingRoundTripsAsStructuredJSON(t *testing.T) {
	repo := NewMemoryRepository()
	store := NewRuntimeSnapshotStore()
	service := NewService(repo, nil, nil, nil)
	service.SetRuntimeSnapshotStore(store)

	const raw = `{"logoResourceId":"asset-1"}`
	if _, err := service.Update(context.Background(), Actor{ID: "admin"}, UpdateInput{
		Key:             "branding",
		Value:           json.RawMessage(raw),
		ExpectedVersion: 0,
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Current(context.Background(), "branding")
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.RawValue) != raw {
		t.Fatalf("stored branding value=%q, want object JSON", stored.RawValue)
	}
	setting, err := service.Get(context.Background(), Actor{ID: "admin"}, "branding")
	if err != nil {
		t.Fatal(err)
	}
	if setting.Value != raw {
		t.Fatalf("present branding value=%q, want %q", setting.Value, raw)
	}
	if got := string(store.Snapshot().Values["branding"]); got != raw {
		t.Fatalf("runtime branding value=%q, want %q", got, raw)
	}
}

func TestSensitiveSettingIsNotPublishedToRuntimeSnapshot(t *testing.T) {
	repo := NewMemoryRepository()
	store := NewRuntimeSnapshotStore()
	service := NewService(repo, nil, nil, nil)
	service.SetRuntimeSnapshotStore(store)
	if _, err := service.Update(context.Background(), Actor{ID: "admin"}, UpdateInput{
		Key:             "observability.otlp.api_key",
		Value:           json.RawMessage(`"secret"`),
		ExpectedVersion: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.Snapshot().Values["observability.otlp.api_key"]; exists {
		t.Fatal("sensitive value leaked into runtime snapshot")
	}
}

func TestRuntimeSnapshotConcurrentUpdatesKeepBothKeys(t *testing.T) {
	store := NewRuntimeSnapshotStore()
	var wg sync.WaitGroup
	for key, value := range map[string]json.RawMessage{"mail.host": json.RawMessage(`"smtp"`), "mail.port": json.RawMessage(`2525`)} {
		key, value := key, value
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.Update(context.Background(), key, value); err != nil {
				t.Errorf("Update(%s): %v", key, err)
			}
		}()
	}
	wg.Wait()
	snapshot := store.Snapshot()
	if string(snapshot.Values["mail.host"]) != `"smtp"` || string(snapshot.Values["mail.port"]) != "2525" {
		t.Fatalf("concurrent update lost a key: %#v", snapshot.Values)
	}
}
