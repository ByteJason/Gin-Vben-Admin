package i18n

import (
	"reflect"
	"testing"
)

func TestRuntimeConfigValidatesSupportedLocalesAndNegotiatesRequests(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig().Validate() error = %v", err)
	}
	if got := cfg.Resolve("en-GB,en;q=0.8"); got != LocaleZhCN {
		t.Fatalf("single Resolve(en-GB) = %q, want %q", got, LocaleZhCN)
	}
	if got := cfg.Resolve("zh-Hant;q=0.9,en-US;q=0.8"); got != LocaleZhCN {
		t.Fatalf("Resolve(zh-Hant) = %q, want %q", got, LocaleZhCN)
	}
	if got := cfg.VisibleLocales(); !reflect.DeepEqual(got, []string{LocaleZhCN}) {
		t.Fatalf("single VisibleLocales() = %#v, want default only", got)
	}
	cfg.Mode = ModeMulti
	if got := cfg.Resolve("en-GB,en;q=0.8"); got != LocaleEnUS {
		t.Fatalf("multi Resolve(en-GB) = %q, want %q", got, LocaleEnUS)
	}
	if got := cfg.VisibleLocales(); !reflect.DeepEqual(got, []string{LocaleZhCN, LocaleEnUS}) {
		t.Fatalf("multi VisibleLocales() = %#v", got)
	}
}

func TestSuggestLocaleUsesBrowserHeaderWithoutExpandingSupportedSet(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   string
	}{
		{header: "zh-TW, en;q=0.5", want: LocaleZhCN},
		{header: "fr-FR, de;q=0.9", want: LocaleEnUS},
		{header: "", want: LocaleEnUS},
	} {
		if got := SuggestLocale(tc.header); got != tc.want {
			t.Fatalf("SuggestLocale(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}

func TestRuntimeConfigRejectsUnknownLocaleAndMissingDefault(t *testing.T) {
	cases := []Config{
		{Mode: ModeSingle, DefaultLocale: LocaleZhCN, SupportedLocales: []string{"zh-CN", "fr-FR"}},
		{Mode: ModeMulti, DefaultLocale: "fr-FR", SupportedLocales: []string{LocaleZhCN, LocaleEnUS}},
		{Mode: ModeMulti, DefaultLocale: LocaleZhCN, SupportedLocales: []string{LocaleZhCN, LocaleZhCN}},
	}
	for _, cfg := range cases {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate(%#v) error = nil", cfg)
		}
	}
}
