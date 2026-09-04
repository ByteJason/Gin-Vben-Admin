package settings

// This file contains the module-level settings contract.  The original
// settings API addressed one key at a time and exposed its storage history.
// Modules are the public contract now: a complete candidate is validated,
// persisted and applied as one unit.  The old key API remains in service.go as
// a compatibility adapter for older integrations, but new callers should use
// the types and methods below.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	domainobs "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/observability"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

// Scope controls the level at which a value may be overridden.
type Scope string

const (
	ScopeSystem       Scope = "system"
	ScopeTenant       Scope = "tenant"
	ScopeOrganization Scope = "organization"
	// ScopeOrg is a concise compatibility alias used by a few adapters.
	ScopeOrg = ScopeOrganization
)

// ApplyMode describes how a committed candidate becomes effective.  It is
// deliberately richer than the former RestartRequired boolean.
type ApplyMode string

const (
	ApplyImmediate       ApplyMode = "immediate"
	ApplyComponentReload ApplyMode = "component_reload"
	ApplyRestart         ApplyMode = "restart"
	ApplyDeployment      ApplyMode = "deployment"
	ApplyMigration       ApplyMode = "migration"
	// Common aliases make the contract pleasant to consume from Go callers.
	ApplyReload         = ApplyComponentReload
	ApplyAfterReload    = ApplyComponentReload
	ApplyAfterRestart   = ApplyRestart
	ApplyDeploymentOnly = ApplyDeployment
	ApplyMigrationOnly  = ApplyMigration
)

// ModuleStatus separates persistence from runtime application.  A saved
// candidate is never reported as a database failure merely because a live
// component could not be rebuilt.
type ModuleStatus string

const (
	StatusSavedAndApplied       ModuleStatus = "saved_and_applied"
	StatusSavedPendingReload    ModuleStatus = "saved_pending_reload"
	StatusSavedPendingRestart   ModuleStatus = "saved_pending_restart"
	StatusSavedPendingMigration ModuleStatus = "saved_pending_migration"
	StatusSavedApplyFailed      ModuleStatus = "saved_apply_failed"
	StatusSaveFailed            ModuleStatus = "save_failed"
	StatusUnchanged             ModuleStatus = "unchanged"
	// Human-readable aliases retained for clients that use the shorter names.
	StatusApplied        = StatusSavedAndApplied
	StatusPendingReload  = StatusSavedPendingReload
	StatusPendingRestart = StatusSavedPendingRestart
	StatusApplyFailed    = StatusSavedApplyFailed
)

// ModuleDefinition describes one atomic edit boundary.
type ModuleDefinition struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName"`
	Description string    `json:"description,omitempty"`
	Category    Category  `json:"category"`
	Group       string    `json:"group"`
	Keys        []string  `json:"keys"`
	ApplyMode   ApplyMode `json:"applyMode"`
	Scope       Scope     `json:"scope"`
	ScopePolicy []Scope   `json:"scopePolicy,omitempty"`
	Editable    bool      `json:"editable"`
}

// ModuleView is the read model consumed by the settings page. Values are
// already masked according to their definitions.
type ModuleView struct {
	ID                string       `json:"id"`
	Module            string       `json:"module"`
	Name              string       `json:"name"`
	DisplayName       string       `json:"displayName"`
	Description       string       `json:"description,omitempty"`
	Category          Category     `json:"category"`
	Group             string       `json:"group"`
	Definitions       []Definition `json:"definitions"`
	Settings          []Setting    `json:"settings"`
	Revision          int64        `json:"revision"`
	Status            ModuleStatus `json:"status"`
	ApplyMode         ApplyMode    `json:"applyMode"`
	ScopePolicy       []Scope      `json:"scopePolicy,omitempty"`
	Source            Source       `json:"source,omitempty"`
	UpdatedAt         time.Time    `json:"updatedAt,omitempty"`
	UpdatedBy         string       `json:"updatedBy,omitempty"`
	ApplyError        string       `json:"applyError,omitempty"`
	OtherNodesPending bool         `json:"otherNodesPending"`
	RequiresRestart   bool         `json:"requiresRestart"`
}

// ModuleUpdateInput is intentionally map-shaped so a module can be submitted
// in one request without exposing storage implementation details.
type ModuleUpdateInput struct {
	Module           string                     `json:"module"`
	Values           map[string]json.RawMessage `json:"values"`
	ExpectedRevision int64                      `json:"expectedRevision"`
	RequestID        string                     `json:"requestId,omitempty"`
	ResetKeys        []string                   `json:"resetKeys,omitempty"`
}

// SaveModuleInput and UpdateModuleInput are naming aliases for adapters.
type SaveModuleInput = ModuleUpdateInput

// ModuleSaveResult reports independent database/runtime outcomes.
type ModuleSaveResult struct {
	Module            string       `json:"module"`
	ID                string       `json:"id"`
	Revision          int64        `json:"revision"`
	PreviousRevision  int64        `json:"previousRevision"`
	ChangedKeys       []string     `json:"changedKeys"`
	Settings          []Setting    `json:"settings"`
	Persisted         bool         `json:"persisted"`
	Applied           bool         `json:"applied"`
	Status            ModuleStatus `json:"status"`
	ApplyMode         ApplyMode    `json:"applyMode"`
	ApplyError        string       `json:"applyError,omitempty"`
	AuditRecorded     bool         `json:"auditRecorded"`
	CacheSynced       bool         `json:"cacheSynced"`
	OtherNodesPending bool         `json:"otherNodesPending"`
	RequiresRestart   bool         `json:"requiresRestart"`
	RequestID         string       `json:"requestId,omitempty"`
	UpdatedAt         time.Time    `json:"updatedAt"`
}

// ModuleValidationResult is returned by ValidateModule without persistence.
type ModuleValidationResult struct {
	Module    string                     `json:"module"`
	Valid     bool                       `json:"valid"`
	ApplyMode ApplyMode                  `json:"applyMode"`
	Values    map[string]json.RawMessage `json:"values"`
	Errors    map[string]string          `json:"errors,omitempty"`
	CheckedAt time.Time                  `json:"checkedAt"`
}

// StoredModule is the repository-neutral aggregate used for atomic saves.
type StoredModule struct {
	Module    string
	Values    map[string]StoredSetting
	Revision  int64
	UpdatedAt time.Time
}

// AtomicModuleRepository is optional so existing repositories can be upgraded
// incrementally. Implementations must perform the complete write under one
// transaction and compare expectedRevision while holding the transaction
// lock.
type AtomicModuleRepository interface {
	CurrentModule(context.Context, string) (StoredModule, error)
	SaveModule(context.Context, string, map[string]StoredSetting, int64) (StoredModule, error)
}

// CurrentSettingsRepository supplies the complete current set for bootstrap
// snapshot reconstruction. It intentionally has no history operation.
type CurrentSettingsRepository interface {
	ListCurrent(context.Context) ([]StoredSetting, error)
}

// SettingDeleter removes a database override so reset-to-default really
// inherits the next source; it must not write the default back to the DB.
type SettingDeleter interface {
	Delete(context.Context, string) error
}

// AtomicModuleResetRepository removes every database override in a module and
// advances its revision under one transaction. Implementations should prefer
// this seam over a loop of per-key deletes so concurrent readers never observe
// a partially reset module.
type AtomicModuleResetRepository interface {
	ResetModule(context.Context, string, int64) (StoredModule, error)
}

// AtomicCredentialClearRepository removes a selected set of sensitive
// database overrides in one module transaction. It is deliberately separate
// from ResetModule: restoring a module removes every override, whereas
// clearing credentials must be an explicit, narrow operation that leaves
// unrelated settings untouched.
type AtomicCredentialClearRepository interface {
	ClearSensitiveKeys(context.Context, string, []string, int64) (StoredModule, error)
}

// RuntimeApplier rebuilds a component or applies a runtime policy. It is
// called only after the database transaction commits. On error the previous
// runtime snapshot remains active.
type RuntimeApplier interface {
	Apply(context.Context, string, map[string]json.RawMessage) error
}

