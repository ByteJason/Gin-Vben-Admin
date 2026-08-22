// Package i18n provides the small runtime locale contract used by API and UI
// boundaries. Translation storage remains replaceable; this package only
// resolves locales and applies deterministic fallback behavior.
package i18n

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

var ErrInvalidCatalog = errors.New("invalid translation catalog")

type Catalog struct {
	fallback string
	messages map[string]map[string]string
	missing  func(MissingTranslation)
}

type MissingTranslation struct {
	Locale string
	Key    string
}

func NewCatalog(fallback string, messages map[string]map[string]string) (*Catalog, error) {
	return NewCatalogWithSink(fallback, messages, nil)
}

func NewCatalogWithSink(fallback string, messages map[string]map[string]string, sink func(MissingTranslation)) (*Catalog, error) {
	fallback = normalizeLocale(fallback)
	if fallback == "" || len(messages) == 0 {
		return nil, ErrInvalidCatalog
	}
	copyMessages := make(map[string]map[string]string, len(messages))
	for locale, entries := range messages {
		locale = normalizeLocale(locale)
		if locale == "" {
			return nil, ErrInvalidCatalog
		}
		copyMessages[locale] = make(map[string]string, len(entries))
		for key, value := range entries {
			if strings.TrimSpace(key) == "" {
				return nil, ErrInvalidCatalog
			}
			copyMessages[locale][key] = value
		}
	}
	if _, ok := copyMessages[fallback]; !ok {
		return nil, ErrInvalidCatalog
	}
	return &Catalog{fallback: fallback, messages: copyMessages, missing: sink}, nil
}

// Resolve returns the best supported locale for an Accept-Language value.
// Unsupported or malformed values resolve to the catalog fallback.
func (c *Catalog) Resolve(header string) string {
	if c == nil {
		return ""
	}
	for _, preference := range parseAcceptLanguage(header) {
		if locale := c.match(preference.locale); locale != "" {
			return locale
		}
	}
	return c.fallback
}

func (c *Catalog) Translate(locale, key string) string {
	if c == nil || strings.TrimSpace(key) == "" {
		return key
	}
	for _, candidate := range []string{normalizeLocale(locale), c.fallback} {
		for _, resolved := range []string{candidate, languageOnly(candidate)} {
			if entries, ok := c.messages[resolved]; ok {
				if value, ok := entries[key]; ok {
					return value
				}
			}
		}
	}
	if c.missing != nil {
		c.missing(MissingTranslation{Locale: normalizeLocale(locale), Key: key})
	}
	return key
}

func (c *Catalog) match(locale string) string {
	locale = normalizeLocale(locale)
	if _, ok := c.messages[locale]; ok {
		return locale
	}
	base := languageOnly(locale)
	for candidate := range c.messages {
		if languageOnly(candidate) == base {
			return candidate
		}
	}
	return ""
}

type languagePreference struct {
	locale  string
	quality float64
	order   int
}

func parseAcceptLanguage(header string) []languagePreference {
	var preferences []languagePreference
	for order, raw := range strings.Split(header, ",") {
		parts := strings.Split(raw, ";")
		locale := normalizeLocale(parts[0])
		if locale == "" || locale == "*" {
			continue
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			name, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(name, "q") {
				if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed >= 0 && parsed <= 1 {
					quality = parsed
				}
			}
		}
		preferences = append(preferences, languagePreference{locale: locale, quality: quality, order: order})
	}
	sort.SliceStable(preferences, func(i, j int) bool {
		if preferences[i].quality == preferences[j].quality {
			return preferences[i].order < preferences[j].order
		}
		return preferences[i].quality > preferences[j].quality
	})
	return preferences
}

func normalizeLocale(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", "-"))
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "-")
	parts[0] = strings.ToLower(parts[0])
	if len(parts) > 1 {
		parts[1] = strings.ToUpper(parts[1])
	}
	return strings.Join(parts, "-")
}

func languageOnly(locale string) string {
	if index := strings.IndexByte(locale, '-'); index >= 0 {
		return locale[:index]
	}
	return locale
}
