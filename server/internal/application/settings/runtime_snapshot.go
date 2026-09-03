package settings

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// RuntimeSnapshot is the immutable, process-local view consumed by services
// whose management settings must take effect without restarting Go. Generation
// is diagnostic/cache metadata; callers continue using stable setting keys.
type RuntimeSnapshot struct {
	Generation uint64
	Values     map[string]json.RawMessage
	UpdatedAt  time.Time
}

// SnapshotSubscriber receives a detached snapshot after a successful
// replacement. Subscribers should do short, non-blocking work; the store
// never invokes them while holding its lock.
type SnapshotSubscriber func(context.Context, RuntimeSnapshot)

type RuntimeSnapshotStore struct {
	mu          sync.RWMutex
	snapshot    RuntimeSnapshot
	subscribers map[uint64]SnapshotSubscriber
	nextID      uint64
}

func NewRuntimeSnapshotStore() *RuntimeSnapshotStore {
	return &RuntimeSnapshotStore{subscribers: map[uint64]SnapshotSubscriber{}, snapshot: RuntimeSnapshot{Values: map[string]json.RawMessage{}}}
}

func (s *RuntimeSnapshotStore) Snapshot() RuntimeSnapshot {
	if s == nil {
		return RuntimeSnapshot{Values: map[string]json.RawMessage{}}
	}
	s.mu.RLock()
	value := cloneRuntimeSnapshot(s.snapshot)
	s.mu.RUnlock()
	return value
}

// Replace atomically swaps the complete final-state map and notifies current
// subscribers. Values are copied and validated as JSON before publication.
func (s *RuntimeSnapshotStore) Replace(ctx context.Context, values map[string]json.RawMessage) (RuntimeSnapshot, error) {
	if s == nil {
		return RuntimeSnapshot{}, ErrInvalidSetting
	}
	copyValues := make(map[string]json.RawMessage, len(values))
	for key, raw := range values {
		if key == "" || len(raw) == 0 || !json.Valid(raw) {
			return RuntimeSnapshot{}, ErrInvalidSetting
		}
		copyValues[key] = append(json.RawMessage(nil), raw...)
	}
	now := time.Now().UTC()
	s.mu.Lock()
	s.snapshot.Generation++
	s.snapshot.Values = copyValues
	s.snapshot.UpdatedAt = now
	value := cloneRuntimeSnapshot(s.snapshot)
	subscribers := make([]SnapshotSubscriber, 0, len(s.subscribers))
	for _, subscriber := range s.subscribers {
		if subscriber != nil {
			subscribers = append(subscribers, subscriber)
		}
	}
	s.mu.Unlock()
	for _, subscriber := range subscribers {
		if ctx == nil {
			ctx = context.Background()
		}
		safeNotify(subscriber, ctx, value)
	}
	return value, nil
}

// Update changes one key while retaining the rest of the immutable snapshot.
func (s *RuntimeSnapshotStore) Update(ctx context.Context, key string, raw json.RawMessage) (RuntimeSnapshot, error) {
	if s == nil || key == "" || len(raw) == 0 || !json.Valid(raw) {
		return RuntimeSnapshot{}, ErrInvalidSetting
	}
	// Do the read/modify/publish sequence under one lock.  Calling Snapshot
	// followed by Replace loses a concurrent update when two settings are
	// saved at the same time (the last writer would publish a stale map).
	copyRaw := append(json.RawMessage(nil), raw...)
	s.mu.Lock()
	if s.snapshot.Values == nil {
		s.snapshot.Values = make(map[string]json.RawMessage)
	}
	s.snapshot.Values[key] = copyRaw
	s.snapshot.Generation++
	s.snapshot.UpdatedAt = time.Now().UTC()
	value := cloneRuntimeSnapshot(s.snapshot)
	subscribers := make([]SnapshotSubscriber, 0, len(s.subscribers))
	for _, subscriber := range s.subscribers {
		if subscriber != nil {
			subscribers = append(subscribers, subscriber)
		}
	}
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	for _, subscriber := range subscribers {
		safeNotify(subscriber, ctx, value)
	}
	return value, nil
}

func (s *RuntimeSnapshotStore) Subscribe(subscriber SnapshotSubscriber) (unsubscribe func()) {
	if s == nil || subscriber == nil {
		return func() {}
	}
	s.mu.Lock()
	s.nextID++
	id := s.nextID
	s.subscribers[id] = subscriber
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.subscribers, id)
		s.mu.Unlock()
	}
}

func cloneRuntimeSnapshot(value RuntimeSnapshot) RuntimeSnapshot {
	value.Values = cloneRawValues(value.Values)
	return value
}

func cloneRawValues(values map[string]json.RawMessage) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(values))
	for key, raw := range values {
		result[key] = append(json.RawMessage(nil), raw...)
	}
	return result
}

func safeNotify(subscriber SnapshotSubscriber, ctx context.Context, value RuntimeSnapshot) {
	defer func() { _ = recover() }()
	subscriber(ctx, cloneRuntimeSnapshot(value))
}
