package settings

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// RuntimeSnapshot is the immutable, process-local view consumed by services
// whose management settings must take effect without restarting Go. Generation
// is diagnostic/cache metadata; callers continue using stable setting keys.
type RuntimeSnapshot struct {
	Generation uint64
	Values     map[string]json.RawMessage
	// Sources mirrors the authority used to build each value. Keeping source
	// metadata beside the immutable value lets administrative reads report the
	// true origin without a database round-trip.
	Sources   map[string]Source
	UpdatedAt time.Time
}

// SnapshotSubscriber receives a detached snapshot after a successful
// replacement. Subscribers should do short, non-blocking work; the store
// never invokes them while holding its lock.
type SnapshotSubscriber func(context.Context, RuntimeSnapshot)

type RuntimeSnapshotStore struct {
	mu sync.RWMutex
	// snapshot is the legacy/global slot used by callers that do not carry a
	// tenant scope. Scoped callers use scoped below; keeping the slot preserves
	// source compatibility for background jobs and older adapters while avoiding
	// accidental cross-tenant fallback in the service layer.
	snapshot    RuntimeSnapshot
	scoped      map[string]RuntimeSnapshot
	subscribers map[uint64]SnapshotSubscriber
	nextID      uint64
}

func NewRuntimeSnapshotStore() *RuntimeSnapshotStore {
	return &RuntimeSnapshotStore{
		subscribers: map[uint64]SnapshotSubscriber{},
		scoped:      map[string]RuntimeSnapshot{},
		snapshot:    RuntimeSnapshot{Values: map[string]json.RawMessage{}, Sources: map[string]Source{}},
	}
}

func (s *RuntimeSnapshotStore) Snapshot() RuntimeSnapshot {
	return s.SnapshotFor("")
}

// SnapshotFor returns the detached snapshot for one scope. An empty scope is
// the legacy process-wide slot. Non-empty scopes never fall back to that slot:
// a missing tenant snapshot is a cache miss and callers must resolve from the
// authoritative source instead of risking a value from another tenant.
func (s *RuntimeSnapshotStore) SnapshotFor(scope string) RuntimeSnapshot {
	if s == nil {
		return RuntimeSnapshot{Values: map[string]json.RawMessage{}, Sources: map[string]Source{}}
	}
	scope = normalizeSnapshotScope(scope)
	s.mu.RLock()
	snapshot := s.snapshot
	if scope != "" {
		if scoped, ok := s.scoped[scope]; ok {
			snapshot = scoped
		} else {
			snapshot = RuntimeSnapshot{Values: map[string]json.RawMessage{}, Sources: map[string]Source{}}
		}
	}
	value := cloneRuntimeSnapshot(snapshot)
	s.mu.RUnlock()
	return value
}

// Replace atomically swaps the complete final-state map and notifies current
// subscribers. Values are copied and validated as JSON before publication.
func (s *RuntimeSnapshotStore) Replace(ctx context.Context, values map[string]json.RawMessage) (RuntimeSnapshot, error) {
	return s.ReplaceWithSources(ctx, values, nil)
}

// ReplaceWithSources atomically swaps the complete value/source map and
// notifies subscribers after publication. Sources are optional for legacy
// callers; an omitted source remains unknown rather than being guessed.
func (s *RuntimeSnapshotStore) ReplaceWithSources(ctx context.Context, values map[string]json.RawMessage, sources map[string]Source) (RuntimeSnapshot, error) {
	return s.ReplaceWithSourcesFor("", ctx, values, sources)
}

