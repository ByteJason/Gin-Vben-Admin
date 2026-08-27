package model

// Module identifies the product area that owns a persistence model. The
// initial schema is installed as one transaction, while this label lets future
// versioned migrations and documentation distinguish shared, admin and client
// changes without duplicating model types.
type Module string

const (
	ModuleShared = Module("shared")
	ModuleAuth   = Module("auth")
	ModuleAdmin  = Module("admin")
	ModuleAudit  = Module("audit")
	ModuleClient = Module("client")
)

// Definition binds one table model to its ownership area. New values are
// created for every call so GORM cannot retain mutable schema state between
// migration runs or tests.
type Definition struct {
	Module Module
	New    func() any
}

var definitions = []Definition{
	{Module: ModuleShared, New: func() any { return &AppMetadata{} }},
	{Module: ModuleShared, New: func() any { return &Tenant{} }},
	{Module: ModuleShared, New: func() any { return &Organization{} }},
	{Module: ModuleShared, New: func() any { return &User{} }},
	{Module: ModuleAdmin, New: func() any { return &Role{} }},
	{Module: ModuleAdmin, New: func() any { return &UserRole{} }},
	{Module: ModuleAdmin, New: func() any { return &Menu{} }},
	{Module: ModuleAdmin, New: func() any { return &Permission{} }},
	{Module: ModuleAdmin, New: func() any { return &IAMPolicy{} }},
	{Module: ModuleAdmin, New: func() any { return &IAMDataScope{} }},
	{Module: ModuleAuth, New: func() any { return &AuthSession{} }},
	{Module: ModuleAudit, New: func() any { return &AuthAuditEvent{} }},
	{Module: ModuleAdmin, New: func() any { return &SettingVersion{} }},
	{Module: ModuleAdmin, New: func() any { return &FileObject{} }},
	{Module: ModuleAdmin, New: func() any { return &SMTPAccount{} }},
	{Module: ModuleAdmin, New: func() any { return &EmailMessage{} }},
	{Module: ModuleAdmin, New: func() any { return &EmailRecipient{} }},
	{Module: ModuleAdmin, New: func() any { return &EmailDeliveryAttempt{} }},
	{Module: ModuleAdmin, New: func() any { return &DictionaryType{} }},
	{Module: ModuleAdmin, New: func() any { return &DictionaryItem{} }},
	{Module: ModuleAdmin, New: func() any { return &DictionaryCacheVersion{} }},
	{Module: ModuleAdmin, New: func() any { return &TaskDefinition{} }},
	{Module: ModuleAdmin, New: func() any { return &TaskRun{} }},
	{Module: ModuleAdmin, New: func() any { return &TaskRunLog{} }},
	{Module: ModuleAdmin, New: func() any { return &ImportExportJob{} }},
	{Module: ModuleAdmin, New: func() any { return &ImportExportError{} }},
	{Module: ModuleAdmin, New: func() any { return &ImportExportArtifact{} }},
}

// Definitions returns the ordered model registry with fresh values.
func Definitions() []Definition {
	result := make([]Definition, len(definitions))
	copy(result, definitions)
	return result
}

// All returns models in the deterministic order used by the initial schema.
func All() []any {
	result := make([]any, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, definition.New())
	}
	return result
}

// ModelsFor returns fresh models owned by one module, preserving the initial
// registry order. It is intended for future module-scoped upgrade planning;
// the initial installer continues to call All so that creation remains one
// transaction and one file.
func ModelsFor(module Module) []any {
	result := make([]any, 0)
	for _, definition := range definitions {
		if definition.Module == module {
			result = append(result, definition.New())
		}
	}
	return result
}

// ModuleFor returns the ownership area for a model value. It is intentionally
// based on the table name, so aliases and fresh values behave identically.
func ModuleFor(value any) Module {
	if value == nil {
		return ""
	}
	name := ""
	if named, ok := value.(interface{ TableName() string }); ok {
		name = named.TableName()
	}
	for _, definition := range definitions {
		candidate := definition.New()
		if named, ok := candidate.(interface{ TableName() string }); ok && named.TableName() == name {
			return definition.Module
		}
	}
	return ""
}
