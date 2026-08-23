// Package tasks owns the tenant-scoped application seam for declarative jobs.
// Execution is deliberately delegated to application/jobs; this package never
// interprets a payload as a command or shell fragment.
package tasks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/task"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

type TaskDefinition = domain.TaskDefinition

var (
	ErrNotFound          = errors.New("task definition not found")
	ErrConflict          = errors.New("task definition already exists")
	ErrRepositoryMissing = errors.New("task repository unavailable")
)

// Repository is the durable boundary. Implementations must apply tenant and
// organization scope to every read/write and preserve soft-deleted rows.
type Repository interface {
	Save(context.Context, TaskDefinition) error
	Get(context.Context, string, string, string) (TaskDefinition, error)
	List(context.Context, string, string) ([]TaskDefinition, error)
	Delete(context.Context, string, string, string) error
}

// MemoryRepository mirrors the SQL uniqueness and soft-delete rules for local
// fixtures and unit tests.
type MemoryRepository struct {
	mu   sync.RWMutex
	data map[string]TaskDefinition
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{data: make(map[string]TaskDefinition)}
}

// NewInMemoryRepository is an explicit alias for callers describing the
// adapter by storage strategy.
func NewInMemoryRepository() *MemoryRepository { return NewMemoryRepository() }

func (r *MemoryRepository) Save(ctx context.Context, d TaskDefinition) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := d.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.data == nil {
		r.data = make(map[string]TaskDefinition)
	}
	for id, existing := range r.data {
		if id != d.ID && existing.DeletedAt == nil && existing.TenantID == d.TenantID && existing.OrgID == d.OrgID && strings.EqualFold(existing.Name, d.Name) {
			return ErrConflict
		}
	}
	now := time.Now().UTC()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	if d.ConcurrencyPolicy == "" {
		d.ConcurrencyPolicy = "forbid"
	}
	if d.Timeout <= 0 && d.TimeoutSeconds > 0 {
		d.Timeout = time.Duration(d.TimeoutSeconds) * time.Second
	}
	if d.TimeoutSeconds <= 0 && d.Timeout > 0 {
		d.TimeoutSeconds = int(d.Timeout / time.Second)
	}
	r.data[d.ID] = cloneDefinition(d)
	return nil
}

func (r *MemoryRepository) Get(ctx context.Context, id, tenantID, orgID string) (TaskDefinition, error) {
	if err := contextErr(ctx); err != nil {
		return TaskDefinition{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.data[strings.TrimSpace(id)]
	if !ok || d.DeletedAt != nil || d.TenantID != tenantID || (orgID != "" && d.OrgID != orgID) {
		return TaskDefinition{}, ErrNotFound
	}
	return cloneDefinition(d), nil
}

func (r *MemoryRepository) List(ctx context.Context, tenantID, orgID string) ([]TaskDefinition, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]TaskDefinition, 0, len(r.data))
	for _, d := range r.data {
		if d.DeletedAt == nil && d.TenantID == tenantID && (orgID == "" || d.OrgID == orgID) {
			out = append(out, cloneDefinition(d))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *MemoryRepository) Delete(ctx context.Context, id, tenantID, orgID string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.data[strings.TrimSpace(id)]
	if !ok || d.DeletedAt != nil || d.TenantID != tenantID || (orgID != "" && d.OrgID != orgID) {
		return ErrNotFound
	}
	now := time.Now().UTC()
	d.DeletedAt = &now
	d.UpdatedAt = now
	r.data[d.ID] = d
	return nil
}

type Service struct {
	repo  Repository
	clock func() time.Time
}

func NewService(repo Repository) *Service { return &Service{repo: repo, clock: time.Now} }

func (s *Service) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.clock = clock
	}
}

