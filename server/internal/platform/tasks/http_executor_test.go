package tasksplatform

import (
	"context"
	"encoding/json"
	"errors"
	tasksapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/tasks"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func httpConfig(url string, allow bool, extra map[string]any) json.RawMessage {
	value := map[string]any{"url": url, "allowInternal": allow, "method": "GET"}
	for k, v := range extra {
		value[k] = v
	}
	raw, _ := json.Marshal(value)
	return raw
}
func TestHTTPExecutorBlocksPrivateByDefault(t *testing.T) {
	for _, target := range []string{"http://127.0.0.1/test", "http://[::1]/", "http://169.254.169.254/", "http://10.0.0.1/", "http://localhost/", "http://224.0.0.1/"} {
		_, err := NewHTTPExecutor(nil, true).Execute(context.Background(), httpConfig(target, false, nil))
		if !errors.Is(err, tasksapp.ErrSSRFBlocked) {
			t.Fatalf("%s: %v", target, err)
		}
	}
}
func TestHTTPExecutorUsesRealResponseAndRawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if string(data) != "hello=world" {
			t.Errorf("raw body=%s", data)
		}
		if r.Header.Get("X-Test") != "yes" {
			t.Error("header absent")
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"count":3,"token":"server-secret"}`))
	}))
	defer srv.Close()
	result, err := NewHTTPExecutor(nil, true).Execute(context.Background(), httpConfig(srv.URL, true, map[string]any{"method": "POST", "body": "hello=world", "headers": map[string]string{"X-Test": "yes"}}))
	if err != nil {
		t.Fatal(err)
	}
	if result.ResultSummary != "HTTP 201" || !strings.Contains(result.RedactedOutput, `"count":3`) || strings.Contains(result.RedactedOutput, "server-secret") {
		t.Fatalf("bad result %+v", result)
	}
}
func TestHTTPExecutorDoesNotTreatRedirectAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://169.254.169.254/latest/meta-data")
		w.WriteHeader(302)
	}))
	defer srv.Close()
	_, err := NewHTTPExecutor(nil, true).Execute(context.Background(), httpConfig(srv.URL, true, nil))
	if !errors.Is(err, ErrHTTPStatus) {
		t.Fatalf("redirect should fail without following: %v", err)
	}
}
func TestHTTPExecutorFailureTimeoutAndOutputBound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Second):
			}
		}
		w.WriteHeader(500)
		_, _ = w.Write([]byte(strings.Repeat("z", 128)))
	}))
	defer srv.Close()
	exec := NewHTTPExecutor(nil, true)
	exec.MaxBodyBytes = 16
	result, err := exec.Execute(context.Background(), httpConfig(srv.URL, true, nil))
	if !errors.Is(err, ErrHTTPStatus) || len(result.RedactedOutput) > 40 {
		t.Fatalf("%+v %v", result, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = exec.Execute(ctx, httpConfig(srv.URL+"/slow", true, nil))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout=%v", err)
	}
}
func TestHTTPExecutorRedactsEchoedRequestSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("echo " + r.Header.Get("Authorization")))
	}))
	defer srv.Close()
	result, err := NewHTTPExecutor(nil, true).Execute(context.Background(), httpConfig(srv.URL, true, map[string]any{"headers": map[string]string{"Authorization": "Bearer keep-private"}}))
	if err != nil || strings.Contains(result.RedactedOutput, "keep-private") {
		t.Fatalf("%+v %v", result, err)
	}
}