// ModuleCache coordinates optional Redis cache/revision propagation. Cache
// failures are advisory: the local snapshot remains authoritative for active
// requests and the next reconciliation can retry.
type ModuleCache interface {
	InvalidateModule(context.Context, string, int64) error
}

// RuntimeModuleProvider supplies the read-only runtime environment module.
// It is intentionally narrower than monitor's full metrics DTO: providers
// return only the bounded, non-secret values declared by the runtime
// definitions. The settings service overlays provider values on safe schema
// defaults and never persists them.
type RuntimeModuleProvider interface {
	LoadRuntimeModule(context.Context) (map[string]StoredSetting, error)
}

// RuntimeModuleProviderFunc adapts a function to RuntimeModuleProvider. It is
// useful for the bootstrap composition root and deterministic tests.
type RuntimeModuleProviderFunc func(context.Context) (map[string]StoredSetting, error)

func (f RuntimeModuleProviderFunc) LoadRuntimeModule(ctx context.Context) (map[string]StoredSetting, error) {
	if f == nil {
		return nil, nil
	}
	return f(ctx)
}

// RuntimeProvider is a concise compatibility alias for adapters that use the
// shorter name.
type RuntimeProvider = RuntimeModuleProvider

func (c Category) String() string { return string(c) }

var moduleOrder = []string{"basic", "security", "file", "captcha", "i18n", "observability", "runtime", "other"}

// moduleForDefinition is kept deterministic and treats observability as its
// own business module rather than leaking the internal `other` category.
func moduleForDefinition(definition Definition) string {
	if group := strings.TrimSpace(definition.Group); group != "" && group != string(CategoryOther) {
		return strings.ToLower(group)
	}
	if strings.HasPrefix(definition.Key, "observability.") {
		return "observability"
	}
	if definition.Category != "" {
		return strings.ToLower(string(definition.Category))
	}
	return "other"
}

func isRetiredSettingKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, prefix := range []string{"mail.", "email.", "smtp."} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return key == "mail" || key == "email" || key == "smtp"
}

// IsRetiredSettingKey lets transports reject settings that moved to an
// independent capability (currently the mail transport) without importing
// or duplicating the legacy definition table.
func IsRetiredSettingKey(key string) bool { return isRetiredSettingKey(key) }

// activeDefinition reports whether a definition belongs to the new settings
// center. Legacy mail definitions can remain in a compatibility map for old
// persisted rows, but are never exposed or accepted by module writes.
func activeDefinition(definition Definition) bool {
	return definition.Key != "" && !definition.Deprecated && !isRetiredSettingKey(definition.Key) && !isInfrastructureSetting(definition.Key)
}

func (s *Service) activeDefinitions() map[string]Definition {
	result := make(map[string]Definition)
	if s == nil {
		return result
	}
	for key, definition := range s.definitions {
		if definition.Key == "" {
			definition.Key = key
		}
		if activeDefinition(definition) {
			result[key] = definition

		}
	}
	return result
}

func moduleName(module string) string { return strings.ToLower(strings.TrimSpace(module)) }

// runtimeSnapshotScope derives the immutable snapshot partition from the
// validated request context. The platform-admin bit is intentionally excluded:
// an administrator still operates on the selected tenant/organization scope,
// and must never read a process-global or another tenant's snapshot.
func runtimeSnapshotScope(ctx context.Context) string {
	scope, ok := tenant.FromContext(ctx)
	if !ok {
		return ""
	}
	return strings.TrimSpace(scope.TenantID) + "\x00" + strings.TrimSpace(scope.Organization)
}

func (s *Service) setModuleState(module string, status ModuleStatus) {
	if s == nil || module == "" {
		return
	}
	s.moduleStateMu.Lock()
	if s.moduleStates == nil {
		s.moduleStates = map[string]ModuleStatus{}
	}
	s.moduleStates[module] = status
	s.moduleStateMu.Unlock()
}

func (s *Service) moduleState(module string) ModuleStatus {
	if s == nil {
		return ""
	}
	s.moduleStateMu.RLock()
	status := s.moduleStates[module]
	s.moduleStateMu.RUnlock()
	return status
}

// moduleOutcome carries the non-persistent part of the last application
// attempt. It is deliberately process-local: a runtime error is diagnostic
// state, not a second configuration source, and a restart reconstructs the
// durable pending state from the committed records.
type moduleOutcome struct {
	ApplyError        string
	OtherNodesPending bool
}

func (s *Service) setModuleOutcome(module string, outcome moduleOutcome) {
	if s == nil || module == "" {
		return
	}
	s.moduleStateMu.Lock()
	if s.moduleOutcomes == nil {
		s.moduleOutcomes = map[string]moduleOutcome{}
	}
	s.moduleOutcomes[module] = outcome
	s.moduleStateMu.Unlock()
}

func (s *Service) moduleOutcome(module string) moduleOutcome {
	if s == nil {
		return moduleOutcome{}
	}
	s.moduleStateMu.RLock()
	outcome := s.moduleOutcomes[module]
	s.moduleStateMu.RUnlock()
	return outcome
}

// inferredModuleStatus supplies a useful status after process restart, when
// the in-memory last-apply result is gone. It only infers a pending state for
// modules that have a persisted database override; untouched defaults are
// already effective and remain saved_and_applied.
func inferredModuleStatus(module string, definition ModuleDefinition, records map[string]StoredSetting, applier RuntimeApplier) ModuleStatus {
	return inferredModuleStatusForDefinitions(module, definition, nil, records, applier)
}

// inferredModuleStatusForDefinitions reconstructs the pending state after a
// process restart from the keys that actually have database overrides. A
// module's aggregate ApplyMode is the strongest capability of the whole form,
// but it must not make an override for an immediate/component-reload key look
// like a migration merely because an unrelated key in the same module would
// require migration. The legacy wrapper above remains for callers that only
// have the aggregate module definition.
func inferredModuleStatusForDefinitions(module string, moduleDefinition ModuleDefinition, definitions map[string]Definition, records map[string]StoredSetting, applier RuntimeApplier) ModuleStatus {
	if module == "runtime" {
		return StatusSavedAndApplied
	}
	hasDatabaseValue := false
	mode := moduleDefinition.ApplyMode
	if len(definitions) > 0 {
		mode = ApplyImmediate
	}
	for _, record := range records {
		if record.Source == SourceDatabase {
			hasDatabaseValue = true
			if len(definitions) > 0 {
				if definition, ok := definitions[record.Key]; ok {
					candidate := definition.ApplyMode
					if candidate == "" {
						candidate = applyModeForDefinition(definition)
					}
					if applyModeRank(candidate) > applyModeRank(mode) {
						mode = candidate
					}
				}
			}
		}
	}
	if !hasDatabaseValue {
		return StatusSavedAndApplied
	}
	switch mode {
	case ApplyMigration:
		return StatusSavedPendingMigration
	case ApplyRestart, ApplyDeployment:
		return StatusSavedPendingRestart
	case ApplyComponentReload:
		if applier == nil {
			return StatusSavedPendingReload
		}
	}
	return StatusSavedAndApplied
}

func (s *Service) definitionsForModule(module string) map[string]Definition {
	module = moduleName(module)
	result := make(map[string]Definition)
	for key, definition := range s.activeDefinitions() {
		if moduleForDefinition(definition) == module {
			result[key] = definition
		}
	}
	return result
}

func moduleDisplayName(module string) string {
	return map[string]string{
		"basic": "基础设置", "security": "安全设置", "file": "文件与存储",
		"captcha": "验证码", "i18n": "语言与区域", "observability": "可观测性", "runtime": "运行环境",
		"other": "其他设置",
	}[module]
}

func moduleDescription(module string) string {
	return map[string]string{
		"basic": "站点名称和品牌展示", "security": "登录令牌与浏览器安全策略",
		"file": "上传规则和对象存储策略", "captcha": "验证码风险与挑战策略",
		"i18n": "默认语言和可用语言", "observability": "指标与链路追踪运行策略",
		"runtime": "服务版本、依赖状态和当前运行节点",
		"other":   "其他运行时设置",
	}[module]
}

