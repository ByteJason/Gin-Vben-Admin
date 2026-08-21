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

type Event struct {
	ID        string
	ActorID   string
	Action    string
	Resource  string
	Outcome   string
	RequestID string
	Details   map[string]any
	CreatedAt time.Time
}

type Filter struct {
	ActorID   string
	Action    string
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

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Query(ctx context.Context, filter Filter) (Page, error) {
	if filter.Limit < 0 || filter.Offset < 0 || !filter.From.IsZero() && !filter.To.IsZero() && filter.From.After(filter.To) {
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
	events, err := s.repo.Query(ctx, filter)
	if err != nil {
		return Page{}, err
	}
	page := Page{Total: len(events), Limit: filter.Limit, Offset: filter.Offset}
	if filter.Offset >= len(events) {
		return page, nil
	}
	end := filter.Offset + filter.Limit
	if end > len(events) {
		end = len(events)
	}
	page.Items = make([]Event, 0, end-filter.Offset)
	for _, event := range events[filter.Offset:end] {
		page.Items = append(page.Items, redactEvent(event))
	}
	return page, nil
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

func redactEvent(event Event) Event {
	event = cloneEvent(event)
	event.Details = redactMap(event.Details)
	return event
}

func redactMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "authorization") || strings.Contains(lower, "api_key") {
			output[key] = "[REDACTED]"
			continue
		}
		switch nested := value.(type) {
		case map[string]any:
			output[key] = redactMap(nested)
		default:
			output[key] = value
		}
	}
	return output
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
