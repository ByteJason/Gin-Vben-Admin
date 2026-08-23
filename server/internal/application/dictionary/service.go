// Package dictionary provides tenant-aware dictionary types and items.
// System-owned rows are immutable; tenant and organization rows can override
// the effective system catalog without mutating the shared seed.
package dictionary

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

var (
	ErrInvalidType       = errors.New("invalid dictionary type")
	ErrInvalidItem       = errors.New("invalid dictionary item")
	ErrTypeNotFound      = errors.New("dictionary type not found")
	ErrItemNotFound      = errors.New("dictionary item not found")
	ErrTypeConflict      = errors.New("dictionary type already exists")
	ErrItemConflict      = errors.New("dictionary item already exists")
	ErrSystemReadOnly    = errors.New("system dictionary is read-only")
	ErrRepositoryMissing = errors.New("dictionary repository unavailable")
	ErrImportLimit       = errors.New("dictionary import limit exceeded")
)

const MaxImportItems = 500

type DictionaryType struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenantId,omitempty"`
	OrgID        string     `json:"orgId,omitempty"`
	Code         string     `json:"code"`
	NameZhCN     string     `json:"nameZhCN"`
	NameEnUS     string     `json:"nameEnUS"`
	Description  string     `json:"description,omitempty"`
	Status       string     `json:"status"`
	SortOrder    int        `json:"sortOrder"`
	SystemOwned  bool       `json:"systemOwned"`
	CacheVersion int64      `json:"cacheVersion"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	DeletedAt    *time.Time `json:"deletedAt,omitempty"`
}

type DictionaryItem struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenantId,omitempty"`
	OrgID        string     `json:"orgId,omitempty"`
	TypeCode     string     `json:"typeCode"`
	Value        string     `json:"value"`
	LabelZhCN    string     `json:"labelZhCN"`
	LabelEnUS    string     `json:"labelEnUS"`
	Label        string     `json:"label"`
	Description  string     `json:"description,omitempty"`
	Tag          string     `json:"tag,omitempty"`
	Status       string     `json:"status"`
	SortOrder    int        `json:"sortOrder"`
	SystemOwned  bool       `json:"systemOwned"`
	CacheVersion int64      `json:"cacheVersion"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	DeletedAt    *time.Time `json:"deletedAt,omitempty"`
}

type TypeInput struct {
	Code        string `json:"code"`
	NameZhCN    string `json:"nameZhCN"`
	NameEnUS    string `json:"nameEnUS"`
	Description string `json:"description"`
	Status      string `json:"status"`
	SortOrder   int    `json:"sortOrder"`
}

type ItemInput struct {
	Value       string `json:"value"`
	LabelZhCN   string `json:"labelZhCN"`
	LabelEnUS   string `json:"labelEnUS"`
	Description string `json:"description"`
	Tag         string `json:"tag"`
	Status      string `json:"status"`
	SortOrder   int    `json:"sortOrder"`
	Enabled     bool   `json:"enabled"`
}

type ListOptions struct {
	Locale          string
	IncludeDisabled bool
}

type AuditEvent struct {
	ActorID   string
	Action    string
	Resource  string
	TypeCode  string
	ItemID    string
	Version   int64
	CreatedAt time.Time
}

type AuditSink interface {
	Record(context.Context, AuditEvent) error
}

// Repository is the durable boundary. Implementations must scope every
// tenant/local query and perform CreateItems atomically for import requests.
type Repository interface {
	ListTypes(context.Context, string, string, bool) ([]DictionaryType, error)
	FindType(context.Context, string) (DictionaryType, error)
	FindTypeByScope(context.Context, string, string, string, bool) (DictionaryType, error)
	CreateType(context.Context, DictionaryType) (DictionaryType, error)
	UpdateType(context.Context, DictionaryType) (DictionaryType, error)
	SoftDeleteType(context.Context, string, string, string, time.Time) error
	ListItems(context.Context, string, string, string, bool) ([]DictionaryItem, error)
	FindItem(context.Context, string, string, string, string, bool) (DictionaryItem, error)
	CreateItems(context.Context, []DictionaryItem) ([]DictionaryItem, error)
	UpdateItem(context.Context, DictionaryItem) (DictionaryItem, error)
	SoftDeleteItem(context.Context, string, string, string, time.Time) error
	BumpVersion(context.Context, string, string, string, time.Time) (int64, error)
	CurrentVersion(context.Context, string, string, string) (int64, error)
}

type Service struct {
	repo  Repository
	audit AuditSink
	clock func() time.Time
}

func NewService(repo Repository, audit AuditSink) *Service {
	return &Service{repo: repo, audit: audit, clock: time.Now}
}

func (s *Service) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.clock = clock
	}
}

func WithActor(ctx context.Context, actorID string) context.Context {
	return context.WithValue(ctx, actorKey{}, strings.TrimSpace(actorID))
}

func actorFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if actor, ok := ctx.Value(actorKey{}).(string); ok {
		return actor
	}
	return ""
}

type actorKey struct{}

func (s *Service) ListTypes(ctx context.Context, options ListOptions) ([]DictionaryType, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if s == nil || s.repo == nil {
		return nil, ErrRepositoryMissing
	}
	rows, err := s.repo.ListTypes(ctx, scope.TenantID, scope.Organization, options.IncludeDisabled)
	if err != nil {
		return nil, fmt.Errorf("%w: list types", ErrRepositoryMissing)
	}
	merged := mergeTypes(rows, scope)
	for i := range merged {
		version, versionErr := s.repoVersion(ctx, scope, merged[i].Code)
		if versionErr != nil {
			return nil, versionErr
		}
		merged[i].CacheVersion = version
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].SortOrder == merged[j].SortOrder {
			return merged[i].Code < merged[j].Code
		}
		return merged[i].SortOrder < merged[j].SortOrder
	})
	return merged, nil
}

func (s *Service) CreateType(ctx context.Context, input TypeInput) (DictionaryType, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return DictionaryType{}, err
	}
	if s == nil || s.repo == nil {
		return DictionaryType{}, ErrRepositoryMissing
	}
	input, err = normalizeTypeInput(input, true)
	if err != nil {
		return DictionaryType{}, err
	}
	now := s.now()
	typeRow := DictionaryType{ID: newID("dict-type"), TenantID: scope.TenantID, OrgID: scope.Organization, Code: input.Code, NameZhCN: input.NameZhCN, NameEnUS: input.NameEnUS, Description: input.Description, Status: normalizedStatus(input.Status), SortOrder: input.SortOrder, CreatedAt: now, UpdatedAt: now}
	created, err := s.repo.CreateType(ctx, typeRow)
	if err != nil {
		return DictionaryType{}, err
	}
	version, err := s.repo.BumpVersion(ctx, scope.TenantID, scope.Organization, created.Code, now)
	if err != nil {
		return DictionaryType{}, err
	}
	created.CacheVersion = version
	if err := s.record(ctx, AuditEvent{ActorID: actorFromContext(ctx), Action: "dictionary.type.create", Resource: "dictionary", TypeCode: created.Code, Version: version, CreatedAt: now}); err != nil {
		return DictionaryType{}, err
	}
	return created, nil
}

func (s *Service) UpdateType(ctx context.Context, code string, input TypeInput) (DictionaryType, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return DictionaryType{}, err
	}
	if s == nil || s.repo == nil {
		return DictionaryType{}, ErrRepositoryMissing
	}
	local, err := s.repo.FindTypeByScope(ctx, code, scope.TenantID, scope.Organization, true)
	if errors.Is(err, ErrTypeNotFound) {
		return DictionaryType{}, ErrSystemReadOnly
	}
	if err != nil {
		return DictionaryType{}, err
	}
	if local.DeletedAt != nil {
		return DictionaryType{}, ErrTypeNotFound
	}
	if local.SystemOwned {
		return DictionaryType{}, ErrSystemReadOnly
	}
	input, err = normalizeTypeInputForUpdate(input, local)
	if err != nil {
		return DictionaryType{}, err
	}
	local.NameZhCN, local.NameEnUS, local.Description, local.Status, local.SortOrder = input.NameZhCN, input.NameEnUS, input.Description, normalizedStatus(input.Status), input.SortOrder
	local.UpdatedAt = s.now()
	updated, err := s.repo.UpdateType(ctx, local)
	if err != nil {
		return DictionaryType{}, err
	}
	version, err := s.repo.BumpVersion(ctx, scope.TenantID, scope.Organization, updated.Code, local.UpdatedAt)
	if err != nil {
		return DictionaryType{}, err
	}
	updated.CacheVersion = version
	if err := s.record(ctx, AuditEvent{ActorID: actorFromContext(ctx), Action: "dictionary.type.update", Resource: "dictionary", TypeCode: updated.Code, Version: version, CreatedAt: local.UpdatedAt}); err != nil {
		return DictionaryType{}, err
	}
	return updated, nil
}

func (s *Service) DeleteType(ctx context.Context, code string) error {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	if s == nil || s.repo == nil {
		return ErrRepositoryMissing
	}
	local, err := s.repo.FindTypeByScope(ctx, code, scope.TenantID, scope.Organization, true)
	if errors.Is(err, ErrTypeNotFound) {
		return ErrSystemReadOnly
	}
	if err != nil {
		return err
	}
	if local.SystemOwned {
		return ErrSystemReadOnly
	}
	now := s.now()
	if err := s.repo.SoftDeleteType(ctx, local.ID, scope.TenantID, scope.Organization, now); err != nil {
		return err
	}
	version, err := s.repo.BumpVersion(ctx, scope.TenantID, scope.Organization, local.Code, now)
	if err != nil {
		return err
	}
	return s.record(ctx, AuditEvent{ActorID: actorFromContext(ctx), Action: "dictionary.type.delete", Resource: "dictionary", TypeCode: local.Code, Version: version, CreatedAt: now})
}

func (s *Service) ListItems(ctx context.Context, typeCode string, options ListOptions) ([]DictionaryItem, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if s == nil || s.repo == nil {
		return nil, ErrRepositoryMissing
	}
	typeCode = strings.TrimSpace(typeCode)
	if typeCode == "" {
		return nil, ErrInvalidType
	}
	rows, err := s.repo.ListItems(ctx, typeCode, scope.TenantID, scope.Organization, options.IncludeDisabled)
	if err != nil {
		return nil, fmt.Errorf("%w: list items", ErrRepositoryMissing)
	}
	merged := mergeItems(rows, scope)
	version, err := s.repoVersion(ctx, scope, typeCode)
	if err != nil {
		return nil, err
	}
	locale := normalizeLocale(options.Locale)
	for i := range merged {
		merged[i].CacheVersion = version
		merged[i].Label = localizedLabel(merged[i], locale)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].SortOrder == merged[j].SortOrder {
			return merged[i].Value < merged[j].Value
		}
		return merged[i].SortOrder < merged[j].SortOrder
	})
	return merged, nil
}

func (s *Service) CreateItem(ctx context.Context, typeCode string, input ItemInput) (DictionaryItem, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return DictionaryItem{}, err
	}
	if s == nil || s.repo == nil {
		return DictionaryItem{}, ErrRepositoryMissing
	}
	typeCode = strings.TrimSpace(typeCode)
	if typeCode == "" {
		return DictionaryItem{}, ErrInvalidType
	}
	if _, err := s.repo.FindTypeByScope(ctx, typeCode, scope.TenantID, scope.Organization, false); err != nil {
		return DictionaryItem{}, err
	}
	if err := validateItemInput(input); err != nil {
		return DictionaryItem{}, err
	}
	now := s.now()
	row := DictionaryItem{ID: newID("dict-item"), TenantID: scope.TenantID, OrgID: scope.Organization, TypeCode: typeCode, Value: strings.TrimSpace(input.Value), LabelZhCN: strings.TrimSpace(input.LabelZhCN), LabelEnUS: strings.TrimSpace(input.LabelEnUS), Description: strings.TrimSpace(input.Description), Tag: strings.TrimSpace(input.Tag), Status: normalizedItemStatus(input), SortOrder: input.SortOrder, CreatedAt: now, UpdatedAt: now}
	created, err := s.repo.CreateItems(ctx, []DictionaryItem{row})
	if err != nil {
		return DictionaryItem{}, err
	}
	version, err := s.repo.BumpVersion(ctx, scope.TenantID, scope.Organization, typeCode, now)
	if err != nil {
		return DictionaryItem{}, err
	}
	created[0].CacheVersion = version
	created[0].Label = localizedLabel(created[0], "zh-CN")
	if err := s.record(ctx, AuditEvent{ActorID: actorFromContext(ctx), Action: "dictionary.item.create", Resource: "dictionary", TypeCode: typeCode, ItemID: row.ID, Version: version, CreatedAt: now}); err != nil {
		return DictionaryItem{}, err
	}
	return created[0], nil
}

func (s *Service) UpdateItem(ctx context.Context, typeCode, id string, input ItemInput) (DictionaryItem, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return DictionaryItem{}, err
	}
	if s == nil || s.repo == nil {
		return DictionaryItem{}, ErrRepositoryMissing
	}
	row, err := s.repo.FindItem(ctx, id, scope.TenantID, scope.Organization, strings.TrimSpace(typeCode), true)
	if err != nil {
		return DictionaryItem{}, err
	}
	if row.SystemOwned {
		return DictionaryItem{}, ErrSystemReadOnly
	}
	if err := validateItemInputForUpdate(input, row); err != nil {
		return DictionaryItem{}, err
	}
	row.Value = choose(input.Value, row.Value)
	row.LabelZhCN = choose(input.LabelZhCN, row.LabelZhCN)
	row.LabelEnUS = choose(input.LabelEnUS, row.LabelEnUS)
	row.Description = choose(input.Description, row.Description)
	row.Tag = choose(input.Tag, row.Tag)
	row.Status = choose(input.Status, row.Status)
	if input.SortOrder != 0 {
		row.SortOrder = input.SortOrder
	}
	if input.Enabled {
		row.Status = "active"
	}
	row.UpdatedAt = s.now()
	updated, err := s.repo.UpdateItem(ctx, row)
	if err != nil {
		return DictionaryItem{}, err
	}
	version, err := s.repo.BumpVersion(ctx, scope.TenantID, scope.Organization, row.TypeCode, row.UpdatedAt)
	if err != nil {
		return DictionaryItem{}, err
	}
	updated.CacheVersion, updated.Label = version, localizedLabel(updated, "zh-CN")
	if err := s.record(ctx, AuditEvent{ActorID: actorFromContext(ctx), Action: "dictionary.item.update", Resource: "dictionary", TypeCode: row.TypeCode, ItemID: row.ID, Version: version, CreatedAt: row.UpdatedAt}); err != nil {
		return DictionaryItem{}, err
	}
	return updated, nil
}

func (s *Service) DeleteItem(ctx context.Context, typeCode, id string) error {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	if s == nil || s.repo == nil {
		return ErrRepositoryMissing
	}
	row, err := s.repo.FindItem(ctx, id, scope.TenantID, scope.Organization, strings.TrimSpace(typeCode), true)
	if err != nil {
		return err
	}
	if row.SystemOwned {
		return ErrSystemReadOnly
	}
	now := s.now()
	if err := s.repo.SoftDeleteItem(ctx, row.ID, scope.TenantID, scope.Organization, now); err != nil {
		return err
	}
	version, err := s.repo.BumpVersion(ctx, scope.TenantID, scope.Organization, row.TypeCode, now)
	if err != nil {
		return err
	}
	return s.record(ctx, AuditEvent{ActorID: actorFromContext(ctx), Action: "dictionary.item.delete", Resource: "dictionary", TypeCode: row.TypeCode, ItemID: row.ID, Version: version, CreatedAt: now})
}

func (s *Service) ImportItems(ctx context.Context, typeCode string, inputs []ItemInput) ([]DictionaryItem, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if s == nil || s.repo == nil {
		return nil, ErrRepositoryMissing
	}
	typeCode = strings.TrimSpace(typeCode)
	if typeCode == "" {
		return nil, ErrInvalidType
	}
	if _, err := s.repo.FindTypeByScope(ctx, typeCode, scope.TenantID, scope.Organization, false); err != nil {
		return nil, err
	}
	if len(inputs) == 0 || len(inputs) > MaxImportItems {
		return nil, ErrImportLimit
	}
	rows := make([]DictionaryItem, 0, len(inputs))
	seen := map[string]struct{}{}
	now := s.now()
	for _, input := range inputs {
		if err := validateItemInput(input); err != nil {
			return nil, err
		}
		value := strings.TrimSpace(input.Value)
		if _, exists := seen[value]; exists {
			return nil, ErrItemConflict
		}
		seen[value] = struct{}{}
		rows = append(rows, DictionaryItem{ID: newID("dict-item"), TenantID: scope.TenantID, OrgID: scope.Organization, TypeCode: typeCode, Value: value, LabelZhCN: strings.TrimSpace(input.LabelZhCN), LabelEnUS: strings.TrimSpace(input.LabelEnUS), Description: strings.TrimSpace(input.Description), Tag: strings.TrimSpace(input.Tag), Status: normalizedItemStatus(input), SortOrder: input.SortOrder, CreatedAt: now, UpdatedAt: now})
	}
	created, err := s.repo.CreateItems(ctx, rows)
	if err != nil {
		return nil, err
	}
	version, err := s.repo.BumpVersion(ctx, scope.TenantID, scope.Organization, typeCode, now)
	if err != nil {
		return nil, err
	}
	for i := range created {
		created[i].CacheVersion, created[i].Label = version, localizedLabel(created[i], "zh-CN")
	}
	if err := s.record(ctx, AuditEvent{ActorID: actorFromContext(ctx), Action: "dictionary.item.import", Resource: "dictionary", TypeCode: typeCode, Version: version, CreatedAt: now}); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Service) repoVersion(ctx context.Context, scope tenant.Context, code string) (int64, error) {
	return s.repo.CurrentVersion(ctx, scope.TenantID, scope.Organization, code)
}

func (s *Service) record(ctx context.Context, event AuditEvent) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Record(ctx, event)
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock().UTC()
}

func mergeTypes(rows []DictionaryType, scope tenant.Context) []DictionaryType {
	chosen := map[string]DictionaryType{}
	priority := map[string]int{}
	for _, row := range rows {
		if row.DeletedAt != nil {
			continue
		}
		p := precedence(row.TenantID, row.OrgID, scope)
		if p < 0 || p <= priority[row.Code] {
			continue
		}
		chosen[row.Code], priority[row.Code] = row, p
	}
	result := make([]DictionaryType, 0, len(chosen))
	for _, row := range chosen {
		result = append(result, row)
	}
	return result
}

func mergeItems(rows []DictionaryItem, scope tenant.Context) []DictionaryItem {
	chosen := map[string]DictionaryItem{}
	priority := map[string]int{}
	for _, row := range rows {
		if row.DeletedAt != nil {
			continue
		}
		p := precedence(row.TenantID, row.OrgID, scope)
		if p < 0 || p <= priority[row.Value] {
			continue
		}
		chosen[row.Value], priority[row.Value] = row, p
	}
	result := make([]DictionaryItem, 0, len(chosen))
	for _, row := range chosen {
		result = append(result, row)
	}
	return result
}

func precedence(tenantID, orgID string, scope tenant.Context) int {
	if tenantID == "" && orgID == "" {
		return 1
	}
	if tenantID != scope.TenantID {
		return -1
	}
	if orgID == scope.Organization && orgID != "" {
		return 3
	}
	if orgID == "" {
		return 2
	}
	return -1
}

func localizedLabel(item DictionaryItem, locale string) string {
	if normalizeLocale(locale) == "en-US" {
		if strings.TrimSpace(item.LabelEnUS) != "" {
			return item.LabelEnUS
		}
	}
	if strings.TrimSpace(item.LabelZhCN) != "" {
		return item.LabelZhCN
	}
	return item.LabelEnUS
}

func normalizeLocale(locale string) string {
	locale = strings.TrimSpace(strings.ReplaceAll(locale, "_", "-"))
	if strings.EqualFold(locale, "en") || strings.EqualFold(locale, "en-US") {
		return "en-US"
	}
	return "zh-CN"
}

func normalizeTypeInput(input TypeInput, create bool) (TypeInput, error) {
	input.Code, input.NameZhCN, input.NameEnUS = strings.TrimSpace(input.Code), strings.TrimSpace(input.NameZhCN), strings.TrimSpace(input.NameEnUS)
	if input.Code == "" || len(input.Code) > 128 || (create && input.NameZhCN == "" && input.NameEnUS == "") {
		return TypeInput{}, ErrInvalidType
	}
	if input.Status == "" {
		input.Status = "active"
	}
	if input.Status != "active" && input.Status != "disabled" {
		return TypeInput{}, ErrInvalidType
	}
	if input.SortOrder < 0 {
		return TypeInput{}, ErrInvalidType
	}
	return input, nil
}

func normalizeTypeInputForUpdate(input TypeInput, current DictionaryType) (TypeInput, error) {
	input.Code = current.Code
	if input.NameZhCN == "" {
		input.NameZhCN = current.NameZhCN
	}
	if input.NameEnUS == "" {
		input.NameEnUS = current.NameEnUS
	}
	if input.Description == "" {
		input.Description = current.Description
	}
	if input.Status == "" {
		input.Status = current.Status
	}
	if input.SortOrder == 0 {
		input.SortOrder = current.SortOrder
	}
	return normalizeTypeInput(input, false)
}

func validateItemInput(input ItemInput) error {
	if strings.TrimSpace(input.Value) == "" || len(strings.TrimSpace(input.Value)) > 191 || strings.TrimSpace(input.LabelZhCN) == "" && strings.TrimSpace(input.LabelEnUS) == "" {
		return ErrInvalidItem
	}
	if input.Status != "" && input.Status != "active" && input.Status != "disabled" {
		return ErrInvalidItem
	}
	if input.SortOrder < 0 {
		return ErrInvalidItem
	}
	return nil
}

func validateItemInputForUpdate(input ItemInput, current DictionaryItem) error {
	if input.Value == "" {
		input.Value = current.Value
	}
	if input.LabelZhCN == "" {
		input.LabelZhCN = current.LabelZhCN
	}
	if input.LabelEnUS == "" {
		input.LabelEnUS = current.LabelEnUS
	}
	return validateItemInput(input)
}

func normalizedStatus(status string) string {
	if status == "disabled" {
		return status
	}
	return "active"
}
func normalizedItemStatus(input ItemInput) string {
	if input.Status == "disabled" || !input.Enabled && input.Status == "disabled" {
		return "disabled"
	}
	return "active"
}
func choose(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

var idCounter atomic.Uint64

func newID(prefix string) string { return fmt.Sprintf("%s-%d", prefix, idCounter.Add(1)) }

// NewVersionID supplies durable adapters with a collision-resistant opaque
// identifier while keeping ID formatting out of SQL adapters.
func NewVersionID() string { return newID("version") }
