// Package settings provides versioned, auditable runtime settings. It is
// storage/provider agnostic so B6 can run with memory or a database adapter;
// no exporter or external collector is created here.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const maskedValue = "[REDACTED]"

var (
	ErrInvalidSetting   = errors.New("invalid setting")
	ErrVersionConflict  = errors.New("setting version conflict")
	ErrSettingNotFound  = errors.New("setting not found")
	ErrPermissionDenied = errors.New("setting permission denied")
)

type ValueKind string

const (
	KindString ValueKind = "string"
	KindBool   ValueKind = "bool"
	KindNumber ValueKind = "number"
	KindJSON   ValueKind = "json"
	KindSecret ValueKind = "secret"
)

type Definition struct {
	Key       string
	Kind      ValueKind
	Sensitive bool
	Default   string
	Allowed   []string
}

func DefaultDefinitions() map[string]Definition {
	return map[string]Definition{
		"site.name":                         {Key: "site.name", Kind: KindString, Default: `"Gin-Vben-Admin"`},
		"locale.default":                    {Key: "locale.default", Kind: KindString, Default: `"zh-CN"`, Allowed: []string{"zh-CN", "en-US"}},
		"observability.metrics.enabled":     {Key: "observability.metrics.enabled", Kind: KindBool, Default: `false`},
		"observability.metrics.endpoint":    {Key: "observability.metrics.endpoint", Kind: KindString, Default: `""`},
		"observability.tracing.enabled":     {Key: "observability.tracing.enabled", Kind: KindBool, Default: `false`},
		"observability.tracing.endpoint":    {Key: "observability.tracing.endpoint", Kind: KindString, Default: `""`},
		"observability.tracing.protocol":    {Key: "observability.tracing.protocol", Kind: KindString, Default: `"http/protobuf"`, Allowed: []string{"grpc", "http/protobuf"}},
		"observability.tracing.sample_rate": {Key: "observability.tracing.sample_rate", Kind: KindNumber, Default: `0`},
		"observability.otlp.api_key":        {Key: "observability.otlp.api_key", Kind: KindSecret, Sensitive: true, Default: `""`},
	}
}

type Actor struct {
	ID string
}

type UpdateInput struct {
	Key             string
	Value           json.RawMessage
	ExpectedVersion int64
}

type RollbackInput struct {
	Key             string
	Version         int64
	ExpectedVersion int64
}

