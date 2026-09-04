package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

type moduleApplierFixture struct {
	err   error
	calls int
}

func (f *moduleApplierFixture) Apply(_ context.Context, _ string, _ map[string]json.RawMessage) error {
	f.calls++
	return f.err
}

func TestActiveSettingsModulesExcludeRetiredMailDefinitions(t *testing.T) {
	svc := NewService(NewMemoryRepository(), nil, nil, nil)
	defs, err := svc.Definitions(context.Background(), Actor{ID: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range defs {
		if IsRetiredSettingKey(definition.Key) || definition.Category == CategoryMail {
			t.Fatalf("retired mail definition exposed: %+v", definition)
		}
		if definition.DisplayName == "" || definition.Group == "" || definition.ApplyMode == "" {
			t.Fatalf("incomplete active metadata: %+v", definition)
		}
		if definition.ApplyMode == ApplyDeployment && definition.Editable {
			t.Fatalf("deployment definition must be read-only: %+v", definition)
		}
	}
	modules, err := svc.Modules(context.Background(), Actor{ID: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	for _, module := range modules {
		if strings.Contains(strings.ToLower(module.ID), "mail") {
			t.Fatalf("retired mail module exposed: %+v", module)
		}
	}
}

func TestSourcePolicyIgnoresDisallowedDatabaseRowsAndBlocksOverride(t *testing.T) {
	definitions := map[string]Definition{
		"basic.site_name": {
			Key:          "basic.site_name",
			Category:     CategoryBasic,
			Kind:         KindString,
			Default:      `"compiled"`,
			DisplayName:  "站点名称",
			Group:        "basic",
			Editable:     true,
			SourcePolicy: []Source{SourceEnv, SourceDefault},
		},
	}
	repo := NewMemoryRepository()
	if _, err := repo.Append(context.Background(), StoredSetting{
		Key:      "basic.site_name",
		RawValue: json.RawMessage(`"database"`),
		Source:   SourceDatabase,
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	service := NewService(repo, nil, nil, definitions)
	setting, err := service.Get(context.Background(), Actor{ID: "admin"}, "basic.site_name")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if setting.Value != `"compiled"` || setting.Source != SourceDefault {
		t.Fatalf("disallowed database row became effective: %+v", setting)
	}
	_, err = service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module:           "basic",
		ExpectedRevision: 0,
		Values:           map[string]json.RawMessage{"basic.site_name": json.RawMessage(`"new"`)},
	})
	if !errors.Is(err, ErrSettingLocked) {
		t.Fatalf("SaveModule() error = %v, want ErrSettingLocked", err)
	}
}

func TestProcessEnvironmentSourceLocksModuleSave(t *testing.T) {
	t.Setenv("SITE_NAME", "deployed")
	repo := NewMemoryRepository()
	service := NewService(repo, nil, nil, nil)
	service.SetSourceResolver(NewProcessEnvironmentResolver(DefaultDefinitions()))
	setting, err := service.Get(context.Background(), Actor{ID: "admin"}, "basic.site_name")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if setting.Value != `"deployed"` || setting.Source != SourceEnv || setting.Editable {
		t.Fatalf("environment source was not surfaced as locked: %+v", setting)
	}
	_, err = service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module:           "basic",
		ExpectedRevision: 0,
		Values:           map[string]json.RawMessage{"basic.site_name": json.RawMessage(`"后台覆盖"`)},
	})
	if !errors.Is(err, ErrSettingLocked) {
		t.Fatalf("SaveModule() error = %v, want ErrSettingLocked", err)
	}
}

func TestResetModuleRemovesMaskedDatabaseOverride(t *testing.T) {
	t.Setenv("SITE_NAME", "deployed")
	repo := NewMemoryRepository()
	if _, err := repo.Append(context.Background(), StoredSetting{
		Key:      "basic.site_name",
		RawValue: json.RawMessage(`"stale"`),
		Source:   SourceDatabase,
	}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	service := NewService(repo, nil, nil, nil)
	service.SetSourceResolver(NewProcessEnvironmentResolver(DefaultDefinitions()))
	result, err := service.ResetModule(context.Background(), Actor{ID: "admin"}, "basic", 1, "reset-masked")
	if err != nil {
		t.Fatalf("ResetModule() error = %v", err)
	}
	if len(result.ChangedKeys) != 1 || result.ChangedKeys[0] != "basic.site_name" {
		t.Fatalf("reset changed keys = %#v", result.ChangedKeys)
	}
	if _, err := repo.Current(context.Background(), "basic.site_name"); !errors.Is(err, ErrSettingNotFound) {
		t.Fatalf("stale database override remains: %v", err)
	}
	setting, err := service.Get(context.Background(), Actor{ID: "admin"}, "basic.site_name")
	if err != nil || setting.Source != SourceEnv || setting.Value != `"deployed"` {
		t.Fatalf("effective deployment value after reset = %+v err=%v", setting, err)
	}
}

func TestSaveModuleIsAtomicAndPublishesOneRevision(t *testing.T) {
	repo := NewMemoryRepository()
	store := NewRuntimeSnapshotStore()
	service := NewService(repo, nil, nil, nil)
	service.SetRuntimeSnapshotStore(store)
	service.SetRuntimeApplier(&moduleApplierFixture{})
	first, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "observability", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{
			"observability.metrics.enabled":  []byte("true"),
			"observability.metrics.endpoint": []byte(`"https://metrics.example.test"`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Persisted || !first.Applied || first.Revision != 1 || first.Status != StatusSavedAndApplied {
		t.Fatalf("first module result = %+v", first)
	}
	if got, ok := store.Value("observability.metrics.enabled"); !ok || string(got) != "true" {
		t.Fatalf("runtime snapshot missing enabled value: %s %v", got, ok)
	}
	second, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "observability", ExpectedRevision: first.Revision,
		Values: map[string]json.RawMessage{"observability.metrics.enabled": []byte("false")},
	})
	if err != nil || second.Revision != 2 || len(second.ChangedKeys) != 1 {
		t.Fatalf("second module result = %+v err=%v", second, err)
	}
	_, err = service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "observability", ExpectedRevision: 1,
		Values: map[string]json.RawMessage{"observability.metrics.enabled": []byte("true")},
	})
	if !errors.Is(err, ErrModuleRevisionConflict) {
		t.Fatalf("stale module save error = %v", err)
	}
}

func TestSaveModuleUnchangedStillReturnsCompleteResult(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, nil, nil, nil)
	first, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{"basic.site_name": []byte(`"Portal"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: first.Revision,
		Values: map[string]json.RawMessage{"basic.site_name": []byte(`"Portal"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != StatusUnchanged || second.ID != "basic" || len(second.Settings) == 0 {
		t.Fatalf("unchanged module result is incomplete: %+v", second)
	}
}

func TestBlankSensitiveModuleValueIsNoop(t *testing.T) {
	definitions := map[string]Definition{
		"basic.api_token": {
			Key: "basic.api_token", Category: CategoryBasic, Group: "basic",
			Kind: KindSecret, Sensitive: true, Default: `""`, Editable: true,
		},
	}
	repo := NewMemoryRepository()
	service := NewService(repo, nil, nil, definitions)
	first, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{"basic.api_token": []byte(`"secret"`)},
	})
	if err != nil {
		t.Fatalf("initial SaveModule() error = %v", err)
	}
	second, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: first.Revision,
		Values: map[string]json.RawMessage{"basic.api_token": []byte(`"   "`)},
	})
	if err != nil {
		t.Fatalf("blank SaveModule() error = %v", err)
	}
	if second.Status != StatusUnchanged || second.Revision != first.Revision || len(second.ChangedKeys) != 0 {
		t.Fatalf("blank secret changed the module: %+v", second)
	}
	record, err := repo.Current(context.Background(), "basic.api_token")
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if string(record.RawValue) != `"secret"` || record.Version != first.Revision {
		t.Fatalf("blank secret replaced persisted value: %+v", record)
	}
}

