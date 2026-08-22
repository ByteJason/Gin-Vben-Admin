package settings

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultDefinitionsExposeV010ConfigurationCategories(t *testing.T) {
	definitions := DefaultDefinitions()
	for key, category := range map[string]Category{
		"basic.site_name":        CategoryBasic,
		"security.jwt_secret":    CategorySecurity,
		"mail.enabled":           CategoryMail,
		"file.max_size":          CategoryFile,
		"captcha.enabled":        CategoryCaptcha,
		"i18n.mode":              CategoryI18n,
		"i18n.default_locale":    CategoryI18n,
		"i18n.supported_locales": CategoryI18n,
	} {
		definition, ok := definitions[key]
		if !ok || definition.Category != category {
			t.Fatalf("definition %q = %#v, want category %q", key, definition, category)
		}
	}
}

func TestMapSourceResolverUsesDEC018Precedence(t *testing.T) {
	resolver := NewMapSourceResolver(map[string]map[Source][]byte{
		"basic.site_name": {
			SourceDatabase: []byte(`"DB"`),
			SourceYAML:     []byte(`"YAML"`),
			SourceDotEnv:   []byte(`"DOTENV"`),
			SourceEnv:      []byte(`"ENV"`),
		},
	})
	value, err := resolver.Resolve(context.Background(), "basic.site_name")
	if err != nil || value.Source != SourceEnv || string(value.RawValue) != `"ENV"` {
		t.Fatalf("resolved value = %#v err=%v", value, err)
	}
}

func TestServiceEncryptsSensitiveValuesAtRestAndKeepsResponseMasked(t *testing.T) {
	repo := NewMemoryRepository()
	encryptor, err := NewEnvelopeEncryptor([]byte("runtime-secret-for-settings"))
	if err != nil {
		t.Fatalf("NewEnvelopeEncryptor() error = %v", err)
	}
	svc := NewService(repo, nil, nil, DefaultDefinitions())
	svc.SetEncryptor(encryptor)
	setting, err := svc.Update(context.Background(), Actor{ID: "admin"}, UpdateInput{
		Key:             "security.jwt_secret",
		Value:           json.RawMessage(`"TOKEN"`),
		ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if setting.Value != maskedValue || setting.Source != SourceDatabase {
		t.Fatalf("setting = %#v", setting)
	}
	stored, err := repo.Current(context.Background(), "security.jwt_secret")
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if !stored.Encrypted || strings.Contains(string(stored.RawValue), "TOKEN") {
		t.Fatalf("stored sensitive value = %#v", stored)
	}
	plain, err := encryptor.Decrypt(context.Background(), "security.jwt_secret", stored.RawValue)
	if err != nil || string(plain) != `"TOKEN"` {
		t.Fatalf("decrypted value = %q err=%v", plain, err)
	}
}

func TestServiceReturnsEffectiveSourceAndConnectionRequestID(t *testing.T) {
	repo := NewMemoryRepository()
	svc := NewService(repo, nil, nil, DefaultDefinitions())
	svc.SetSourceResolver(NewMapSourceResolver(map[string]map[Source][]byte{
		"mail.port": {SourceDotEnv: []byte(`1026`)},
	}))
	result, err := svc.TestConnection(context.Background(), Actor{ID: "admin"}, "mail.port", "req-1", nil)
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if result.Status != "ok" || result.Source != SourceDotEnv || result.RequestID != "req-1" || result.Category != CategoryMail {
		t.Fatalf("result = %#v", result)
	}
}
