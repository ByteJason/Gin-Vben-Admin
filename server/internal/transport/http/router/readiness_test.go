package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	platformhealth "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/health"
)

type readinessDependency struct {
	err error
}

func (readinessDependency) Name() string { return "database" }

func (d readinessDependency) Ping(context.Context) error { return d.err }

func TestReadinessReflectsDependenciesWithoutAffectingLiveness(t *testing.T) {
	checker := platformhealth.NewChecker(time.Second, readinessDependency{err: errors.New("offline")})
	r := NewRouter(checker)

	live := httptest.NewRecorder()
	r.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if live.Code != http.StatusOK {
		t.Fatalf("liveness status = %d, want %d", live.Code, http.StatusOK)
	}

	readyRequest := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	readyRequest.Header.Set("X-Request-ID", "REQ-ready-down")
	ready := httptest.NewRecorder()
	r.ServeHTTP(ready, readyRequest)
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want %d; body=%s", ready.Code, http.StatusServiceUnavailable, ready.Body.String())
	}

	var body struct {
		Code    int    `json:"code"`
		TraceID string `json:"traceId"`
		Data    struct {
			Status string            `json:"status"`
			Checks map[string]string `json:"checks"`
		} `json:"data"`
	}
	if err := json.NewDecoder(ready.Body).Decode(&body); err != nil {
		t.Fatalf("decode readiness response: %v", err)
	}
	if body.Code != 40001 || body.TraceID != "REQ-ready-down" {
		t.Fatalf("unexpected dependency error: %+v", body)
	}
	if body.Data.Status != "unavailable" || body.Data.Checks["database"] != platformhealth.StatusDown {
		t.Fatalf("unexpected safe dependency details: %+v", body.Data)
	}
}

func TestReadinessReturnsDependencyStatusesWhenReady(t *testing.T) {
	checker := platformhealth.NewChecker(time.Second, readinessDependency{})
	r := NewRouter(checker)

	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("readiness status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}

	var body struct {
		Data struct {
			Status string            `json:"status"`
			Checks map[string]string `json:"checks"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode readiness response: %v", err)
	}
	if body.Data.Status != "ready" || body.Data.Checks["database"] != platformhealth.StatusUp {
		t.Fatalf("unexpected readiness data: %+v", body.Data)
	}
}