func applyModeRank(mode ApplyMode) int {
	switch mode {
	case ApplyMigration:
		return 5
	case ApplyDeployment:
		return 4
	case ApplyRestart:
		return 3
	case ApplyComponentReload:
		return 2
	default:
		return 1
	}
}

func mergedApplyMode(definitions map[string]Definition) ApplyMode {
	mode := ApplyImmediate
	for _, definition := range definitions {
		candidate := definition.ApplyMode
		if candidate == "" {
			candidate = applyModeForDefinition(definition)
		}
		if applyModeRank(candidate) > applyModeRank(mode) {
			mode = candidate
		}
	}
	return mode
}

// mergedApplyModeForKeys computes the strongest apply requirement for the
// fields actually being changed. A module can contain both migration-only
// fields (for example a storage-driver switch) and component-reload fields
// (for example a credential); touching the latter must not incorrectly mark
// the whole save as a migration when the former is unchanged.
func mergedApplyModeForKeys(definitions map[string]Definition, keys []string) ApplyMode {
	if len(keys) == 0 {
		return mergedApplyMode(definitions)
	}
	subset := make(map[string]Definition, len(keys))
	for _, key := range keys {
		if definition, ok := definitions[key]; ok {
			subset[key] = definition
		}
	}
	if len(subset) == 0 {
		return mergedApplyMode(definitions)
	}
	return mergedApplyMode(subset)
}

func moduleDefinition(module string, definitions map[string]Definition) ModuleDefinition {
	keys := make([]string, 0, len(definitions))
	for key := range definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var category Category
	var scope Scope
	scopeSet := map[Scope]struct{}{}
	editable := false
	for _, key := range keys {
		definition := definitions[key]
		if category == "" {
			category = definition.Category
		}
		if scope == "" {
			scope = definition.Scope
		}
		if definition.Scope != "" {
			scopeSet[definition.Scope] = struct{}{}
		}
		if definition.Editable {
			editable = true
		}
	}
	if scope == "" {
		scope = ScopeTenant
	}
	policies := make([]Scope, 0, len(scopeSet))
	for candidate := range scopeSet {
		policies = append(policies, candidate)
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i] < policies[j] })
	name := moduleDisplayName(module)
	return ModuleDefinition{ID: module, Name: name, DisplayName: name, Description: moduleDescription(module), Category: category, Group: module, Keys: keys, ApplyMode: mergedApplyMode(definitions), Scope: scope, ScopePolicy: policies, Editable: editable}
}

// Modules lists active module definitions in stable product order.
func (s *Service) Modules(ctx context.Context, actor Actor) ([]ModuleDefinition, error) {
	if err := s.authorize(ctx, actor, "*", "read"); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, definition := range s.activeDefinitions() {
		seen[moduleForDefinition(definition)] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		position := func(value string) int {
			for index, item := range moduleOrder {
				if item == value {
					return index
				}
			}
			return len(moduleOrder)
		}
		pi, pj := position(keys[i]), position(keys[j])
		if pi != pj {
			return pi < pj
		}
		return keys[i] < keys[j]
	})
	result := make([]ModuleDefinition, 0, len(keys))
	for _, module := range keys {
		result = append(result, moduleDefinition(module, s.definitionsForModule(module)))
	}
	return result, nil
}

// ListModules is a semantic alias used by HTTP adapters.
func (s *Service) ListModules(ctx context.Context, actor Actor) ([]ModuleDefinition, error) {
	return s.Modules(ctx, actor)
}

func (s *Service) moduleRevision(ctx context.Context, module string, definitions map[string]Definition) (int64, error) {
	if repository, ok := s.repo.(interface {
		CurrentModule(context.Context, string) (StoredModule, error)
	}); ok {
		stored, err := repository.CurrentModule(ctx, module)
		if errors.Is(err, ErrSettingNotFound) || errors.Is(err, ErrModuleNotFound) {
			// A reset may leave only an aggregate tombstone. Preserve its
			// monotonic revision even though there is no active value row.
			return stored.Revision, nil
		}
		if err != nil {
			return 0, err
		}
		return stored.Revision, nil
	}
	var revision int64
	for key := range definitions {
		if record, err := s.repo.Current(ctx, key); err == nil && record.Version > revision {
			revision = record.Version
		} else if err != nil && !errors.Is(err, ErrSettingNotFound) {
			return 0, err
		}
	}
	return revision, nil
}

func (s *Service) effectiveModuleValues(ctx context.Context, module string, definitions map[string]Definition) (map[string]json.RawMessage, map[string]StoredSetting, error) {
	values := make(map[string]json.RawMessage, len(definitions))
	records := make(map[string]StoredSetting, len(definitions))
	for key, definition := range definitions {
		record, err := s.resolve(ctx, key, definition)
		if err != nil {
			return nil, nil, err
		}
		raw, err := s.readPayload(ctx, key, record)
		if err != nil {
			return nil, nil, err
		}
		if !json.Valid(raw) {
			return nil, nil, fmt.Errorf("%w: %s", ErrInvalidSetting, key)
		}
		values[key] = append(json.RawMessage(nil), raw...)
		record.RawValue = append([]byte(nil), raw...)
		records[key] = record
	}
	// Runtime values are observations, not database overrides. Overlaying them
	// after normal resolution keeps the same schema/read model while ensuring a
	// transient probe failure can fall back to deterministic safe defaults.
	if module == "runtime" && s.runtimeProvider != nil {
		provided, _ := s.runtimeProvider.LoadRuntimeModule(ctx)
		for key, record := range provided {
			definition, ok := definitions[key]
			if !ok || definition.Sensitive || len(record.RawValue) == 0 || !json.Valid(record.RawValue) {
				continue
			}
			if record.Key == "" {
				record.Key = key
			}
			if record.Source == "" {
				record.Source = SourceDefault
			}
			values[key] = append(json.RawMessage(nil), record.RawValue...)
			record.RawValue = append([]byte(nil), record.RawValue...)
			records[key] = record
		}
	}
	return values, records, nil
}

// GetModule returns a complete effective module assembled from the process
// snapshot/repository/defaults. It never returns mail definitions.
func (s *Service) GetModule(ctx context.Context, actor Actor, module string) (ModuleView, error) {
	module = moduleName(module)
	if err := s.authorize(ctx, actor, module, "read"); err != nil {
		return ModuleView{}, err
	}
	definitions := s.definitionsForModule(module)
	if len(definitions) == 0 {
		return ModuleView{}, ErrModuleNotFound
	}
	values, records, err := s.effectiveModuleValues(ctx, module, definitions)
	if err != nil {
		return ModuleView{}, err
	}
	revision, err := s.moduleRevision(ctx, module, definitions)
	if err != nil {
		return ModuleView{}, err
	}
	definition := moduleDefinition(module, definitions)
	status := s.moduleState(module)
	if status == "" {
		status = inferredModuleStatusForDefinitions(module, definition, definitions, records, s.applier)
	}
	if module == "runtime" {
		// The runtime module reports the highest committed revision across
		// mutable modules, not its own (always zero) diagnostic revision. A
		// provider may supply a fresher distributed revision; preserve it when
		// present and otherwise derive the value from the authoritative repository.
		if raw, ok := values["runtime.config.revision"]; !ok || string(raw) == `0` {
			configRevision := s.activeConfigRevision(ctx)
			values["runtime.config.revision"] = json.RawMessage(strconv.FormatInt(configRevision, 10))
			source := SourceDefault
			if configRevision > 0 {
				source = SourceDatabase
			}
			records["runtime.config.revision"] = StoredSetting{Key: "runtime.config.revision", RawValue: values["runtime.config.revision"], Source: source}
		}
		pending := s.hasPendingRestart(ctx)
		pendingRaw, _ := json.Marshal(pending)
		values["runtime.pending_restart"] = pendingRaw
		pendingSource := SourceDefault
		if pending {
			pendingSource = SourceDatabase
		}
		records["runtime.pending_restart"] = StoredSetting{Key: "runtime.pending_restart", RawValue: pendingRaw, Source: pendingSource}
		if pending && status == StatusSavedAndApplied {
			status = StatusSavedPendingRestart
		}
	}
	keys := make([]string, 0, len(definitions))
	for key := range definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	settings := make([]Setting, 0, len(keys))
	for _, key := range keys {
		record := records[key]
		record.RawValue = values[key]
		settings = append(settings, s.present(record, definitions[key]))
	}
	outcome := s.moduleOutcome(module)
	// The apply mode describes capability; RequiresRestart reports the current
	// state. A fresh read-only runtime module may contain deployment metadata but
	// must not claim that this process is waiting for a restart until a pending
	// change actually exists.
	requiresRestart := status == StatusSavedPendingRestart || status == StatusSavedPendingMigration
	return ModuleView{ID: module, Module: module, Name: definition.Name, DisplayName: definition.DisplayName, Description: definition.Description, Category: definition.Category, Group: module, Definitions: orderedDefinitions(definitions), Settings: settings, Revision: revision, Status: status, ApplyMode: definition.ApplyMode, ScopePolicy: append([]Scope(nil), definition.ScopePolicy...), Source: commonSource(records), UpdatedAt: latestTime(records), UpdatedBy: latestUpdatedBy(records), ApplyError: outcome.ApplyError, OtherNodesPending: outcome.OtherNodesPending, RequiresRestart: requiresRestart}, nil
}