func (s *Service) Create(ctx context.Context, d TaskDefinition) (TaskDefinition, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return TaskDefinition{}, err
	}
	if s == nil || s.repo == nil {
		return TaskDefinition{}, ErrRepositoryMissing
	}
	d.TenantID, d.OrgID = scope.TenantID, scope.Organization
	if strings.TrimSpace(d.ID) == "" {
		d.ID = newID("task")
	}
	d.ConcurrencyPolicy = normalizedPolicy(d.ConcurrencyPolicy)
	if d.Timeout <= 0 && d.TimeoutSeconds > 0 {
		d.Timeout = time.Duration(d.TimeoutSeconds) * time.Second
	}
	if d.Timeout <= 0 {
		d.Timeout = 30 * time.Second
	}
	if d.TimeoutSeconds <= 0 {
		d.TimeoutSeconds = int(d.Timeout / time.Second)
	}
	if d.MaxAttempts <= 0 {
		d.MaxAttempts = 3
	}
	if d.Concurrency <= 0 {
		d.Concurrency = 1
	}
	if d.Timezone == "" {
		d.Timezone = "UTC"
	}
	if err := d.Validate(); err != nil {
		return TaskDefinition{}, err
	}
	if err := s.repo.Save(ctx, d); err != nil {
		return TaskDefinition{}, err
	}
	return s.Get(ctx, d.ID)
}

func (s *Service) Update(ctx context.Context, id string, d TaskDefinition) (TaskDefinition, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return TaskDefinition{}, err
	}
	if s == nil || s.repo == nil {
		return TaskDefinition{}, ErrRepositoryMissing
	}
	existing, err := s.repo.Get(ctx, id, scope.TenantID, scope.Organization)
	if err != nil {
		return TaskDefinition{}, err
	}
	d.ID, d.TenantID, d.OrgID = existing.ID, existing.TenantID, existing.OrgID
	d.CreatedAt = existing.CreatedAt
	if d.Timezone == "" {
		d.Timezone = existing.Timezone
	}
	if len(d.PayloadSchema) == 0 {
		d.PayloadSchema = existing.PayloadSchema
	}
	if d.Type == "" {
		d.Type = existing.Type
	}
	if d.Name == "" {
		d.Name = existing.Name
	}
	if d.Concurrency <= 0 {
		d.Concurrency = existing.Concurrency
	}
	if d.Timeout <= 0 {
		d.Timeout = existing.Timeout
	}
	if d.TimeoutSeconds <= 0 {
		d.TimeoutSeconds = existing.TimeoutSeconds
	}
	if d.MaxAttempts <= 0 {
		d.MaxAttempts = existing.MaxAttempts
	}
	if d.ConcurrencyPolicy == "" {
		d.ConcurrencyPolicy = existing.ConcurrencyPolicy
	}
	if err := d.Validate(); err != nil {
		return TaskDefinition{}, err
	}
	if err := s.repo.Save(ctx, d); err != nil {
		return TaskDefinition{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Get(ctx context.Context, id string) (TaskDefinition, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return TaskDefinition{}, err
	}
	if s == nil || s.repo == nil {
		return TaskDefinition{}, ErrRepositoryMissing
	}
	return s.repo.Get(ctx, id, scope.TenantID, scope.Organization)
}

func (s *Service) List(ctx context.Context) ([]TaskDefinition, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if s == nil || s.repo == nil {
		return nil, ErrRepositoryMissing
	}
	return s.repo.List(ctx, scope.TenantID, scope.Organization)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	if s == nil || s.repo == nil {
		return ErrRepositoryMissing
	}
	return s.repo.Delete(ctx, id, scope.TenantID, scope.Organization)
}

func normalizedPolicy(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "allow" || value == "replace" {
		return value
	}
	return "forbid"
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func cloneDefinition(d TaskDefinition) TaskDefinition {
	d.PayloadSchema = append([]byte(nil), d.PayloadSchema...)
	return d
}

func newID(prefix string) string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return prefix + "-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return prefix + "-" + hex.EncodeToString(raw[:])
}
