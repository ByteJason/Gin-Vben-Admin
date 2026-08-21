package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestObservabilityRecordsNormalizedRouteAndRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &recordingStub{}
	router := gin.New()
	router.Use(RequestID(), Observability(recorder))
	router.GET("/users/:id", func(c *gin.Context) { c.Status(http.StatusCreated) })

	request := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	request.Header.Set(RequestIDHeader, "req-observe-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusCreated)
	}
	if recorder.route != "/users/:id" || recorder.method != http.MethodGet || recorder.status != http.StatusCreated || recorder.requestID != "req-observe-1" || recorder.duration <= 0 {
		t.Fatalf("recorded=%+v", recorder)
	}
}

type recordingStub struct {
	method    string
	route     string
	status    int
	duration  time.Duration
	requestID string
}

func (s *recordingStub) RecordHTTP(method, route string, status int, duration time.Duration, requestID ...string) {
	s.method, s.route, s.status, s.duration = method, route, status, duration
	if len(requestID) > 0 {
		s.requestID = requestID[0]
	}
}
