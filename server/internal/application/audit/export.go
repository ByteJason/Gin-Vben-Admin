package audit

// RetentionDryRun is the read-only retention preview exposed alongside
// exports; its implementation lives with the query service.

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type ExportFormat string

const (
	ExportFormatCSV  ExportFormat = "csv"
	ExportFormatJSON ExportFormat = "json"
)

var ErrInvalidExportFormat = errors.New("invalid audit export format")

type ExportResult struct {
	ContentType string
	Data        []byte
	Filename    string
}

// Export returns a redacted, bounded snapshot suitable for a browser
// download. It never mutates the repository or emits raw credential fields.
func (s *Service) Export(ctx context.Context, filter Filter, format ExportFormat) (ExportResult, error) {
	if format != ExportFormatCSV && format != ExportFormatJSON {
		return ExportResult{}, ErrInvalidExportFormat
	}
	if s == nil || s.repo == nil {
		return ExportResult{}, errors.New("audit repository unavailable")
	}
	filter.Offset = 0
	if filter.Limit == 0 || filter.Limit > 10_000 {
		filter.Limit = 10_000
	}
	if filter.Limit < 0 || !filter.Category.Valid() {
		return ExportResult{}, ErrInvalidFilter
	}
	var (
		events []Event
		err    error
	)
	if paged, ok := s.repo.(PageRepository); ok {
		events, _, err = paged.QueryPage(ctx, filter)
	} else {
		events, err = s.repo.Query(ctx, filter)
	}
	if err != nil {
		return ExportResult{}, err
	}
	for index := range events {
		events[index] = normalizeEvent(events[index])
	}
	if format == ExportFormatJSON {
		data, err := json.Marshal(events)
		if err != nil {
			return ExportResult{}, err
		}
		return ExportResult{ContentType: "application/json; charset=utf-8", Data: data, Filename: "audit-events.json"}, nil
	}
	data, err := marshalCSV(events)
	if err != nil {
		return ExportResult{}, err
	}
	return ExportResult{ContentType: "text/csv; charset=utf-8", Data: data, Filename: "audit-events.csv"}, nil
}

func marshalCSV(events []Event) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if err := writer.Write([]string{"id", "category", "actorId", "resource", "action", "outcome", "requestId", "createdAt", "details"}); err != nil {
		return nil, err
	}
	for _, event := range events {
		details, err := json.Marshal(event.Details)
		if err != nil {
			return nil, err
		}
		if err := writer.Write([]string{
			event.ID,
			string(event.Category),
			event.ActorID,
			event.Resource,
			event.Action,
			event.Outcome,
			event.RequestID,
			event.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999Z07:00"),
			string(details),
		}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Sink is the provider contract for console/file and future Loki/ELK
// adapters. Implementations receive an event and must preserve redaction.
type Sink interface {
	Write(context.Context, Event) error
}

type ConsoleSink struct {
	mu     sync.Mutex
	writer io.Writer
}

func NewConsoleSink(writer io.Writer) *ConsoleSink {
	if writer == nil {
		writer = io.Discard
	}
	return &ConsoleSink{writer: writer}
}

func (s *ConsoleSink) Write(_ context.Context, event Event) error {
	if s == nil || s.writer == nil {
		return errors.New("console sink unavailable")
	}
	data, err := json.Marshal(normalizeEvent(event))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = fmt.Fprintf(s.writer, "%s\n", data)
	return err
}

type FileSink struct {
	mu   sync.Mutex
	path string
}

func NewFileSink(path string) (*FileSink, error) {
	if path == "" || filepath.Base(path) == "." {
		return nil, errors.New("invalid audit sink path")
	}
	return &FileSink{path: path}, nil
}

func (s *FileSink) Write(_ context.Context, event Event) error {
	if s == nil || s.path == "" {
		return errors.New("file sink unavailable")
	}
	data, err := json.Marshal(normalizeEvent(event))
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	_, err = fmt.Fprintf(file, "%s\n", data)
	return err
}

type MultiSink struct{ sinks []Sink }

func NewMultiSink(sinks ...Sink) *MultiSink { return &MultiSink{sinks: sinks} }

func (s *MultiSink) Write(ctx context.Context, event Event) error {
	if s == nil {
		return errors.New("audit sink unavailable")
	}
	for _, sink := range s.sinks {
		if sink == nil {
			continue
		}
		if err := sink.Write(ctx, event); err != nil {
			return err
		}
	}
	return nil
}
