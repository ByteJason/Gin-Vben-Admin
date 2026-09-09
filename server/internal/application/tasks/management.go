package tasks

import (
	"context"
	"encoding/json"
	"errors"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/task"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"net/url"
	"sort"
	"strings"
	"time"
)

var ErrInvalidDefinition = errors.New("task configuration is invalid")

func (s *Service) SetMethodExecutor(reg *MethodExecutor) { s.methods = reg }
func (s *Service) Methods() []string {
	if s == nil {
		return []string{}
	}
	return s.methods.Keys()
}
func (s *Service) normalize(d *TaskDefinition) error {
	d.Name = strings.TrimSpace(d.Name)
	d.Description = strings.TrimSpace(d.Description)
	if len(d.Name) > 160 || len(d.Description) > 2000 || d.TimeoutSeconds > 3600 || d.MaxAttempts > 10 || d.Concurrency > 100 {
		return ErrInvalidDefinition
	}
	if d.ExecutorType == "" {
		d.ExecutorType = "registered"
		if d.Type == "http" || d.Type == "webhook" {
			d.ExecutorType = "http"
		}
	}
	if d.ExecutorType != "http" && d.ExecutorType != "registered" {
		return ErrInvalidDefinition
	}
	if len(d.PayloadSchema) == 0 {
		d.PayloadSchema = json.RawMessage(`{}`)
	}
	if len(d.Payload) == 0 {
		d.Payload = json.RawMessage(`{}`)
	}
	if !isJSONObject(d.Payload) || len(d.Payload) > 1<<20 {
		return ErrInvalidRunPayload
	}
	if d.ExecutorType == "registered" {
		d.Type = "manual"
		d.HTTPConfig = nil
		if s.methods != nil && !s.methods.Has(d.MethodKey) {
			return ErrExecutorNotRegistered
		}
	} else {
		if d.Type != "webhook" {
			d.Type = "http"
		}
		d.MethodKey = ""
		var config struct {
			URL     string            `json:"url"`
			Method  string            `json:"method"`
			Headers map[string]string `json:"headers"`
		}
		if json.Unmarshal(d.HTTPConfig, &config) != nil || len(d.HTTPConfig) > 1<<20 {
			return ErrInvalidHTTPPayload
		}
		u, err := url.Parse(config.URL)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return ErrInvalidHTTPPayload
		}
		switch strings.ToUpper(config.Method) {
		case "", "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		default:
			return ErrInvalidHTTPPayload
		}
		for k, v := range config.Headers {
			if strings.ContainsAny(k+v, "\r\n") {
				return ErrInvalidHTTPPayload
			}
		}
	}
	return nil
}

// PublicDefinition masks opaque HTTP bodies and credential-bearing URLs as
// whole values so the edit placeholder can round-trip without exposing secrets.
func PublicDefinition(d TaskDefinition) TaskDefinition {
	d = cloneDefinition(d)
	if len(d.HTTPConfig) > 0 {
		var config map[string]any
		if json.Unmarshal(d.HTTPConfig, &config) == nil {
			redactValue(config)
			if body, ok := config["body"]; ok && body != nil && body != "" {
				config["body"] = "[REDACTED]"
			}
			if raw, ok := config["url"].(string); ok {
				if u, err := url.Parse(raw); err == nil {
					for key := range u.Query() {
						if SensitiveKey(key) {
							config["url"] = "[REDACTED]"
							break
						}
					}
				}
			}
			d.HTTPConfig, _ = json.Marshal(config)
		}
	}
	var payload any
	if json.Unmarshal(d.Payload, &payload) == nil {
		redactValue(payload)
		d.Payload, _ = json.Marshal(payload)
	}
	return d
}
func mergeRedacted(in, old json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return old
	}
	var next, previous any
	if json.Unmarshal(in, &next) != nil || json.Unmarshal(old, &previous) != nil {
		return in
	}
	var merge func(any, any) any
	merge = func(n, p any) any {
		if v, ok := n.(string); ok && v == "[REDACTED]" {
			return p
		}
		if nm, ok := n.(map[string]any); ok {
			pm, _ := p.(map[string]any)
			for k, v := range nm {
				nm[k] = merge(v, pm[k])
			}
		}
		return n
	}
	out, _ := json.Marshal(merge(next, previous))
	return out
}