// hasPendingRestart computes the process-wide restart/migration indicator used
// by the read-only runtime module. It consults in-memory outcomes first and
// only falls back to the durable records after a process restart; a transient
// repository read error leaves the last known indicator unchanged.
func (s *Service) hasPendingRestart(ctx context.Context) bool {
	if s == nil {
		return false
	}
	for _, module := range moduleOrder {
		if module == "runtime" {
			continue
		}
		definitions := s.definitionsForModule(module)
		if len(definitions) == 0 {
			continue
		}
		status := s.moduleState(module)
		if status == StatusSavedPendingRestart || status == StatusSavedPendingMigration {
			return true
		}
		if status != "" {
			continue
		}
		values, records, err := s.effectiveModuleValues(ctx, module, definitions)
		if err != nil {
			continue
		}
		_ = values
		inferred := inferredModuleStatusForDefinitions(module, moduleDefinition(module, definitions), definitions, records, s.applier)
		if inferred == StatusSavedPendingRestart || inferred == StatusSavedPendingMigration {
			return true
		}
	}
	return false
}

// activeConfigRevision returns the latest committed revision among mutable
// modules. Repository errors are ignored here because runtime diagnostics must
// remain renderable during a dependency outage; the provider/default values
// still communicate the degraded state.
func (s *Service) activeConfigRevision(ctx context.Context) int64 {
	if s == nil {
		return 0
	}
	var revision int64
	for _, module := range moduleOrder {
		if module == "runtime" {
			continue
		}
		definitions := s.definitionsForModule(module)
		if len(definitions) == 0 {
			continue
		}
		candidate, err := s.moduleRevision(ctx, module, definitions)
		if err == nil && candidate > revision {
			revision = candidate
		}
	}
	return revision
}

func orderedDefinitions(definitions map[string]Definition) []Definition {
	keys := make([]string, 0, len(definitions))
	for key := range definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]Definition, 0, len(keys))
	for _, key := range keys {
		definition := definitions[key]
		if definition.Sensitive {
			definition.Default = maskedValue
		}
		definition.Allowed = append([]string(nil), definition.Allowed...)
		definition.SourcePolicy = append([]Source(nil), definition.SourcePolicy...)
		result = append(result, definition)
	}
	return result
}

func commonSource(records map[string]StoredSetting) Source {
	if len(records) == 0 {
		return SourceDefault
	}
	// A mixed module reports database only when every value is database-backed;
	// otherwise the source is intentionally left blank to avoid a false claim.
	var source Source
	for _, record := range records {
		candidate := record.Source
		if candidate == "" {
			candidate = SourceDatabase
		}
		if source == "" {
			source = candidate
		} else if source != candidate {
			return ""
		}
	}
	return source
}

func latestTime(records map[string]StoredSetting) time.Time {
	var latest time.Time
	for _, record := range records {
		if record.UpdatedAt.After(latest) {
			latest = record.UpdatedAt
		}
	}
	return latest
}

func latestUpdatedBy(records map[string]StoredSetting) string {
	var latest time.Time
	var actor string
	for _, record := range records {
		if record.UpdatedAt.After(latest) {
			latest = record.UpdatedAt
			actor = record.UpdatedBy
		}
	}
	return actor
}

// validateModuleRequest centralizes transport-independent bounds checks. HTTP
// handlers perform the same checks for early feedback, but callers using the
// application service directly must receive identical behavior.
func validateModuleRequest(input ModuleUpdateInput, requireRevision bool) error {
	if strings.TrimSpace(input.Module) == "" {
		return fmt.Errorf("%w: module is required", ErrInvalidSetting)
	}
	if requireRevision && input.ExpectedRevision < 0 {
		return fmt.Errorf("%w: expectedRevision must be non-negative", ErrInvalidSetting)
	}
	if len(input.RequestID) > maxRequestIDLength {
		return fmt.Errorf("%w: requestId is too long", ErrInvalidSetting)
	}
	return nil
}

// ValidateModule validates a full candidate without writing or applying it.
func (s *Service) ValidateModule(ctx context.Context, actor Actor, input ModuleUpdateInput) (ModuleValidationResult, error) {
	module := moduleName(input.Module)
	if err := validateModuleRequest(input, false); err != nil {
		return ModuleValidationResult{Module: module, Valid: false, CheckedAt: time.Now().UTC()}, err
	}
	if err := s.authorize(ctx, actor, module, "validate"); err != nil {
		return ModuleValidationResult{}, err
	}
	definitions := s.definitionsForModule(module)
	if len(definitions) == 0 {
		return ModuleValidationResult{}, ErrModuleNotFound
	}
	if len(input.ResetKeys) > 0 {
		return ModuleValidationResult{}, fmt.Errorf("%w: resetKeys is only supported by the module reset operation", ErrInvalidSetting)
	}
	if module == "runtime" && len(input.Values) > 0 {
		// Runtime is an observation surface. Treat attempted edits as a
		// permission error rather than allowing a no-op validation to imply that
		// deployment-owned values can be changed from the UI.
		return ModuleValidationResult{Module: module, Valid: false, ApplyMode: mergedApplyMode(definitions), CheckedAt: time.Now().UTC()}, ErrPermissionDenied
	}
	values, records, err := s.effectiveModuleValues(ctx, module, definitions)
	if err != nil {
		return ModuleValidationResult{}, err
	}
	for key, raw := range input.Values {
		definition, ok := definitions[key]
		if !ok {
			return ModuleValidationResult{}, fmt.Errorf("%w: unknown module key %q", ErrInvalidSetting, key)
		}
		if sensitiveInputIsNoop(definition, raw) {
			// Do not let an omitted/blank credential trip deployment-source or
			// editability checks: it is explicitly a no-op.
			continue
		}
		if err := s.ensureDatabaseWritable(ctx, key, definition); err != nil {
			return ModuleValidationResult{}, err
		}
		if records[key].Source != "" && records[key].Source != SourceDatabase && records[key].Source != SourceDefault {
			return ModuleValidationResult{}, fmt.Errorf("%w: %s", ErrSettingLocked, key)
		}
		values[key] = append(json.RawMessage(nil), raw...)
		if err := validateValue(definition, raw); err != nil {
			return ModuleValidationResult{}, err
		}
	}
	if err := validateModuleValues(module, definitions, values); err != nil {
		return ModuleValidationResult{Module: module, Valid: false, Values: redactModuleValues(values, definitions), Errors: map[string]string{"module": err.Error()}, CheckedAt: time.Now().UTC()}, err
	}
	return ModuleValidationResult{Module: module, Valid: true, Values: redactModuleValues(values, definitions), ApplyMode: mergedApplyMode(definitions), CheckedAt: time.Now().UTC()}, nil
}

