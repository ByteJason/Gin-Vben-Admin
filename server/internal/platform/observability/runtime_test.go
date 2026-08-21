package observabilityplatform

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainobs "example.com/gin-vben-admin/server/internal/domain/observability"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
)

func TestRuntimeCollectsPrometheusMetricsOnlyWhenEnabled(t *testing.T) {
	runtime, err := NewRuntime(domainobs.Config{MetricsEnabled: true, MetricsEndpoint: "http://127.0.0.1:0/metrics"})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	runtime.RecordHTTP("GET", "/health/live", 200, 5*time.Millisecond)
	result := httptest.NewRecorder()
	runtime.ServeMetrics(result, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "app_http_requests_total") {
		t.Fatalf("metrics response = %d %q", result.Code, result.Body.String())
	}
}

func TestRuntimeExportsSampledOTLPSpanAndDoesNotBlockRequest(t *testing.T) {
	received := make(chan []byte, 1)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()
	runtime, err := NewRuntime(domainobs.Config{
		TracingEnabled: true,
		OTLPEndpoint:   collector.URL,
		OTLPProtocol:   "http/protobuf",
		SampleRate:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.RecordHTTP("GET", "/health/live", 200, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-received:
		if len(body) == 0 {
			t.Fatal("collector received an empty OTLP payload")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for OTLP payload")
	}
	_ = runtime.Close()
}

func TestRuntimeDisabledDoesNotCreateCollectors(t *testing.T) {
	runtime, err := NewRuntime(domainobs.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if runtime.CollectorCount() != 0 {
		t.Fatalf("CollectorCount() = %d, want 0", runtime.CollectorCount())
	}
	result := httptest.NewRecorder()
	runtime.ServeMetrics(result, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if result.Code != http.StatusNotFound {
		t.Fatalf("disabled metrics status = %d, want 404", result.Code)
	}
}

func TestRuntimeExporterFailureDegradesAndCountsDroppedSpan(t *testing.T) {
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "collector unavailable", http.StatusBadGateway)
	}))
	defer collector.Close()
	runtime, err := NewRuntime(domainobs.Config{
		MetricsEnabled:  true,
		MetricsEndpoint: "http://127.0.0.1:9090/metrics",
		TracingEnabled:  true,
		OTLPEndpoint:    collector.URL,
		OTLPProtocol:    "http/protobuf",
		SampleRate:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	runtime.RecordHTTP("GET", "/health/live", http.StatusOK, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Flush(ctx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	result := httptest.NewRecorder()
	runtime.ServeMetrics(result, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "app_observability_export_dropped_total 1") {
		t.Fatalf("degradation metrics = %d %q", result.Code, result.Body.String())
	}
}

type grpcTraceCollector struct {
	collectortrace.UnimplementedTraceServiceServer
	received chan *collectortrace.ExportTraceServiceRequest
}

func (c *grpcTraceCollector) Export(_ context.Context, request *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	c.received <- request
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func TestRuntimeExportsSampledOTLPSpanOverGRPC(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	collector := &grpcTraceCollector{received: make(chan *collectortrace.ExportTraceServiceRequest, 1)}
	collectortrace.RegisterTraceServiceServer(server, collector)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	runtime, err := NewRuntime(domainobs.Config{
		TracingEnabled: true,
		OTLPEndpoint:   "http://" + listener.Addr().String(),
		OTLPProtocol:   "grpc",
		SampleRate:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	runtime.RecordHTTP("GET", "/health/live", http.StatusOK, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-collector.received:
		if len(request.ResourceSpans) != 1 {
			t.Fatalf("resource spans = %d, want 1", len(request.ResourceSpans))
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for gRPC OTLP payload")
	}
}