type Setting struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Version   int64     `json:"version"`
	Sensitive bool      `json:"sensitive"`
	UpdatedBy string    `json:"updatedBy"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type StoredSetting struct {
	Key       string
	RawValue  []byte
	Version   int64
	Sensitive bool
	UpdatedBy string
	UpdatedAt time.Time
}

type Repository interface {
	Current(context.Context, string) (StoredSetting, error)
	Append(context.Context, StoredSetting) (StoredSetting, error)
	History(context.Context, string) ([]StoredSetting, error)
}

type AuditEvent struct {
	ActorID string
	Action  string
	Key     string
	Version int64
}

type AuditSink interface {
	Record(context.Context, AuditEvent) error
}
type CacheInvalidator interface {
	Invalidate(context.Context, string) error
}
type Authorizer interface {
	Authorize(context.Context, Actor, string, string) error
}

type Service struct {
	repo        Repository
	audit       AuditSink
	cache       CacheInvalidator
	authorizer  Authorizer
	definitions map[string]Definition
}

func NewService(repo Repository, audit AuditSink, cache CacheInvalidator, definitions map[string]Definition) *Service {
	if definitions == nil {
		definitions = DefaultDefinitions()
	}
	return &Service{repo: repo, audit: audit, cache: cache, definitions: definitions}
}

func (s *Service) SetAuthorizer(authorizer Authorizer) { s.authorizer = authorizer }

// Definitions returns a stable, read-only schema view for administration UIs.
// Secret defaults are never exposed by this method; callers only receive the
// type and policy metadata needed to render a form.
func (s *Service) Definitions(ctx context.Context, actor Actor) ([]Definition, error) {
	if err := s.authorize(ctx, actor, "*", "read"); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrSettingNotFound
	}
	keys := make([]string, 0, len(s.definitions))
	for key := range s.definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]Definition, 0, len(keys))
	for _, key := range keys {
		definition := s.definitions[key]
		if definition.Sensitive {
			definition.Default = maskedValue
		}
		definition.Allowed = append([]string(nil), definition.Allowed...)
		items = append(items, definition)
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, actor Actor, key string) (Setting, error) {
	if err := s.authorize(ctx, actor, key, "read"); err != nil {
		return Setting{}, err
	}
	definition, ok := s.definitions[key]
	if !ok {
		return Setting{}, ErrSettingNotFound
	}
	record, err := s.repo.Current(ctx, key)
	if errors.Is(err, ErrSettingNotFound) {
		return s.present(StoredSetting{Key: key, RawValue: []byte(definition.Default), Sensitive: definition.Sensitive}, definition), nil
	}
	if err != nil {
		return Setting{}, err
	}
	return s.present(record, definition), nil
}

// History returns redacted versions in storage order so an operator can
// inspect a diff source before choosing an explicit rollback target.
func (s *Service) History(ctx context.Context, actor Actor, key string) ([]Setting, error) {
	if err := s.authorize(ctx, actor, key, "read"); err != nil {
		return nil, err
	}
	definition, ok := s.definitions[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	if s.repo == nil {
		return nil, errors.New("settings repository unavailable")
	}
	records, err := s.repo.History(ctx, key)
	if err != nil {
		return nil, err
	}
	items := make([]Setting, 0, len(records))
	for _, record := range records {
		items = append(items, s.present(record, definition))
	}
	return items, nil
}

func (s *Service) Update(ctx context.Context, actor Actor, input UpdateInput) (Setting, error) {
	if err := s.authorize(ctx, actor, input.Key, "write"); err != nil {
		return Setting{}, err
	}
	definition, ok := s.definitions[input.Key]
	if !ok {
		return Setting{}, ErrSettingNotFound
	}
	if err := validateValue(definition, input.Value); err != nil {
		return Setting{}, err
	}
	current, err := s.repo.Current(ctx, input.Key)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return Setting{}, err
	}
	if errors.Is(err, ErrSettingNotFound) {
		current.Version = 0
	}
	if current.Version != input.ExpectedVersion {
		return Setting{}, ErrVersionConflict
	}
	record, err := s.repo.Append(ctx, StoredSetting{Key: input.Key, RawValue: append([]byte(nil), input.Value...), Sensitive: definition.Sensitive, UpdatedBy: actor.ID, UpdatedAt: time.Now().UTC()})
	if err != nil {
		return Setting{}, err
	}
	if err := s.invalidateAndAudit(ctx, actor, input.Key, record.Version, "update"); err != nil {
		return Setting{}, err
	}
	return s.present(record, definition), nil
}

func (s *Service) Rollback(ctx context.Context, actor Actor, input RollbackInput) (Setting, error) {
	if err := s.authorize(ctx, actor, input.Key, "rollback"); err != nil {
		return Setting{}, err
	}
	definition, ok := s.definitions[input.Key]
	if !ok {
		return Setting{}, ErrSettingNotFound
	}
	current, err := s.repo.Current(ctx, input.Key)
	if err != nil {
		return Setting{}, err
	}
	if current.Version != input.ExpectedVersion {
		return Setting{}, ErrVersionConflict
	}
	history, err := s.repo.History(ctx, input.Key)
	if err != nil {
		return Setting{}, err
	}
	var source *StoredSetting
	for index := range history {
		if history[index].Version == input.Version {
			source = &history[index]
			break
		}
	}
	if source == nil {
		return Setting{}, ErrSettingNotFound
	}
	record, err := s.repo.Append(ctx, StoredSetting{Key: input.Key, RawValue: append([]byte(nil), source.RawValue...), Sensitive: definition.Sensitive, UpdatedBy: actor.ID, UpdatedAt: time.Now().UTC()})
	if err != nil {
		return Setting{}, err
	}
	if err := s.invalidateAndAudit(ctx, actor, input.Key, record.Version, "rollback"); err != nil {
		return Setting{}, err
	}
	return s.present(record, definition), nil
}

func (s *Service) authorize(ctx context.Context, actor Actor, key, action string) error {
	if strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(key) == "" || strings.TrimSpace(action) == "" {
		return ErrPermissionDenied
	}
	if s.authorizer != nil {
		return s.authorizer.Authorize(ctx, actor, key, action)
	}
	return nil
}

func (s *Service) invalidateAndAudit(ctx context.Context, actor Actor, key string, version int64, action string) error {
	if s.cache != nil {
		if err := s.cache.Invalidate(ctx, key); err != nil {
			return err
		}
	}
	if s.audit != nil {
		if err := s.audit.Record(ctx, AuditEvent{ActorID: actor.ID, Action: action, Key: key, Version: version}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) present(record StoredSetting, definition Definition) Setting {
	value := string(record.RawValue)
	if definition.Sensitive {
		value = maskedValue
	}
	return Setting{Key: record.Key, Value: value, Version: record.Version, Sensitive: definition.Sensitive, UpdatedBy: record.UpdatedBy, UpdatedAt: record.UpdatedAt}
}

func validateValue(definition Definition, raw json.RawMessage) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return ErrInvalidSetting
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return ErrInvalidSetting
	}
	switch definition.Kind {
	case KindString, KindSecret:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%w: %s expects string", ErrInvalidSetting, definition.Key)
		}
	case KindBool:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%w: %s expects boolean", ErrInvalidSetting, definition.Key)
		}
	case KindNumber:
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("%w: %s expects number", ErrInvalidSetting, definition.Key)
		}
	case KindJSON:
	default:
		return ErrInvalidSetting
	}
	if len(definition.Allowed) > 0 {
		valueString, ok := value.(string)
		if !ok {
			return ErrInvalidSetting
		}
		for _, allowed := range definition.Allowed {
			if valueString == allowed {
				return nil
			}
		}
		return fmt.Errorf("%w: %s value is not allowed", ErrInvalidSetting, definition.Key)
	}
	return nil
}

// MemoryRepository is a deterministic adapter for tests and local bootstrap.
type MemoryRepository struct {
	mu     sync.RWMutex
	values map[string][]StoredSetting
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{values: map[string][]StoredSetting{}}
}
func (r *MemoryRepository) Current(_ context.Context, key string) (StoredSetting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := r.values[key]
	if len(values) == 0 {
		return StoredSetting{}, ErrSettingNotFound
	}
	return cloneStored(values[len(values)-1]), nil
}
func (r *MemoryRepository) Append(_ context.Context, value StoredSetting) (StoredSetting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value.Version = int64(len(r.values[value.Key]) + 1)
	r.values[value.Key] = append(r.values[value.Key], cloneStored(value))
	return cloneStored(value), nil
}
func (r *MemoryRepository) History(_ context.Context, key string) ([]StoredSetting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	values := r.values[key]
	if len(values) == 0 {
		return nil, ErrSettingNotFound
	}
	out := make([]StoredSetting, len(values))
	for i := range values {
		out[i] = cloneStored(values[i])
	}
	return out, nil
}
func cloneStored(value StoredSetting) StoredSetting {
	value.RawValue = append([]byte(nil), value.RawValue...)
	return value
}