// ReplaceWithSourcesFor atomically swaps the complete final-state map for one
// scope. The empty scope retains the historical process-wide behaviour.
func (s *RuntimeSnapshotStore) ReplaceWithSourcesFor(scope string, ctx context.Context, values map[string]json.RawMessage, sources map[string]Source) (RuntimeSnapshot, error) {
	if s == nil {
		return RuntimeSnapshot{}, ErrInvalidSetting
	}
	scope = normalizeSnapshotScope(scope)
	copyValues := make(map[string]json.RawMessage, len(values))
	copySources := make(map[string]Source, len(values))
	for key, raw := range values {
		if key == "" || len(raw) == 0 || !json.Valid(raw) {
			return RuntimeSnapshot{}, ErrInvalidSetting
		}
		copyValues[key] = append(json.RawMessage(nil), raw...)
		if source := sources[key]; source != "" {
			copySources[key] = source
		}
	}
	now := time.Now().UTC()
	s.mu.Lock()
	target := &s.snapshot
	if scope != "" {
		if s.scoped == nil {
			s.scoped = map[string]RuntimeSnapshot{}
		}
		scoped := s.scoped[scope]
		target = &scoped
		s.scoped[scope] = scoped
	}
	target.Generation++
	target.Values = copyValues
	target.Sources = copySources
	target.UpdatedAt = now
	if scope != "" {
		s.scoped[scope] = *target
	}
	value := cloneRuntimeSnapshot(*target)
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

func runtimeKeyBelongsToModule(module, key string) bool {
	module = strings.ToLower(strings.TrimSpace(module))
	key = strings.ToLower(strings.TrimSpace(key))
	if module == "" || key == "" {
		return false
	}
	return strings.HasPrefix(key, module+".") || (module == "basic" && key == "branding")
}

// Update changes one key while retaining the rest of the immutable snapshot.
func (s *RuntimeSnapshotStore) Update(ctx context.Context, key string, raw json.RawMessage) (RuntimeSnapshot, error) {
	return s.UpdateWithSource(ctx, key, raw, "")
}

// UpdateWithSource updates one key while retaining the rest of the immutable
// snapshot and records its effective source when supplied.
func (s *RuntimeSnapshotStore) UpdateWithSource(ctx context.Context, key string, raw json.RawMessage, source Source) (RuntimeSnapshot, error) {
	return s.UpdateWithSourceFor("", ctx, key, raw, source)
}

// UpdateWithSourceFor updates one key in a scope-local snapshot. It performs
// the read/modify/publish sequence under one lock, just like the legacy method.
func (s *RuntimeSnapshotStore) UpdateWithSourceFor(scope string, ctx context.Context, key string, raw json.RawMessage, source Source) (RuntimeSnapshot, error) {
	if s == nil || key == "" || len(raw) == 0 || !json.Valid(raw) {
		return RuntimeSnapshot{}, ErrInvalidSetting
	}
	scope = normalizeSnapshotScope(scope)
	// Do the read/modify/publish sequence under one lock.  Calling Snapshot
	// followed by Replace loses a concurrent update when two settings are
	// saved at the same time (the last writer would publish a stale map).
	copyRaw := append(json.RawMessage(nil), raw...)
	s.mu.Lock()
	target := &s.snapshot
	if scope != "" {
		if s.scoped == nil {
			s.scoped = map[string]RuntimeSnapshot{}
		}
		scoped := s.scoped[scope]
		target = &scoped
	}
	if target.Values == nil {
		target.Values = make(map[string]json.RawMessage)
	}
	if target.Sources == nil {
		target.Sources = make(map[string]Source)
	}
	target.Values[key] = copyRaw
	if source != "" {
		target.Sources[key] = source
	}
	target.Generation++
	target.UpdatedAt = time.Now().UTC()
	if scope != "" {
		s.scoped[scope] = *target
	}
	value := cloneRuntimeSnapshot(*target)
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

// Value returns a detached value from the current process-local snapshot.
// Business request handlers can use this method instead of consulting a
// database or Redis on every request.
func (s *RuntimeSnapshotStore) Value(key string) (json.RawMessage, bool) {
	return s.ValueFor("", key)
}

// ValueFor returns one key from a scope-local snapshot without falling back to
// the global slot.
func (s *RuntimeSnapshotStore) ValueFor(scope, key string) (json.RawMessage, bool) {
	if s == nil {
		return nil, false
	}
	scope = normalizeSnapshotScope(scope)
	s.mu.RLock()
	snapshot := s.snapshot
	if scope != "" {
		var exists bool
		snapshot, exists = s.scoped[scope]
		if !exists {
			s.mu.RUnlock()
			return nil, false
		}
	}
	raw, ok := snapshot.Values[strings.TrimSpace(key)]
	if ok {
		raw = append(json.RawMessage(nil), raw...)
	}
	s.mu.RUnlock()
	return raw, ok
}

// Source returns the effective authority recorded alongside a snapshot value.
// It performs no I/O and returns an empty value when the snapshot predates
// source metadata or the key is absent.
func (s *RuntimeSnapshotStore) Source(key string) Source {
	return s.SourceFor("", key)
}

// SourceFor returns source metadata from one scope-local snapshot.
func (s *RuntimeSnapshotStore) SourceFor(scope, key string) Source {
	if s == nil {
		return ""
	}
	scope = normalizeSnapshotScope(scope)
	s.mu.RLock()
	snapshot := s.snapshot
	if scope != "" {
		var exists bool
		snapshot, exists = s.scoped[scope]
		if !exists {
			s.mu.RUnlock()
			return ""
		}
	}
	source := snapshot.Sources[strings.TrimSpace(key)]
	s.mu.RUnlock()
	return source
}

// Module returns all keys belonging to a module.  The method only uses the
// stable key prefix convention and is primarily a fast read-side helper; the
// settings service remains the source of module definitions.
func (s *RuntimeSnapshotStore) Module(module string) map[string]json.RawMessage {
	return s.ModuleFor("", module)
}

// ModuleFor returns all keys belonging to a scope-local module.
func (s *RuntimeSnapshotStore) ModuleFor(scope, module string) map[string]json.RawMessage {
	result := map[string]json.RawMessage{}
	if s == nil {
		return result
	}
	scope = normalizeSnapshotScope(scope)
	module = strings.ToLower(strings.TrimSpace(module))
	s.mu.RLock()
	snapshot := s.snapshot
	if scope != "" {
		var exists bool
		snapshot, exists = s.scoped[scope]
		if !exists {
			s.mu.RUnlock()
			return result
		}
	}
	for key, raw := range snapshot.Values {
		belongs := strings.HasPrefix(strings.ToLower(key), module+".")
		if module == "basic" && key == "branding" {
			belongs = true
		}
		if belongs {
			result[key] = append(json.RawMessage(nil), raw...)
		}
	}
	s.mu.RUnlock()
	return result
}

// ReplaceModule atomically replaces only one module while retaining values
// from every other module. It is useful for a component-specific reload and
// preserves the immutable snapshot guarantee under concurrent updates.
func (s *RuntimeSnapshotStore) ReplaceModule(ctx context.Context, module string, values map[string]json.RawMessage) (RuntimeSnapshot, error) {
	return s.ReplaceModuleWithSources(ctx, module, values, nil)
}

// ReplaceModuleWithSources atomically replaces one module and its source
// metadata while retaining every other module.
func (s *RuntimeSnapshotStore) ReplaceModuleWithSources(ctx context.Context, module string, values map[string]json.RawMessage, sources map[string]Source) (RuntimeSnapshot, error) {
	return s.ReplaceModuleWithSourcesFor("", ctx, module, values, sources)
}

// ReplaceModuleWithSourcesFor replaces one module inside a scope-local
// snapshot while retaining values from every other module in that scope.
func (s *RuntimeSnapshotStore) ReplaceModuleWithSourcesFor(scope string, ctx context.Context, module string, values map[string]json.RawMessage, sources map[string]Source) (RuntimeSnapshot, error) {
	if s == nil {
		return RuntimeSnapshot{}, ErrInvalidSetting
	}
	scope = normalizeSnapshotScope(scope)
	module = strings.ToLower(strings.TrimSpace(module))
	if module == "" {
		return RuntimeSnapshot{}, ErrInvalidSetting
	}
	for key, raw := range values {
		if key == "" || !runtimeKeyBelongsToModule(module, key) || len(raw) == 0 || !json.Valid(raw) {
			return RuntimeSnapshot{}, ErrInvalidSetting
		}
	}
	s.mu.Lock()
	target := &s.snapshot
	if scope != "" {
		if s.scoped == nil {
			s.scoped = map[string]RuntimeSnapshot{}
		}
		scoped := s.scoped[scope]
		target = &scoped
	}
	if target.Values == nil {
		target.Values = map[string]json.RawMessage{}
	}
	if target.Sources == nil {
		target.Sources = map[string]Source{}
	}
	for key := range target.Values {
		belongs := strings.HasPrefix(strings.ToLower(key), module+".")
		if module == "basic" && key == "branding" {
			belongs = true
		}
		if belongs {
			delete(target.Values, key)
			delete(target.Sources, key)
		}
	}
	for key, raw := range values {
		target.Values[key] = append(json.RawMessage(nil), raw...)
		if source := sources[key]; source != "" {
			target.Sources[key] = source
		}
	}
	target.Generation++
	target.UpdatedAt = time.Now().UTC()
	if scope != "" {
		s.scoped[scope] = *target
	}
	value := cloneRuntimeSnapshot(*target)
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
	value.Sources = cloneSources(value.Sources)
	return value
}

func normalizeSnapshotScope(scope string) string {
	return strings.TrimSpace(scope)
}

func cloneSources(values map[string]Source) map[string]Source {
	result := make(map[string]Source, len(values))
	for key, source := range values {
		result[key] = source
	}
	return result
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