// redactModuleValues keeps the validation response useful for ordinary fields
// while ensuring secrets never cross the HTTP boundary. A masked JSON string
// mirrors Definition.Default/Setting.Value and is intentionally not accepted
// as an update payload without an explicit replacement from the caller.
func redactModuleValues(values map[string]json.RawMessage, definitions map[string]Definition) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(values))
	for key, raw := range values {
		if definition, ok := definitions[key]; ok && definition.Sensitive {
			result[key] = json.RawMessage(`"` + maskedValue + `"`)
			continue
		}
		result[key] = append(json.RawMessage(nil), raw...)
	}
	return result
}

func isMaskedSecret(raw []byte) bool { return string(raw) == `"`+maskedValue+`"` }

// isBlankSensitiveValue implements the API contract that an empty secret
// field means "leave the existing credential unchanged". It deliberately
// applies only to sensitive definitions; an empty ordinary string remains a
// real candidate and is validated/persisted normally. Whitespace around a
// JSON string is ignored, while malformed non-empty JSON still reaches the
// normal validator and is rejected.
func isBlankSensitiveValue(raw []byte) bool {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return true
	}
	var value string
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return false
	}
	return strings.TrimSpace(value) == ""
}

func sensitiveInputIsNoop(definition Definition, raw []byte) bool {
	return definition.Sensitive && (isMaskedSecret(raw) || isBlankSensitiveValue(raw))
}

func canonicalJSON(raw []byte) []byte {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return append([]byte(nil), raw...)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return append([]byte(nil), raw...)
	}
	return encoded
}

func sameJSON(a, b []byte) bool {
	return string(canonicalJSON(a)) == string(canonicalJSON(b))
}

// SaveModule performs the complete module save/apply lifecycle. Database
// success is returned in the result even when a runtime applier fails.
func (s *Service) SaveModule(ctx context.Context, actor Actor, input ModuleUpdateInput) (ModuleSaveResult, error) {
	module := moduleName(input.Module)
	if err := validateModuleRequest(input, true); err != nil {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, err
	}
	if err := s.authorize(ctx, actor, module, "write"); err != nil {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, err
	}
	definitions := s.definitionsForModule(module)
	if len(definitions) == 0 {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, ErrModuleNotFound
	}
	if len(input.ResetKeys) > 0 {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, fmt.Errorf("%w: resetKeys is only supported by the module reset operation", ErrInvalidSetting)
	}
	if module == "runtime" {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, ApplyMode: mergedApplyMode(definitions), RequestID: input.RequestID}, ErrPermissionDenied
	}
	if s.repo == nil {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, errors.New("settings repository unavailable")
	}
	// Service-level serialization protects repositories that only expose the
	// legacy per-key append primitive. Native AtomicModuleRepository adapters
	// additionally enforce the compare-and-swap in their transaction.
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	values, records, err := s.effectiveModuleValues(ctx, module, definitions)
	if err != nil {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, err
	}
	previous := cloneRawValues(values)
	for key, raw := range input.Values {
		definition, ok := definitions[key]
		if !ok {
			return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, fmt.Errorf("%w: unknown module key %q", ErrInvalidSetting, key)
		}
		if sensitiveInputIsNoop(definition, raw) {
			// Blank/masked credentials are intentionally omitted from the
			// candidate. Clearing is a separate, explicit operation.
			continue
		}
		if err := s.ensureDatabaseWritable(ctx, key, definition); err != nil {
			return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, err
		}
		if !definition.Editable {
			return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, ErrPermissionDenied
		}
		if source := records[key].Source; source != "" && source != SourceDatabase && source != SourceDefault {
			return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, fmt.Errorf("%w: %s", ErrSettingLocked, key)
		}
		values[key] = append(json.RawMessage(nil), raw...)
	}
	if err := validateModuleValues(module, definitions, values); err != nil {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, err
	}
	currentRevision, err := s.moduleRevision(ctx, module, definitions)
	if err != nil {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, err
	}
	if currentRevision != input.ExpectedRevision {
		return ModuleSaveResult{Module: module, Revision: currentRevision, PreviousRevision: currentRevision, Status: StatusSaveFailed, RequestID: input.RequestID}, errors.Join(ErrVersionConflict, ErrModuleRevisionConflict)
	}
	changed := make([]string, 0)
	for key, value := range values {
		if !sameJSON(previous[key], value) {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	result := ModuleSaveResult{Module: module, ID: module, PreviousRevision: currentRevision, ChangedKeys: changed, RequestID: input.RequestID, UpdatedAt: time.Now().UTC(), ApplyMode: mergedApplyModeForKeys(definitions, changed)}
	if len(changed) == 0 {
		// Keep the success envelope shape stable even when the submitted
		// candidate is identical to the effective module.  Consumers use the
		// returned settings to replace their draft and must not have to issue a
		// second read just because nothing changed.
		result.ID = module
		result.Revision = currentRevision
		result.Persisted, result.Applied, result.AuditRecorded, result.CacheSynced = true, true, true, true
		result.Status = StatusUnchanged
		result.Settings = s.presentModuleValues(StoredModule{Module: module, Revision: currentRevision, UpdatedAt: result.UpdatedAt}, definitions, values, records)
		return result, nil
	}
	storedValues := make(map[string]StoredSetting, len(changed))
	for _, key := range changed {
		definition := definitions[key]
		payload, encrypted, prepareErr := s.preparePayload(ctx, key, definition, values[key])
		if prepareErr != nil {
			return result, prepareErr
		}
		storedValues[key] = StoredSetting{Key: key, RawValue: payload, Sensitive: definition.Sensitive, Encrypted: encrypted, Source: SourceDatabase, UpdatedBy: actor.ID, UpdatedAt: result.UpdatedAt}
	}
	var committed StoredModule
	if repository, ok := s.repo.(AtomicModuleRepository); ok {
		committed, err = repository.SaveModule(ctx, module, storedValues, currentRevision)
	} else {
		committed, err = s.appendModuleCompat(ctx, module, storedValues, currentRevision)
	}
	if err != nil {
		if errors.Is(err, ErrVersionConflict) || errors.Is(err, ErrModuleRevisionConflict) {
			return result, errors.Join(ErrVersionConflict, ErrModuleRevisionConflict)
		}
		result.Status = StatusSaveFailed
		return result, err
	}
	result.Persisted = true
	result.Revision = committed.Revision
	if result.Revision == 0 {
		result.Revision = currentRevision + 1
	}
	// Cache invalidation and audit are deliberately best-effort after the DB
	// commit. A Redis outage must not make the local process forget its last
	// valid configuration.
	result.CacheSynced = true
	if cache, ok := s.cache.(ModuleCache); ok {
		if cacheErr := cache.InvalidateModule(ctx, module, result.Revision); cacheErr != nil {
			result.CacheSynced = false
			result.OtherNodesPending = true
		}
	}
	for _, key := range changed {
		if s.cache != nil {
			if cacheErr := s.cache.Invalidate(ctx, key); cacheErr != nil {
				result.CacheSynced = false
				result.OtherNodesPending = true
			}
		}
	}
	// Apply the candidate only after persistence succeeds. Deployment/restart
	// modes remain pending and intentionally keep the old live component.
	candidateSources := make(map[string]Source, len(values))
	for key := range values {
		source := records[key].Source
		if _, changedKey := storedValues[key]; changedKey {
			source = SourceDatabase
		}
		if source == "" {
			source = SourceDefault
		}
		candidateSources[key] = source
	}
	result.Applied, result.Status, result.ApplyError = s.applyModuleCandidate(ctx, module, definitions, values, candidateSources, result.ApplyMode)
	result.RequiresRestart = result.Status == StatusSavedPendingRestart || result.Status == StatusSavedPendingMigration
	s.setModuleState(module, result.Status)
	s.setModuleOutcome(module, moduleOutcome{ApplyError: result.ApplyError, OtherNodesPending: result.OtherNodesPending})
	if s.audit != nil {
		event := AuditEvent{ActorID: actor.ID, Action: "module_update", Module: module, Keys: append([]string(nil), changed...), Version: result.Revision, Revision: result.Revision, SaveResult: "saved", ApplyResult: string(result.Status), RequestID: input.RequestID}
		if auditErr := s.audit.Record(ctx, event); auditErr == nil {
			result.AuditRecorded = true
		}
	} else {
		result.AuditRecorded = true
	}
	result.Settings = s.presentModuleValues(committed, definitions, values, records)
	return result, nil
}

// UpdateModule is the canonical verb; SaveModule remains available for
// callers that use the UI wording.
func (s *Service) UpdateModule(ctx context.Context, actor Actor, input ModuleUpdateInput) (ModuleSaveResult, error) {
	return s.SaveModule(ctx, actor, input)
}

func (s *Service) appendModuleCompat(ctx context.Context, module string, values map[string]StoredSetting, expected int64) (StoredModule, error) {
	current, err := s.moduleRevision(ctx, module, s.definitionsForModule(module))
	if err != nil {
		return StoredModule{}, err
	}
	if current != expected {
		return StoredModule{}, errors.Join(ErrVersionConflict, ErrModuleRevisionConflict)
	}
	result := StoredModule{Module: module, Values: make(map[string]StoredSetting, len(values)), Revision: current + 1, UpdatedAt: time.Now().UTC()}
	for key, value := range values {
		record, appendErr := s.repo.Append(ctx, value)
		if appendErr != nil {
			return StoredModule{}, appendErr
		}
		result.Values[key] = record
	}
	return result, nil
}

func (s *Service) applyModuleCandidate(ctx context.Context, module string, definitions map[string]Definition, values map[string]json.RawMessage, sources map[string]Source, mode ApplyMode) (bool, ModuleStatus, string) {
	if mode == ApplyRestart {
		return false, StatusSavedPendingRestart, ""
	}
	if mode == ApplyDeployment {
		return false, StatusSavedPendingRestart, ""
	}
	if mode == ApplyMigration {
		return false, StatusSavedPendingMigration, ""
	}
	if mode == ApplyComponentReload && s.applier == nil {
		return false, StatusSavedPendingReload, ""
	}
	if s.applier != nil {
		if err := s.applier.Apply(ctx, module, cloneRawValues(values)); err != nil {
			return false, StatusSavedApplyFailed, sanitizeApplyError(err)
		}
	}
	if s.runtime != nil {
		// Publish from the exact tenant/organization partition being updated.
		// Reading the legacy process-wide slot here would merge another scope's
		// values into this candidate and leak them on the next request.
		snapshot := s.runtime.SnapshotFor(runtimeSnapshotScope(ctx))
		published := cloneRawValues(snapshot.Values)
		for key, raw := range values {
			if definition, ok := definitions[key]; ok && !definition.Sensitive {
				published[key] = append(json.RawMessage(nil), raw...)
			}
		}
		// Preserve source metadata for unchanged keys and publish the complete
		// candidate only after the component applier has succeeded.
		publishedSources := cloneSources(snapshot.Sources)
		for key, source := range sources {
			publishedSources[key] = source
		}
		if _, err := s.runtime.ReplaceWithSourcesFor(runtimeSnapshotScope(ctx), ctx, published, publishedSources); err != nil {
			return false, StatusSavedApplyFailed, sanitizeApplyError(err)
		}
	}
	return true, StatusSavedAndApplied, ""
}

func sanitizeApplyError(err error) string {
	if err == nil {
		return ""
	}
	// Runtime errors are returned to administrators as a bounded diagnostic;
	// redact common credential markers before crossing the transport boundary.
	message := err.Error()
	for _, token := range []string{"password", "secret", "token", "api_key", "authorization"} {
		lower := strings.ToLower(message)
		if index := strings.Index(lower, token); index >= 0 {
			message = strings.TrimSpace(message[:index]) + token + " redacted"
			break
		}
	}
	if len(message) > 256 {
		message = message[:256]
	}
	return message
}

func (s *Service) presentModuleValues(committed StoredModule, definitions map[string]Definition, values map[string]json.RawMessage, fallback ...map[string]StoredSetting) []Setting {
	records := make(map[string]StoredSetting, len(definitions))
	if len(fallback) > 0 {
		for key, record := range fallback[0] {
			records[key] = cloneStored(record)
		}
	}
	for key, record := range committed.Values {
		records[key] = cloneStored(record)
	}
	keys := make([]string, 0, len(definitions))
	for key := range definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]Setting, 0, len(keys))
	for _, key := range keys {
		record := records[key]
		if record.Key == "" {
			record = StoredSetting{Key: key, RawValue: values[key], Source: SourceDatabase, Version: committed.Revision, UpdatedAt: committed.UpdatedAt}
		}
		if record.RawValue == nil {
			record.RawValue = values[key]
		}
		result = append(result, s.present(record, definitions[key]))
	}
	return result
}

