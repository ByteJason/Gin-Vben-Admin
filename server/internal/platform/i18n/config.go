package i18n

import (
	"errors"
	"fmt"
	"strings"
)

const (
	LocaleZhCN = "zh-CN"
	LocaleEnUS = "en-US"
)

type Mode string

const (
	ModeSingle Mode = "single"
	ModeMulti  Mode = "multi"
)

var ErrInvalidRuntimeConfig = errors.New("invalid i18n runtime configuration")

// Config is the small, restart-bound locale policy shared by installation,
// API negotiation, and the three management templates. Only the two bundled
// locales are accepted in this release.
type Config struct {
	Mode             Mode     `mapstructure:"mode" yaml:"mode" json:"mode"`
	DefaultLocale    string   `mapstructure:"default_locale" yaml:"default_locale" json:"defaultLocale"`
	SupportedLocales []string `mapstructure:"supported_locales" yaml:"supported_locales" json:"supportedLocales"`
}

func DefaultConfig() Config {
	return Config{Mode: ModeSingle, DefaultLocale: LocaleZhCN, SupportedLocales: []string{LocaleZhCN, LocaleEnUS}}
}

func (c Config) Validate() error {
	mode := c.Mode
	if mode == "" {
		mode = ModeSingle
	}
	if mode != ModeSingle && mode != ModeMulti {
		return fmt.Errorf("%w: mode must be single or multi", ErrInvalidRuntimeConfig)
	}
	defaultLocale := NormalizeLocale(c.DefaultLocale)
	if defaultLocale == "" {
		return fmt.Errorf("%w: default locale is required", ErrInvalidRuntimeConfig)
	}
	locales := c.SupportedLocales
	if len(locales) == 0 {
		locales = DefaultConfig().SupportedLocales
	}
	seen := make(map[string]struct{}, len(locales))
	for _, value := range locales {
		locale := NormalizeLocale(value)
		if !isBundledLocale(locale) {
			return fmt.Errorf("%w: unsupported locale %q", ErrInvalidRuntimeConfig, value)
		}
		if _, duplicate := seen[locale]; duplicate {
			return fmt.Errorf("%w: duplicate locale %q", ErrInvalidRuntimeConfig, locale)
		}
		seen[locale] = struct{}{}
	}
	if _, ok := seen[defaultLocale]; !ok {
		return fmt.Errorf("%w: default locale %q is not supported", ErrInvalidRuntimeConfig, defaultLocale)
	}
	return nil
}

// Resolve selects a supported locale from an HTTP Accept-Language header.
// Single mode intentionally ignores the header and always uses the configured
// default; multi mode uses deterministic quality/order negotiation.
func (c Config) Resolve(header string) string {
	if c.Validate() != nil {
		return LocaleZhCN
	}
	if c.Mode != ModeMulti {
		return NormalizeLocale(c.DefaultLocale)
	}
	locales := c.supported()
	for _, preference := range parseAcceptLanguage(header) {
		for _, supported := range locales {
			if preference.locale == supported || languageOnly(preference.locale) == languageOnly(supported) {
				return supported
			}
		}
	}
	return NormalizeLocale(c.DefaultLocale)
}

func (c Config) VisibleLocales() []string {
	if c.Validate() != nil || c.Mode != ModeMulti {
		return []string{NormalizeLocale(c.DefaultLocale)}
	}
	return append([]string(nil), c.supported()...)
}

func (c Config) supported() []string {
	if len(c.SupportedLocales) == 0 {
		return append([]string(nil), DefaultConfig().SupportedLocales...)
	}
	result := make([]string, 0, len(c.SupportedLocales))
	for _, value := range c.SupportedLocales {
		result = append(result, NormalizeLocale(value))
	}
	return result
}

// SuggestLocale maps a browser hint to one of the bundled locales for the
// first-install form. It is only a suggestion; the explicit user selection is
// persisted by the installer.
func SuggestLocale(header string) string {
	for _, preference := range parseAcceptLanguage(header) {
		if languageOnly(preference.locale) == "zh" {
			return LocaleZhCN
		}
		if preference.locale != "" {
			return LocaleEnUS
		}
	}
	return LocaleEnUS
}

func NormalizeLocale(value string) string { return normalizeLocale(value) }

func isBundledLocale(value string) bool { return value == LocaleZhCN || value == LocaleEnUS }

// ValidateLocaleList canonicalizes a user-provided comma-separated list while
// keeping validation in one place for environment/bootstrap adapters.
func ValidateLocaleList(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: supported locales are required", ErrInvalidRuntimeConfig)
	}
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		locale := NormalizeLocale(strings.TrimSpace(value))
		if !isBundledLocale(locale) {
			return nil, fmt.Errorf("%w: unsupported locale %q", ErrInvalidRuntimeConfig, value)
		}
		if _, ok := seen[locale]; ok {
			return nil, fmt.Errorf("%w: duplicate locale %q", ErrInvalidRuntimeConfig, locale)
		}
		seen[locale] = struct{}{}
		result = append(result, locale)
	}
	return result, nil
}
