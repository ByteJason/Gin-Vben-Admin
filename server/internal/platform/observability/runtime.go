package observabilityplatform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	domainobs "example.com/gin-vben-admin/server/internal/domain/observability"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

const (
	defaultServiceName = "gin-vben-admin"
	spanQueueSize      = 256
)

// Runtime is the B8 runtime collector. Metrics are exposed in Prometheus text
// format for an external scraper; sampled spans are sent as OTLP HTTP/protobuf
// to an externally configured Collector. Neither backend is bundled.
type Runtime struct {
	config      domainobs.Config
	metrics     *metricsRegistry
	exporter    *otlpExporter
	queue       chan spanRecord
	queueMu     sync.Mutex
	closed      bool
	pendingMu   sync.Mutex
	pending     int
	pendingDone chan struct{}
	workers     sync.WaitGroup
	closeOnce   sync.Once
	dropped     atomic.Uint64
}

type metricKey struct {
	method string
	route  string
	status int
}

type metricValue struct {
	count uint64
	sum   float64
}

type metricsRegistry struct {
	mu       sync.RWMutex
	requests map[metricKey]metricValue
}

type spanRecord struct {
	traceID   []byte
	spanID    []byte
	name      string
	method    string
	route     string
	status    int
	requestID string
	start     time.Time
	end       time.Time
}

type otlpExporter struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

// NewRuntime validates the B6 configuration and constructs only the enabled
// collectors. It never performs a network request.
func NewRuntime(config domainobs.Config) (*Runtime, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	runtime := &Runtime{config: config, pendingDone: closedSignal()}
	if config.MetricsEnabled {
		runtime.metrics = &metricsRegistry{requests: make(map[metricKey]metricValue)}
	}
	if config.TracingEnabled {
		if config.OTLPProtocol != "http/protobuf" {
			// gRPC remains a configuration-compatible seam; the B8 runtime
			// intentionally reports it as unavailable instead of silently
			// pretending to export spans.
			return nil, fmt.Errorf("otlp protocol %q requires the gRPC exporter", config.OTLPProtocol)
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !config.TLSVerify} // #nosec G402 -- explicit user setting
		runtime.exporter = &otlpExporter{
			endpoint: normalizeOTLPEndpoint(config.OTLPEndpoint),
			apiKey:   config.OTLPAPIKey,
			client:   &http.Client{Transport: transport, Timeout: 5 * time.Second},
		}
		runtime.queue = make(chan spanRecord, spanQueueSize)
		runtime.workers.Add(1)
		go runtime.exportLoop()
	}
	return runtime, nil
}

func (r *Runtime) CollectorCount() int {
	if r == nil {
		return 0
	}
	count := 0
	if r.metrics != nil {
		count++
	}
	if r.exporter != nil {
		count++
	}
	return count
}

// RecordHTTP records one completed request. It is safe to call from a Gin
// middleware and never performs network I/O on the request goroutine.
func (r *Runtime) RecordHTTP(method, route string, status int, duration time.Duration, requestID ...string) {
	if r == nil {
		return
	}
	method = boundedLabel(method, 32)
	route = boundedLabel(route, 160)
	if route == "" {
		route = "unknown"
	}
	if r.metrics != nil {
		r.metrics.record(metricKey{method: method, route: route, status: status}, duration)
	}
	if r.exporter == nil || !r.sample() {
		return
	}
	id := ""
	if len(requestID) > 0 {
		id = requestID[0]
	}
	traceID, spanID := correlationIDs(id, method, route, time.Now())
	span := spanRecord{
		traceID: traceID, spanID: spanID, name: method + " " + route,
		method: method, route: route, status: status, requestID: boundedLabel(id, 128),
		start: time.Now().Add(-duration), end: time.Now(),
	}
	r.queueMu.Lock()
	if r.closed {
		r.queueMu.Unlock()
		return
	}
	r.pendingMu.Lock()
	if r.pending == 0 {
		r.pendingDone = make(chan struct{})
	}
	r.pending++
	r.pendingMu.Unlock()
	select {
	case r.queue <- span:
	default:
		r.completePending()
		r.dropped.Add(1)
	}
	r.queueMu.Unlock()
}

func (r *Runtime) sample() bool {
	return r.config.SampleRate >= 1 || (r.config.SampleRate > 0 && rand.Float64() < r.config.SampleRate)
}

// ServeMetrics serves Prometheus exposition text. Disabled metrics return
// 404 so an accidental public scrape does not imply collection is active.
func (r *Runtime) ServeMetrics(w http.ResponseWriter, req *http.Request) {
	if r == nil || r.metrics == nil {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(r.metrics.render(r.dropped.Load())))
}