// ResetModule removes database overrides for the selected module. It is a
// distinct operation from writing defaults and therefore preserves source
// inheritance semantics.
func (s *Service) ResetModule(ctx context.Context, actor Actor, module string, expectedRevision int64, requestID string) (ModuleSaveResult, error) {
	module = moduleName(module)
	if err := validateModuleRequest(ModuleUpdateInput{Module: module, ExpectedRevision: expectedRevision, RequestID: requestID}, true); err != nil {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: requestID}, err
	}
	if err := s.authorize(ctx, actor, module, "reset"); err != nil {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: requestID}, err
	}
	definitions := s.definitionsForModule(module)
	if len(definitions) == 0 {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: requestID}, ErrModuleNotFound
	}
	if module == "runtime" {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, ApplyMode: mergedApplyMode(definitions), RequestID: requestID}, ErrPermissionDenied
	}
	// Serialize reset with module saves for repositories that do not expose a
	// native aggregate transaction.
	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	currentRevision, err := s.moduleRevision(ctx, module, definitions)
	if err != nil {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: requestID}, err
	}
	if expectedRevision != currentRevision {
		return ModuleSaveResult{Module: module, Revision: currentRevision, Status: StatusSaveFailed, RequestID: requestID}, errors.Join(ErrVersionConflict, ErrModuleRevisionConflict)
	}
	values, records, err := s.effectiveModuleValues(ctx, module, definitions)
	if err != nil {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: requestID}, err
	}
	// Read persisted rows independently of effective source resolution. A
	// deployment source can mask a stale database override; reset must still
	// remove that override rather than report a misleading no-op.
	databaseRecords, err := s.currentDatabaseOverrides(ctx, definitions)
	if err != nil {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: requestID}, err
	}
	changed := make([]string, 0)
	for key, record := range databaseRecords {
		if record.Source == SourceDatabase && databaseOverrideMatchesContext(ctx, record) {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	if len(changed) == 0 {
		updatedAt := time.Now().UTC()
		return ModuleSaveResult{Module: module, ID: module, Revision: currentRevision, PreviousRevision: currentRevision, ChangedKeys: changed, Settings: s.presentModuleValues(StoredModule{Module: module, Revision: currentRevision, UpdatedAt: updatedAt}, definitions, values, records), Persisted: true, Applied: true, Status: StatusUnchanged, ApplyMode: mergedApplyMode(definitions), CacheSynced: true, AuditRecorded: true, RequestID: requestID, UpdatedAt: updatedAt}, nil
	}
	resetRevision := currentRevision + 1
	if resetter, ok := s.repo.(AtomicModuleResetRepository); ok {
		resetResult, resetErr := resetter.ResetModule(ctx, module, currentRevision)
		if resetErr != nil {
			return ModuleSaveResult{Module: module, Revision: currentRevision, PreviousRevision: currentRevision, Status: StatusSaveFailed, RequestID: requestID}, resetErr
		}
		if resetResult.Revision > 0 {
			resetRevision = resetResult.Revision
		}
	} else {
		deleter, ok := s.repo.(SettingDeleter)
		if !ok {
			return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: requestID}, errors.New("settings repository does not support reset")
		}
		for _, key := range changed {
			if err := deleter.Delete(ctx, key); err != nil {
				return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: requestID}, err
			}
		}
	}
	// Re-resolve after deletion so runtime receives inherited defaults rather
	// than a copy of the removed override.
	values, records, err = s.effectiveModuleValues(ctx, module, definitions)
	if err != nil {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: requestID}, err
	}
	mode := mergedApplyModeForKeys(definitions, changed)
	candidateSources := make(map[string]Source, len(values))
	for key, record := range records {
		source := record.Source
		if source == "" {
			source = SourceDefault
		}
		candidateSources[key] = source
	}
	applied, status, applyErr := s.applyModuleCandidate(ctx, module, definitions, values, candidateSources, mode)
	s.setModuleState(module, status)
	result := ModuleSaveResult{Module: module, ID: module, Revision: resetRevision, PreviousRevision: currentRevision, ChangedKeys: changed, Persisted: true, Applied: applied, Status: status, ApplyMode: mode, ApplyError: applyErr, RequiresRestart: status == StatusSavedPendingRestart || status == StatusSavedPendingMigration, CacheSynced: true, AuditRecorded: true, RequestID: requestID, UpdatedAt: time.Now().UTC()}
	if cache, ok := s.cache.(ModuleCache); ok {
		if cacheErr := cache.InvalidateModule(ctx, module, resetRevision); cacheErr != nil {
			result.CacheSynced = false
			result.OtherNodesPending = true
		}
	}
	for _, key := range changed {
		if s.cache != nil {
			if cacheErr := s.cache.Invalidate(ctx, key); cacheErr != nil {
				result.CacheSynced = false
				result.OtherNodesPending = true
			}
		}
	}
	s.setModuleOutcome(module, moduleOutcome{ApplyError: result.ApplyError, OtherNodesPending: result.OtherNodesPending})
	if s.audit != nil {
		result.AuditRecorded = s.audit.Record(ctx, AuditEvent{ActorID: actor.ID, Action: "module_reset", Module: module, Keys: append([]string(nil), changed...), Revision: result.Revision, SaveResult: "saved", ApplyResult: string(status), RequestID: requestID}) == nil
	}
	result.Settings = s.presentModuleValues(StoredModule{Module: module, Revision: resetRevision, UpdatedAt: result.UpdatedAt}, definitions, values, records)
	return result, nil
}

