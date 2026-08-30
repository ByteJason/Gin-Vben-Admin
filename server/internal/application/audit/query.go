// Package audit exposes a storage-neutral, paginated audit query seam. Writers
// may remain in authentication/settings modules; this package owns read-side
// filtering and redaction so a future UI cannot accidentally expose secrets.
package audit

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrInvalidFilter = errors.New("invalid audit filter")

// Category is the stable audit taxonomy exposed to administrators. Writers
// may persist an event type such as auth.login or settings.update; the read
// side normalizes those prefixes into one of the three public categories.
type Category string

const (
	CategoryLogin     Category = "login"
	CategoryOperation Category = "operation"
	CategorySystem    Category = "system"
)

func (c Category) Valid() bool {
	return c == "" || c == CategoryLogin || c == CategoryOperation || c == CategorySystem
}

// Classify maps persisted resource names to the stable taxonomy. Unknown
// resources are ordinary management operations rather than system events.
func Classify(resource, _ string) Category {
	switch strings.ToLower(strings.TrimSpace(resource)) {
	case "auth", "authentication", "login":
		return CategoryLogin
	case "system", "runtime", "health", "observability":
		return CategorySystem
	default:
		return CategoryOperation
	}
}

type Event struct {
	Category  Category       `json:"category"`
	ID        string         `json:"id"`
	ActorID   string         `json:"actorId"`
	Action    string         `json:"action"`
	Resource  string         `json:"resource"`
	Outcome   string         `json:"outcome"`
	RequestID string         `json:"requestId"`
	Details   map[string]any `json:"details,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type Filter struct {
	ActorID   string
	Action    string
	Category  Category
	Resource  string
	Outcome   string
	RequestID string
	From      time.Time
	To        time.Time
	Limit     int
	Offset    int
}

type Page struct {
	Items  []Event `json:"items"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

type Repository interface {
	Query(context.Context, Filter) ([]Event, error)
}

// PageRepository lets database adapters return a total without loading every
// matching row. MemoryRepository intentionally uses the simpler Repository
// contract for deterministic unit tests.
type PageRepository interface {
	QueryPage(context.Context, Filter) ([]Event, int, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Query(ctx context.Context, filter Filter) (Page, error) {
	if filter.Limit < 0 || filter.Offset < 0 || !filter.Category.Valid() || !filter.From.IsZero() && !filter.To.IsZero() && filter.From.After(filter.To) {
		return Page{}, ErrInvalidFilter
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	if s == nil || s.repo == nil {
		return Page{}, errors.New("audit repository unavailable")
	}
	var (
		events []Event
		total  int
		err    error
	)
	if paged, ok := s.repo.(PageRepository); ok {
		// PageRepository implementations apply offset/limit in the storage
		// query. Do not slice the returned page a second time; doing so would
		// skip rows whenever an administrator moves past the first page.
		events, total, err = paged.QueryPage(ctx, filter)
		if err != nil {
			return Page{}, err
		}
		page := Page{Total: total, Limit: filter.Limit, Offset: filter.Offset, Items: make([]Event, 0, len(events))}
		for _, event := range events {
			page.Items = append(page.Items, normalizeEvent(event))
		}
		return page, nil
	} else {
		events, err = s.repo.Query(ctx, filter)
		total = len(events)
	}
	if err != nil {
		return Page{}, err
	}
	page := Page{Total: total, Limit: filter.Limit, Offset: filter.Offset}
	if filter.Offset >= len(events) {
		return page, nil
	}
	end := filter.Offset + filter.Limit
	if end > len(events) {
		end = len(events)
	}
	page.Items = make([]Event, 0, end-filter.Offset)
	for _, event := range events[filter.Offset:end] {
		page.Items = append(page.Items, normalizeEvent(event))
	}
	return page, nil
}

// QueryLoginEvents returns only authentication login events for one actor.
// Keeping the event type fixed here prevents a user-management caller from
// widening the seam into arbitrary audit access while preserving the shared
// pagination, tenant scope and redaction behavior of Query.
func (s *Service) QueryLoginEvents(ctx context.Context, actorID string, filter Filter) (Page, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return Page{}, ErrInvalidFilter
	}
	filter.ActorID = actorID
	filter.Action = "login"
	filter.Resource = "auth"
	filter.Category = CategoryLogin
	return s.Query(ctx, filter)
}

// RetentionReport describes what a retention policy would affect. It is
// deliberately read-only; callers must use an explicit, separately reviewed
// deletion workflow for any physical cleanup.
type RetentionReport struct {
	Cutoff        time.Time `json:"cutoff"`
	MatchingCount int       `json:"matchingCount"`
	RetentionDays int       `json:"retentionDays"`
}

type BeforeCounter interface {
	CountBefore(context.Context, time.Time) (int, error)
}

func (s *Service) RetentionDryRun(ctx context.Context, now time.Time, days int) (RetentionReport, error) {
	if days <= 0 || days > 3650 || s == nil || s.repo == nil {
		return RetentionReport{}, ErrInvalidFilter
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	count := 0
	var err error
	if counter, ok := s.repo.(BeforeCounter); ok {
		count, err = counter.CountBefore(ctx, cutoff)
	} else {
		var events []Event
		events, err = s.repo.Query(ctx, Filter{To: cutoff, Limit: 10_000})
		count = len(events)
	}
	if err != nil {
		return RetentionReport{}, err
	}
	return RetentionReport{Cutoff: cutoff, MatchingCount: count, RetentionDays: days}, nil
}

// MemoryRepository is deterministic and intended for unit tests/local seams.
type MemoryRepository struct {
	mu     sync.RWMutex
	events []Event
}

func NewMemoryRepository(events []Event) *MemoryRepository {
	copyEvents := make([]Event, len(events))
	for i, event := range events {
		copyEvents[i] = cloneEvent(event)
	}
	return &MemoryRepository{events: copyEvents}
}

func (r *MemoryRepository) Query(_ context.Context, filter Filter) ([]Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	filtered := make([]Event, 0, len(r.events))
	for _, event := range r.events {
		if filter.ActorID != "" && event.ActorID != filter.ActorID ||
			filter.Action != "" && event.Action != filter.Action ||
			filter.Category != "" && normalizeCategory(event) != filter.Category ||
			filter.Resource != "" && event.Resource != filter.Resource ||
			filter.Outcome != "" && event.Outcome != filter.Outcome ||
			filter.RequestID != "" && event.RequestID != filter.RequestID ||
			!filter.From.IsZero() && event.CreatedAt.Before(filter.From) ||
			!filter.To.IsZero() && event.CreatedAt.After(filter.To) {
			continue
		}
		filtered = append(filtered, cloneEvent(event))
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].CreatedAt.After(filtered[j].CreatedAt) })
	return filtered, nil
}

func (r *MemoryRepository) CountBefore(_ context.Context, cutoff time.Time) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, event := range r.events {
		if event.CreatedAt.Before(cutoff) {
			count++
		}
	}
	return count, nil
}

func redactEvent(event Event) Event {
	event = cloneEvent(event)
	event.Category = normalizeCategory(event)
	event.Details = redactMap(event.Details)
	return event
}

func normalizeCategory(event Event) Category {
	if event.Category.Valid() && event.Category != "" {
		return event.Category
	}
	return Classify(event.Resource, event.Action)
}

func normalizeEvent(event Event) Event { return redactEvent(event) }

func redactMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		lower := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "authorization") || strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") {
			output[key] = "[REDACTED]"
			continue
		}
		// Device identifiers and browser fingerprints are useful for correlating
		// attempts, but the default operations view must not expose the raw
		// stable identifier. Keep the key so the UI can label the field while
		// replacing its value at the read boundary.
		if strings.Contains(lower, "fingerprint") || lower == "device_id" || lower == "deviceid" {
			output[key] = maskIdentifier(value)
			continue
		}
		switch nested := value.(type) {
		case map[string]any:
			output[key] = redactMap(nested)
		case []any:
			output[key] = redactSlice(nested)
		default:
			output[key] = value
		}
	}
	return output
}

func redactSlice(input []any) []any {
	if input == nil {
		return nil
	}
	output := make([]any, len(input))
	for index, value := range input {
		switch nested := value.(type) {
		case map[string]any:
			output[index] = redactMap(nested)
		case []any:
			output[index] = redactSlice(nested)
		default:
			output[index] = value
		}
	}
	return output
}

func maskIdentifier(value any) string {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "[MASKED]"
	}
	text = strings.TrimSpace(text)
	if len(text) <= 8 {
		return "[MASKED]"
	}
	return text[:4] + "…" + text[len(text)-4:]
}

func cloneEvent(event Event) Event {
	event.Details = redactCopy(event.Details)
	return event
}

func redactCopy(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		if nested, ok := value.(map[string]any); ok {
			output[key] = redactCopy(nested)
		} else {
			output[key] = value
		}
	}
	return output
}