func TestBlankSensitiveLegacyUpdateIsNoop(t *testing.T) {
	definitions := map[string]Definition{
		"basic.api_token": {
			Key: "basic.api_token", Category: CategoryBasic, Group: "basic",
			Kind: KindSecret, Sensitive: true, Default: `""`, Editable: true,
		},
	}
	repo := NewMemoryRepository()
	service := NewService(repo, nil, nil, definitions)
	first, err := service.Update(context.Background(), Actor{ID: "admin"}, UpdateInput{
		Key: "basic.api_token", Value: []byte(`"secret"`), ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("initial Update() error = %v", err)
	}
	second, err := service.Update(context.Background(), Actor{ID: "admin"}, UpdateInput{
		Key: "basic.api_token", Value: []byte(`""`), ExpectedVersion: first.Version,
	})
	if err != nil {
		t.Fatalf("blank Update() error = %v", err)
	}
	if second.Version != first.Version || second.Value != maskedValue || !second.Sensitive {
		t.Fatalf("blank legacy update was not a no-op: %+v", second)
	}
	record, err := repo.Current(context.Background(), "basic.api_token")
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if string(record.RawValue) != `"secret"` || record.Version != first.Version {
		t.Fatalf("blank legacy update replaced persisted value: %+v", record)
	}
}

func TestSaveModuleKeepsPreviousSnapshotWhenApplyFails(t *testing.T) {
	repo := NewMemoryRepository()
	store := NewRuntimeSnapshotStore()
	service := NewService(repo, nil, nil, nil)
	service.SetRuntimeSnapshotStore(store)
	if _, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{"basic.site_name": []byte(`"before"`)},
	}); err != nil {
		t.Fatal(err)
	}
	previous, _ := store.Value("basic.site_name")
	applier := &moduleApplierFixture{err: errors.New("component secret failed")}
	service.SetRuntimeApplier(applier)
	result, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: 1,
		Values: map[string]json.RawMessage{"basic.site_name": []byte(`"after"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Persisted || result.Applied || result.Status != StatusSavedApplyFailed || !strings.Contains(result.ApplyError, "component") {
		t.Fatalf("apply failure result = %+v", result)
	}
	current, _ := store.Value("basic.site_name")
	if string(current) != string(previous) {
		t.Fatalf("previous runtime snapshot replaced on apply failure: %s -> %s", previous, current)
	}
	if applier.calls != 1 {
		t.Fatalf("applier calls = %d", applier.calls)
	}
}

func TestClearCredentialsIsExplicitAndLeavesOtherModuleValues(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, nil, nil, nil)
	service.SetRuntimeApplier(&moduleApplierFixture{})
	first, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "file", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{
			"file.provider":      []byte(`"s3"`),
			"file.s3.endpoint":   []byte(`"https://objects.example.test"`),
			"file.s3.bucket":     []byte(`"media"`),
			"file.s3.region":     []byte(`"auto"`),
			"file.s3.access_key": []byte(`"access"`),
			"file.s3.secret_key": []byte(`"secret-value"`),
			"file.s3.path_style": []byte(`true`),
		},
	})
	if err != nil {
		t.Fatalf("SaveModule() error = %v", err)
	}
	if first.Revision == 0 {
		t.Fatalf("SaveModule() revision = %#v", first)
	}
	result, err := service.ClearCredentials(context.Background(), Actor{ID: "admin"}, ClearCredentialsInput{
		Module: "file", Keys: []string{"file.s3.secret_key"}, ExpectedRevision: first.Revision, RequestID: "clear-1",
	})
	if err != nil {
		t.Fatalf("ClearCredentials() error = %v", err)
	}
	if !result.Persisted || !result.Applied || result.Status != StatusSavedAndApplied || len(result.ChangedKeys) != 1 || result.ChangedKeys[0] != "file.s3.secret_key" {
		t.Fatalf("clear result = %+v", result)
	}
	if _, err := repo.Current(context.Background(), "file.s3.secret_key"); !errors.Is(err, ErrSettingNotFound) {
		t.Fatalf("cleared credential remains in repository: %v", err)
	}
	provider, err := service.Get(context.Background(), Actor{ID: "admin"}, "file.provider")
	if err != nil || provider.Value != `"s3"` {
		t.Fatalf("unrelated module value changed: %+v err=%v", provider, err)
	}
	secret, err := service.Get(context.Background(), Actor{ID: "admin"}, "file.s3.secret_key")
	if err != nil || secret.Value != maskedValue || secret.Source != SourceDefault {
		t.Fatalf("cleared credential effective value = %+v err=%v", secret, err)
	}
}

func TestClearCredentialsRejectsNonSensitiveAndDeploymentOwnedKeys(t *testing.T) {
	service := NewService(NewMemoryRepository(), nil, nil, nil)
	if _, err := service.ClearCredentials(context.Background(), Actor{ID: "admin"}, ClearCredentialsInput{Module: "file", Keys: []string{"file.provider"}, ExpectedRevision: 0}); !errors.Is(err, ErrInvalidSetting) {
		t.Fatalf("non-sensitive clear error = %v", err)
	}
	t.Setenv("FILE_S3_SECRET_KEY", "deployed-secret")
	service.SetSourceResolver(NewProcessEnvironmentResolver(DefaultDefinitions()))
	if _, err := service.ClearCredentials(context.Background(), Actor{ID: "admin"}, ClearCredentialsInput{Module: "file", Keys: []string{"file.s3.secret_key"}, ExpectedRevision: 0}); !errors.Is(err, ErrSettingLocked) {
		t.Fatalf("deployment-owned clear error = %v", err)
	}
}

func TestResetModuleDeletesDatabaseOverride(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, nil, nil, nil)
	if _, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{"basic.site_name": []byte(`"override"`)},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := service.ResetModule(context.Background(), Actor{ID: "admin"}, "basic", 1, "reset-1")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Persisted || len(result.ChangedKeys) == 0 {
		t.Fatalf("reset result = %+v", result)
	}
	setting, err := service.Get(context.Background(), Actor{ID: "admin"}, "basic.site_name")
	if err != nil || setting.Source != SourceDefault || setting.Value != `"Gin-Vben-Admin"` {
		t.Fatalf("reset effective setting = %+v err=%v", setting, err)
	}
}

func TestMemoryRepositoryKeepsOrganizationOverridesIsolated(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, nil, nil, nil)
	baseCtx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	if _, err := service.SaveModule(baseCtx, Actor{ID: "admin"}, ModuleUpdateInput{Module: "basic", ExpectedRevision: 0, Values: map[string]json.RawMessage{"basic.site_name": []byte(`"tenant"`)}}); err != nil {
		t.Fatal(err)
	}
	orgACtx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	orgA, err := service.SaveModule(orgACtx, Actor{ID: "admin"}, ModuleUpdateInput{Module: "basic", ExpectedRevision: 1, Values: map[string]json.RawMessage{"basic.site_name": []byte(`"org-a"`)}})
	if err != nil {
		t.Fatal(err)
	}
	orgBCtx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-b"})
	if _, err := service.Get(orgBCtx, Actor{ID: "admin"}, "basic.site_name"); err != nil {
		t.Fatal(err)
	}
	orgB, err := service.SaveModule(orgBCtx, Actor{ID: "admin"}, ModuleUpdateInput{Module: "basic", ExpectedRevision: 1, Values: map[string]json.RawMessage{"basic.site_name": []byte(`"org-b"`)}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := service.Get(orgACtx, Actor{ID: "admin"}, "basic.site_name"); got.Value != `"org-a"` {
		t.Fatalf("org-a value = %+v", got)
	}
	if got, _ := service.Get(orgBCtx, Actor{ID: "admin"}, "basic.site_name"); got.Value != `"org-b"` {
		t.Fatalf("org-b value = %+v", got)
	}
	if got, _ := service.Get(baseCtx, Actor{ID: "admin"}, "basic.site_name"); got.Value != `"tenant"` {
		t.Fatalf("tenant value = %+v", got)
	}
	if _, err := service.ResetModule(orgACtx, Actor{ID: "admin"}, "basic", orgA.Revision, "reset-org-a"); err != nil {
		t.Fatal(err)
	}
	if got, _ := service.Get(orgACtx, Actor{ID: "admin"}, "basic.site_name"); got.Value != `"tenant"` {
		t.Fatalf("org-a fallback after reset = %+v", got)
	}
	if got, _ := service.Get(orgBCtx, Actor{ID: "admin"}, "basic.site_name"); got.Value != `"org-b"` {
		t.Fatalf("org-b changed after org-a reset = %+v", got)
	}
	if orgB.Revision == 0 {
		t.Fatal("org-b revision was not advanced")
	}
}

func TestRuntimeModuleIsReadOnlyAndProviderBacked(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, nil, nil, nil)
	service.SetRuntimeModuleProvider(RuntimeModuleProviderFunc(func(context.Context) (map[string]StoredSetting, error) {
		return map[string]StoredSetting{
			"runtime.version": {
				Key: "runtime.version", RawValue: json.RawMessage(`"1.2.3"`), Source: SourceYAML,
			},
			"runtime.database.status": {
				Key: "runtime.database.status", RawValue: json.RawMessage(`"ok"`), Source: SourceYAML,
			},
		}, nil
	}))

	modules, err := service.Modules(context.Background(), Actor{ID: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	var runtimeDefinition *ModuleDefinition
	for index := range modules {
		if modules[index].ID == "runtime" {
			runtimeDefinition = &modules[index]
			break
		}
	}
	if runtimeDefinition == nil {
		t.Fatalf("runtime module missing: %+v", modules)
	}
	if runtimeDefinition.Category != CategoryRuntime || runtimeDefinition.Editable {
		t.Fatalf("runtime module policy = %+v", *runtimeDefinition)
	}
	if runtimeDefinition.ApplyMode != ApplyDeployment {
		t.Fatalf("runtime module apply mode = %q, want deployment", runtimeDefinition.ApplyMode)
	}

	view, err := service.GetModule(context.Background(), Actor{ID: "admin"}, "runtime")
	if err != nil {
		t.Fatal(err)
	}
	if view.DisplayName != "运行环境" || view.Category != CategoryRuntime || view.RequiresRestart {
		t.Fatalf("runtime view identity = %+v", view)
	}
	for _, definition := range view.Definitions {
		if definition.Editable || definition.ApplyMode != ApplyDeployment || definition.Sensitive {
			t.Fatalf("runtime definition is not read-only deployment metadata: %+v", definition)
		}
	}
	values := map[string]Setting{}
	for _, setting := range view.Settings {
		values[setting.Key] = setting
	}
	if values["runtime.version"].Value != `"1.2.3"` || values["runtime.version"].Source != SourceYAML {
		t.Fatalf("provider value = %+v", values["runtime.version"])
	}
	if values["runtime.database.status"].Value != `"ok"` {
		t.Fatalf("database status = %+v", values["runtime.database.status"])
	}
	if values["runtime.config.revision"].Value != "0" || values["runtime.config.revision"].Source != SourceDefault {
		t.Fatalf("default config revision = %+v", values["runtime.config.revision"])
	}

	result, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "runtime", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{"runtime.node": json.RawMessage(`"forged"`)},
	})
	if !errors.Is(err, ErrPermissionDenied) || result.Status != StatusSaveFailed {
		t.Fatalf("runtime save result=%+v err=%v", result, err)
	}
	if _, err := repo.Current(context.Background(), "runtime.node"); !errors.Is(err, ErrSettingNotFound) {
		t.Fatalf("runtime value was persisted: %v", err)
	}
}

func TestRuntimeConfigRevisionReflectsMutableModuleRevision(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, nil, nil, nil)
	if _, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{"basic.site_name": json.RawMessage(`"changed"`)},
	}); err != nil {
		t.Fatal(err)
	}
	view, err := service.GetModule(context.Background(), Actor{ID: "admin"}, "runtime")
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range view.Settings {
		if setting.Key == "runtime.config.revision" {
			if setting.Value != "1" || setting.Source != SourceDatabase {
				t.Fatalf("runtime revision = %+v", setting)
			}
			return
		}
	}
	t.Fatal("runtime.config.revision missing")
}

func TestLoadRuntimeSnapshotSkipsEncryptedSensitiveValues(t *testing.T) {
	repo := NewMemoryRepository()
	// The encrypted payload is deliberately not decryptable by this service;
	// snapshot hydration must skip it rather than failing or retaining a secret.
	if _, err := repo.Append(context.Background(), StoredSetting{Key: "observability.otlp.api_key", RawValue: []byte(`"ciphertext"`), Encrypted: true, Sensitive: true, Source: SourceDatabase}); err != nil {
		t.Fatal(err)
	}
	store := NewRuntimeSnapshotStore()
	service := NewService(repo, nil, nil, nil)
	service.SetRuntimeSnapshotStore(store)
	if _, err := service.LoadRuntimeSnapshot(context.Background()); err != nil {
		t.Fatalf("LoadRuntimeSnapshot() error = %v", err)
	}
	if _, ok := store.Value("observability.otlp.api_key"); ok {
		t.Fatal("sensitive setting appeared in runtime snapshot")
	}
}

func TestGetModuleRetainsApplyFailureStatusWithoutReplacingSnapshot(t *testing.T) {
	repo := NewMemoryRepository()
	store := NewRuntimeSnapshotStore()
	service := NewService(repo, nil, nil, nil)
	service.SetRuntimeSnapshotStore(store)
	if _, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{"basic.site_name": json.RawMessage(`"before"`)},
	}); err != nil {
		t.Fatal(err)
	}
	service.SetRuntimeApplier(&moduleApplierFixture{err: errors.New("reload failed")})
	result, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: 1,
		Values: map[string]json.RawMessage{"basic.site_name": json.RawMessage(`"after"`)},
	})
	if err != nil || result.Status != StatusSavedApplyFailed {
		t.Fatalf("save result=%+v err=%v", result, err)
	}
	view, err := service.GetModule(context.Background(), Actor{ID: "admin"}, "basic")
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != StatusSavedApplyFailed {
		t.Fatalf("module status=%q, want %q", view.Status, StatusSavedApplyFailed)
	}
}

func TestGetModuleInfersPendingStateFromChangedKeyApplyMode(t *testing.T) {
	repo := NewMemoryRepository()
	// The file module contains a migration-only provider switch and
	// component-reload S3 fields. Persisting only an S3 field must not inherit
	// the module's aggregate migration mode after a process restart.
	if _, err := repo.Append(context.Background(), StoredSetting{
		Key: "file.s3.endpoint", RawValue: json.RawMessage(`"https://objects.example.test"`), Source: SourceDatabase,
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(repo, nil, nil, nil)
	view, err := service.GetModule(context.Background(), Actor{ID: "admin"}, "file")
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != StatusSavedPendingReload {
		t.Fatalf("file status = %q, want %q", view.Status, StatusSavedPendingReload)
	}
	if view.Status == StatusSavedPendingMigration {
		t.Fatal("component-reload override was incorrectly reported as migration")
	}
}

type failedModuleCache struct{}

func (failedModuleCache) Invalidate(context.Context, string) error {
	return errors.New("cache unavailable")
}

func (failedModuleCache) InvalidateModule(context.Context, string, int64) error {
	return errors.New("cache unavailable")
}

func TestSaveModuleMarksOtherNodesPendingWhenCacheSyncFails(t *testing.T) {
	service := NewService(NewMemoryRepository(), nil, failedModuleCache{}, nil)
	result, err := service.SaveModule(context.Background(), Actor{ID: "admin"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{"basic.site_name": json.RawMessage(`"cached"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A failed cache notification must not undo the committed database write.
	// The local runtime remains valid; only the distributed state is marked
	// pending.
	if result.Status != StatusSavedAndApplied || !result.Persisted || !result.Applied || result.CacheSynced || !result.OtherNodesPending {
		t.Fatalf("cache failure result = %+v", result)
	}
}

func TestRuntimeSnapshotIsPartitionedByTenantAndOrganization(t *testing.T) {
	repo := NewMemoryRepository()
	store := NewRuntimeSnapshotStore()
	service := NewService(repo, nil, nil, nil)
	service.SetRuntimeSnapshotStore(store)

	tenantA, err := tenant.NewContext("tenant-a", "org-a", false)
	if err != nil {
		t.Fatal(err)
	}
	tenantB, err := tenant.NewContext("tenant-b", "org-b", false)
	if err != nil {
		t.Fatal(err)
	}
	ctxA := tenant.WithContext(context.Background(), tenantA)
	ctxB := tenant.WithContext(context.Background(), tenantB)
	if _, err := service.SaveModule(ctxA, Actor{ID: "admin-a"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{"basic.site_name": json.RawMessage(`"Tenant A"`)},
	}); err != nil {
		t.Fatal(err)
	}
	if got, ok := service.RuntimeValueForContext(ctxB, "basic.site_name"); ok || got != nil {
		t.Fatalf("tenant B received tenant A snapshot: %q present=%v", got, ok)
	}
	setting, err := service.Get(ctxB, Actor{ID: "admin-b"}, "basic.site_name")
	if err != nil {
		t.Fatal(err)
	}
	if setting.Value == `"Tenant A"` || setting.Source != SourceDefault {
		t.Fatalf("tenant B inherited tenant A value: %+v", setting)
	}
	if got, ok := service.RuntimeValueForContext(ctxA, "basic.site_name"); !ok || string(got) != `"Tenant A"` {
		t.Fatalf("tenant A snapshot missing after save: %q present=%v", got, ok)
	}
}

func TestLegacyUpdatePublishesToTenantSnapshot(t *testing.T) {
	repo := NewMemoryRepository()
	store := NewRuntimeSnapshotStore()
	service := NewService(repo, nil, nil, nil)
	service.SetRuntimeSnapshotStore(store)
	scope, err := tenant.NewContext("tenant-scoped", "org-a", false)
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenant.WithContext(context.Background(), scope)
	if _, err := service.Update(ctx, Actor{ID: "admin"}, UpdateInput{
		Key: "basic.site_name", Value: json.RawMessage(`"scoped"`), ExpectedVersion: 0,
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if value, ok := service.RuntimeValueForContext(ctx, "basic.site_name"); !ok || string(value) != `"scoped"` {
		t.Fatalf("scoped snapshot value = %s present=%v", value, ok)
	}
	if value, ok := service.RuntimeValue("basic.site_name"); ok || value != nil {
		t.Fatalf("legacy global snapshot was populated by scoped update: %s present=%v", value, ok)
	}
}

func TestMemoryRepositoryDoesNotShareSameOrganizationAcrossTenants(t *testing.T) {
	repo := NewMemoryRepository()
	service := NewService(repo, nil, nil, nil)
	first, err := tenant.NewContext("tenant-one", "org-shared", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := tenant.NewContext("tenant-two", "org-shared", false)
	if err != nil {
		t.Fatal(err)
	}
	ctxOne := tenant.WithContext(context.Background(), first)
	ctxTwo := tenant.WithContext(context.Background(), second)
	if _, err := service.SaveModule(ctxOne, Actor{ID: "admin-1"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{"basic.site_name": json.RawMessage(`"one"`)},
	}); err != nil {
		t.Fatalf("tenant one save error = %v", err)
	}
	if got, err := service.Get(ctxTwo, Actor{ID: "admin-2"}, "basic.site_name"); err != nil || got.Value == `"one"` {
		t.Fatalf("tenant two observed tenant one value: %+v err=%v", got, err)
	}
	if _, err := service.SaveModule(ctxTwo, Actor{ID: "admin-2"}, ModuleUpdateInput{
		Module: "basic", ExpectedRevision: 0,
		Values: map[string]json.RawMessage{"basic.site_name": json.RawMessage(`"two"`)},
	}); err != nil {
		t.Fatalf("tenant two save error = %v", err)
	}
	if got, err := service.Get(ctxOne, Actor{ID: "admin-1"}, "basic.site_name"); err != nil || got.Value != `"one"` {
		t.Fatalf("tenant one value changed after tenant two save: %+v err=%v", got, err)
	}
}