// ClearCredentials removes explicitly selected sensitive database overrides.
// Empty input is rejected so an accidental empty form submission can never
// clear a credential. The operation is module-scoped and compare-and-swap
// guarded, just like SaveModule and ResetModule; only keys marked Sensitive
// by the code-owned definition table are accepted.
func (s *Service) ClearCredentials(ctx context.Context, actor Actor, input ClearCredentialsInput) (ModuleSaveResult, error) {
	module := moduleName(input.Module)
	failure := func(err error) (ModuleSaveResult, error) {
		return ModuleSaveResult{Module: module, Status: StatusSaveFailed, RequestID: input.RequestID}, err
	}
	if input.ExpectedRevision < 0 || len(input.Keys) == 0 {
		return failure(fmt.Errorf("%w: credential keys and a non-negative revision are required", ErrInvalidSetting))
	}
	if len(input.RequestID) > maxRequestIDLength {
		return failure(fmt.Errorf("%w: requestId is too long", ErrInvalidSetting))
	}
	if err := s.authorize(ctx, actor, module, "clear_credentials"); err != nil {
		return failure(err)
	}
	definitions := s.definitionsForModule(module)
	if len(definitions) == 0 {
		return failure(ErrModuleNotFound)
	}
	if module == "runtime" {
		return failure(ErrPermissionDenied)
	}
	if s.repo == nil {
		return failure(errors.New("settings repository unavailable"))
	}
	keys := make([]string, 0, len(input.Keys))
	seen := make(map[string]struct{}, len(input.Keys))
	for _, rawKey := range input.Keys {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			return failure(fmt.Errorf("%w: credential key is empty", ErrInvalidSetting))
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		definition, ok := definitions[key]
		if !ok || !definition.Sensitive {
			return failure(fmt.Errorf("%w: %s is not a sensitive setting", ErrInvalidSetting, key))
		}
		if !definition.Editable {
			return failure(ErrPermissionDenied)
		}
		if err := s.ensureDatabaseWritable(ctx, key, definition); err != nil {
			return failure(err)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	s.moduleMu.Lock()
	defer s.moduleMu.Unlock()
	currentRevision, err := s.moduleRevision(ctx, module, definitions)
	if err != nil {
		return failure(err)
	}
	if currentRevision != input.ExpectedRevision {
		return ModuleSaveResult{Module: module, Revision: currentRevision, PreviousRevision: currentRevision, Status: StatusSaveFailed, RequestID: input.RequestID}, errors.Join(ErrVersionConflict, ErrModuleRevisionConflict)
	}
	values, records, err := s.effectiveModuleValues(ctx, module, definitions)
	if err != nil {
		return failure(err)
	}
	databaseRecords, err := s.currentDatabaseOverrides(ctx, definitions)
	if err != nil {
		return failure(err)
	}
	changed := make([]string, 0, len(keys))
	for _, key := range keys {
		record, hasDatabase := databaseRecords[key]
		if hasDatabase && databaseOverrideMatchesContext(ctx, record) {
			changed = append(changed, key)
			continue
		}
		// A deployment-owned value cannot be cleared from the settings UI. A
		// default/inherited value is already clear and is an idempotent no-op.
		if source := records[key].Source; source != "" && source != SourceDefault && source != SourceDatabase {
			return failure(fmt.Errorf("%w: %s", ErrSettingLocked, key))
		}
	}
	updatedAt := time.Now().UTC()
	result := ModuleSaveResult{Module: module, ID: module, Revision: currentRevision, PreviousRevision: currentRevision, ChangedKeys: changed, Persisted: true, Applied: true, Status: StatusUnchanged, ApplyMode: mergedApplyModeForKeys(definitions, changed), CacheSynced: true, AuditRecorded: true, RequestID: input.RequestID, UpdatedAt: updatedAt}
	if len(changed) == 0 {
		result.Settings = s.presentModuleValues(StoredModule{Module: module, Revision: currentRevision, UpdatedAt: updatedAt}, definitions, values, records)
		return result, nil
	}
	result.ApplyMode = mergedApplyModeForKeys(definitions, changed)

	clearRevision := currentRevision + 1
	if clearer, ok := s.repo.(AtomicCredentialClearRepository); ok {
		cleared, clearErr := clearer.ClearSensitiveKeys(ctx, module, changed, currentRevision)
		if clearErr != nil {
			return ModuleSaveResult{Module: module, Revision: currentRevision, PreviousRevision: currentRevision, Status: StatusSaveFailed, RequestID: input.RequestID}, clearErr
		}
		if cleared.Revision > 0 {
			clearRevision = cleared.Revision
		}
	} else if deleter, ok := s.repo.(SettingDeleter); ok {
		// The mutex keeps legacy adapters consistent within this process. Native
		// adapters should implement AtomicCredentialClearRepository so the DB
		// transaction also protects concurrent nodes.
		for _, key := range changed {
			if deleteErr := deleter.Delete(ctx, key); deleteErr != nil && !errors.Is(deleteErr, ErrSettingNotFound) {
				return failure(deleteErr)
			}
		}
	} else {
		return failure(errors.New("settings repository does not support credential clearing"))
	}

	values, records, err = s.effectiveModuleValues(ctx, module, definitions)
	if err != nil {
		return failure(err)
	}
	candidateSources := make(map[string]Source, len(values))
	for key, record := range records {
		source := record.Source
		if source == "" {
			source = SourceDefault
		}
		candidateSources[key] = source
	}
	applied, status, applyErr := s.applyModuleCandidate(ctx, module, definitions, values, candidateSources, result.ApplyMode)
	result.Revision, result.Applied, result.Status, result.ApplyError = clearRevision, applied, status, applyErr
	result.RequiresRestart = status == StatusSavedPendingRestart || status == StatusSavedPendingMigration
	s.setModuleState(module, status)
	if cache, ok := s.cache.(ModuleCache); ok {
		if cacheErr := cache.InvalidateModule(ctx, module, clearRevision); cacheErr != nil {
			result.CacheSynced = false
			result.OtherNodesPending = true
		}
	}
	for _, key := range changed {
		if s.cache != nil {
			if cacheErr := s.cache.Invalidate(ctx, key); cacheErr != nil {
				result.CacheSynced = false
				result.OtherNodesPending = true
			}
		}
	}
	s.setModuleOutcome(module, moduleOutcome{ApplyError: result.ApplyError, OtherNodesPending: result.OtherNodesPending})
	if s.audit != nil {
		auditErr := s.audit.Record(ctx, AuditEvent{ActorID: actor.ID, Action: "module_clear_credentials", Module: module, Keys: append([]string(nil), changed...), Revision: clearRevision, SaveResult: "saved", ApplyResult: string(status), RequestID: input.RequestID})
		result.AuditRecorded = auditErr == nil
	}
	result.Settings = s.presentModuleValues(StoredModule{Module: module, Revision: clearRevision, UpdatedAt: updatedAt}, definitions, values, records)
	return result, nil
}

// ClearCredentialsInput is the explicit credential-removal request. It is
// intentionally not folded into ModuleUpdateInput so ordinary blank secret
// fields retain their no-op semantics.
type ClearCredentialsInput struct {
	Module           string
	Keys             []string
	ExpectedRevision int64
	RequestID        string
}

// databaseOverrideMatchesContext distinguishes an organization override from
// a tenant-wide value inherited by that organization. Reset-to-default must
// remove only the exact scope's rows; otherwise an organization operator could
// accidentally clear the tenant fallback while resetting an inherited module.
func databaseOverrideMatchesContext(ctx context.Context, record StoredSetting) bool {
	if record.Source != SourceDatabase {
		return false
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		// Compatibility callers without a tenant context use the in-memory
		// repository and historically treated an empty organization as the only
		// scope. Preserve that behavior while GORM remains default-deny.
		return strings.TrimSpace(record.Organization) == ""
	}
	return strings.TrimSpace(record.Organization) == strings.TrimSpace(scope.Organization)
}

// currentDatabaseOverrides bypasses source precedence and returns only rows
// physically persisted by the settings repository. This is used exclusively by
// restore-default so an environment/YAML value masking an old DB row does not
// leave that row behind forever.
func (s *Service) currentDatabaseOverrides(ctx context.Context, definitions map[string]Definition) (map[string]StoredSetting, error) {
	result := make(map[string]StoredSetting, len(definitions))
	if s == nil || s.repo == nil {
		return result, nil
	}
	for key := range definitions {
		record, err := s.repo.Current(ctx, key)
		if errors.Is(err, ErrSettingNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if record.Source == "" {
			record.Source = SourceDatabase
		}
		record.Source = normalizeSource(record.Source)
		if record.Source == SourceDatabase {
			result[key] = record
		}
	}
	return result, nil
}

// RestoreDefaults is a descriptive alias for ResetModule.
func (s *Service) RestoreDefaults(ctx context.Context, actor Actor, module string, expectedRevision int64, requestID string) (ModuleSaveResult, error) {
	return s.ResetModule(ctx, actor, module, expectedRevision, requestID)
}

// LoadRuntimeSnapshot rebuilds the complete local snapshot from the database
// and definitions. It is safe to call at startup and after a Redis cache loss.
func (s *Service) LoadRuntimeSnapshot(ctx context.Context) (RuntimeSnapshot, error) {
	if s == nil || s.runtime == nil {
		return RuntimeSnapshot{}, ErrInvalidSetting
	}
	if ctx == nil {
		ctx = context.Background()
	}
	values := make(map[string]json.RawMessage)
	sources := make(map[string]Source)
	for key, definition := range s.activeDefinitions() {
		// Runtime diagnostics are supplied by RuntimeModuleProvider on demand;
		// keeping them out of the mutable configuration snapshot avoids freezing
		// a health probe or node identity during cache reconciliation.
		if moduleForDefinition(definition) == "runtime" {
			continue
		}
		// Sensitive settings stay encrypted in persistence and are intentionally
		// absent from the process snapshot. Skip them before resolve/readPayload
		// so a cache-loss reconciliation never needs to decrypt or retain a
		// credential merely to rebuild non-sensitive runtime state.
		if definition.Sensitive {
			continue
		}
		record, err := s.resolve(ctx, key, definition)
		if err != nil {
			return RuntimeSnapshot{}, err
		}
		raw, err := s.readPayload(ctx, key, record)
		if err != nil {
			return RuntimeSnapshot{}, err
		}
		values[key] = append(json.RawMessage(nil), raw...)
		sources[key] = record.Source
	}
	return s.runtime.ReplaceWithSourcesFor(runtimeSnapshotScope(ctx), ctx, values, sources)
}

// ReloadRuntimeSnapshot is an alias used by cache reconciliation workers.
func (s *Service) ReloadRuntimeSnapshot(ctx context.Context) (RuntimeSnapshot, error) {
	return s.LoadRuntimeSnapshot(ctx)
}

// ReconcileRuntimeSnapshot is the periodic recovery hook used when Redis
// invalidation messages may have been missed or a cache has been flushed. The
// database remains the source of truth, so reconciliation always rebuilds the
// complete snapshot instead of applying a partial notification payload. A
// failed read leaves the previous immutable snapshot untouched.
func (s *Service) ReconcileRuntimeSnapshot(ctx context.Context) (RuntimeSnapshot, error) {
	return s.LoadRuntimeSnapshot(ctx)
}

func validateModuleValues(module string, definitions map[string]Definition, values map[string]json.RawMessage) error {
	for key, definition := range definitions {
		raw, ok := values[key]
		if !ok {
			return fmt.Errorf("%w: missing required setting %s", ErrInvalidSetting, key)
		}
		if err := validateValue(definition, raw); err != nil {
			return err
		}
	}
	switch module {
	case "observability":
		config := domainobs.DefaultConfig()
		for key, raw := range values {
			if IsObservabilitySettingKey(key) {
				if err := applyObservabilitySetting(&config, key, raw); err != nil {
					return err
				}
			}
		}
		if err := config.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidSetting, err)
		}
	case "file":
		if max, ok := numberValue(values["file.max_size"]); ok {
			if quota, ok := numberValue(values["file.quota"]); ok && (max <= 0 || quota <= 0 || max > quota) {
				return fmt.Errorf("%w: file.max_size must not exceed file.quota", ErrInvalidSetting)
			}
		}
		if provider, ok := stringValue(values["file.provider"]); ok && provider != "local" {
			for _, key := range []string{"file.s3.endpoint", "file.s3.bucket", "file.s3.region"} {
				if value, valid := stringValue(values[key]); !valid || strings.TrimSpace(value) == "" {
					return fmt.Errorf("%w: %s is required for provider %s", ErrInvalidSetting, key, provider)
				}
			}
		}
	case "i18n":
		if locale, ok := stringValue(values["i18n.default_locale"]); ok {
			var supported []string
			if raw := values["i18n.supported_locales"]; len(raw) > 0 {
				if err := json.Unmarshal(raw, &supported); err == nil && len(supported) > 0 {
					found := false
					for _, item := range supported {
						if item == locale {
							found = true
							break
						}
					}
					if !found {
						return fmt.Errorf("%w: default locale must be supported", ErrInvalidSetting)
					}
				}
			}
		}
	case "security":
		for _, key := range []string{"security.access_ttl", "security.refresh_ttl"} {
			if value, ok := stringValue(values[key]); ok {
				duration, err := time.ParseDuration(value)
				if err != nil || duration <= 0 {
					return fmt.Errorf("%w: %s must be a positive duration", ErrInvalidSetting, key)
				}
			}
		}
	}
	return nil
}

func stringValue(raw []byte) (string, bool) {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func numberValue(raw []byte) (float64, bool) {
	var value float64
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return 0, false
	}
	return value, true
}

var _ AtomicModuleRepository = (*MemoryRepository)(nil)
var _ AtomicModuleResetRepository = (*MemoryRepository)(nil)
var _ CurrentSettingsRepository = (*MemoryRepository)(nil)
var _ SettingDeleter = (*MemoryRepository)(nil)