func (r *Runtime) Flush(ctx context.Context) error {
	if r == nil || r.exporter == nil {
		return nil
	}
	for {
		r.pendingMu.Lock()
		pending := r.pending
		done := r.pendingDone
		r.pendingMu.Unlock()
		if pending == 0 {
			return nil
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		if r.queue != nil {
			r.queueMu.Lock()
			r.closed = true
			close(r.queue)
			r.queueMu.Unlock()
			r.workers.Wait()
		}
	})
	return nil
}

func (r *Runtime) exportLoop() {
	defer r.workers.Done()
	for span := range r.queue {
		if err := r.exporter.Export(context.Background(), span); err != nil {
			r.dropped.Add(1)
		}
		r.completePending()
	}
}

func (r *Runtime) completePending() {
	r.pendingMu.Lock()
	if r.pending > 0 {
		r.pending--
		if r.pending == 0 {
			close(r.pendingDone)
		}
	}
	r.pendingMu.Unlock()
}

func (m *metricsRegistry) record(key metricKey, duration time.Duration) {
	m.mu.Lock()
	value := m.requests[key]
	value.count++
	value.sum += duration.Seconds()
	m.requests[key] = value
	m.mu.Unlock()
}

func (m *metricsRegistry) render(dropped uint64) string {
	m.mu.RLock()
	keys := make([]metricKey, 0, len(m.requests))
	for key := range m.requests {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		return keys[i].method < keys[j].method
	})
	var b strings.Builder
	b.WriteString("# HELP app_http_requests_total Total HTTP requests.\n# TYPE app_http_requests_total counter\n")
	b.WriteString("# HELP app_http_request_duration_seconds HTTP request duration.\n# TYPE app_http_request_duration_seconds summary\n")
	for _, key := range keys {
		value := m.requests[key]
		labels := fmt.Sprintf(`method="%s",route="%s",status="%d"`, escapeLabel(key.method), escapeLabel(key.route), key.status)
		b.WriteString("app_http_requests_total{" + labels + "} " + strconv.FormatUint(value.count, 10) + "\n")
		b.WriteString("app_http_request_duration_seconds_sum{" + labels + "} " + strconv.FormatFloat(value.sum, 'f', 6, 64) + "\n")
		b.WriteString("app_http_request_duration_seconds_count{" + labels + "} " + strconv.FormatUint(value.count, 10) + "\n")
	}
	m.mu.RUnlock()
	b.WriteString("# HELP app_observability_export_dropped_total Spans dropped because the exporter failed or queue was full.\n# TYPE app_observability_export_dropped_total counter\n")
	b.WriteString("app_observability_export_dropped_total " + strconv.FormatUint(dropped, 10) + "\n")
	return b.String()
}

func (e *otlpExporter) Export(ctx context.Context, span spanRecord) error {
	if e == nil || e.client == nil {
		return errors.New("otlp exporter is not configured")
	}
	payload := &collectortrace.ExportTraceServiceRequest{ResourceSpans: []*tracev1.ResourceSpans{{
		Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{stringAttribute("service.name", defaultServiceName)}},
		ScopeSpans: []*tracev1.ScopeSpans{{Spans: []*tracev1.Span{{
			TraceId: span.traceID, SpanId: span.spanID, Name: span.name,
			Kind:              tracev1.Span_SPAN_KIND_SERVER,
			StartTimeUnixNano: uint64(span.start.UnixNano()), EndTimeUnixNano: uint64(span.end.UnixNano()),
			Attributes: []*commonv1.KeyValue{
				stringAttribute("http.request.method", span.method),
				stringAttribute("http.route", span.route),
				stringAttribute("http.request_id", span.requestID),
				intAttribute("http.response.status_code", int64(span.status)),
			},
			Status: spanStatus(span.status),
		}}}},
	}}}
	body, err := proto.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("otlp collector returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func stringAttribute(key, value string) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}}}
}

func intAttribute(key string, value int64) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: value}}}
}

func spanStatus(status int) *tracev1.Status {
	code := tracev1.Status_STATUS_CODE_OK
	if status >= 500 {
		code = tracev1.Status_STATUS_CODE_ERROR
	}
	return &tracev1.Status{Code: code}
}

func normalizeOTLPEndpoint(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if strings.HasSuffix(value, "/v1/traces") {
		return value
	}
	return value + "/v1/traces"
}

func correlationIDs(requestID, method, route string, now time.Time) ([]byte, []byte) {
	digest := sha256.Sum256([]byte(requestID + "|" + method + "|" + route + "|" + now.UTC().Format(time.RFC3339Nano)))
	traceID := append([]byte(nil), digest[:16]...)
	spanID := append([]byte(nil), digest[16:24]...)
	return traceID, spanID
}

func boundedLabel(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return strings.ReplaceAll(value, "\n", `\n`)
}

func closedSignal() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}