type DefinitionQuery struct {
	Page, PageSize     int
	Name, ExecutorType string
	Enabled            *bool
}
type DefinitionPage struct {
	Items    []TaskDefinition `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
}

func pageBounds(page, size, total int) (int, int, int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	return page, size, start, end
}
func (s *Service) ListPage(ctx context.Context, q DefinitionQuery) (DefinitionPage, error) {
	items, err := s.List(ctx)
	if err != nil {
		return DefinitionPage{}, err
	}
	latest := map[string]TaskRun{}
	if s.runs != nil {
		scope, scopeErr := tenant.RequireContext(ctx)
		if scopeErr != nil {
			return DefinitionPage{}, scopeErr
		}
		history, historyErr := s.runs.List(ctx, "", scope.TenantID, scope.Organization)
		if historyErr != nil {
			return DefinitionPage{}, historyErr
		}
		for _, r := range history {
			if prev, ok := latest[r.TaskID]; !ok || r.CreatedAt.After(prev.CreatedAt) {
				latest[r.TaskID] = r
			}
		}
	}
	filtered := []TaskDefinition{}
	for _, d := range items {
		if q.Name != "" && !strings.Contains(strings.ToLower(d.Name), strings.ToLower(q.Name)) {
			continue
		}
		executor := d.ExecutorType
		if executor == "" {
			executor = "registered"
			if d.Type != "manual" {
				executor = "http"
			}
		}
		d.ExecutorType = executor
		if q.ExecutorType != "" && q.ExecutorType != executor || q.Enabled != nil && d.Enabled != *q.Enabled {
			continue
		}
		if d.Enabled && d.Cron != "" {
			if dates, err := domain.NextExecutions(d.Cron, d.Timezone, s.clock(), 1); err == nil && len(dates) > 0 {
				d.NextRunAt = &dates[0]
			}
		}
		if last, ok := latest[d.ID]; ok {
			d.LastRunAt = &last.CreatedAt
			d.LastRunStatus = string(last.Status)
			d.LastRunErrorCode = last.ErrorCode
		}
		filtered = append(filtered, d)
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].CreatedAt.After(filtered[j].CreatedAt) })
	page, size, start, end := pageBounds(q.Page, q.PageSize, len(filtered))
	return DefinitionPage{Items: filtered[start:end], Total: len(filtered), Page: page, PageSize: size}, nil
}

type RunQuery struct {
	Page, PageSize                          int
	TaskID, TaskName, Status, TriggerSource string
	From, To                                *time.Time
}
type RunPage struct {
	Items    []TaskRun `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
}

func matchesRunStatus(r TaskRun, status string) bool {
	switch status {
	case "":
		return true
	case "skipped":
		return r.ErrorCode == "concurrency.skipped"
	case "timeout":
		return r.ErrorCode == "worker.timeout"
	default:
		return string(r.Status) == status
	}
}
func (s *RunService) ListPage(ctx context.Context, q RunQuery) (RunPage, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return RunPage{}, err
	}
	if s == nil || s.repo == nil {
		return RunPage{}, ErrRunQueueUnavailable
	}
	items, err := s.repo.List(ctx, q.TaskID, scope.TenantID, scope.Organization)
	if err != nil {
		return RunPage{}, err
	}
	definitions, _ := s.definitions.List(ctx)
	names := map[string]TaskDefinition{}
	for _, d := range definitions {
		names[d.ID] = d
	}
	filtered := []TaskRun{}
	for _, r := range items {
		if r.TaskName == "" {
			r.TaskName = names[r.TaskID].Name
			r.TaskDescription = names[r.TaskID].Description
		}
		if q.TaskName != "" && !strings.Contains(strings.ToLower(r.TaskName), strings.ToLower(q.TaskName)) {
			continue
		}
		if !matchesRunStatus(r, q.Status) || q.TriggerSource != "" && r.TriggerSource != q.TriggerSource || q.From != nil && r.CreatedAt.Before(*q.From) || q.To != nil && r.CreatedAt.After(*q.To) {
			continue
		}
		filtered = append(filtered, r)
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].CreatedAt.After(filtered[j].CreatedAt) })
	page, size, start, end := pageBounds(q.Page, q.PageSize, len(filtered))
	return RunPage{Items: filtered[start:end], Total: len(filtered), Page: page, PageSize: size}, nil
}
