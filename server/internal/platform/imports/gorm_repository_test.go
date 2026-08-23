package importsplatform

import (
	"context"
	"errors"
	"testing"
	"time"

	importsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/imports"
)

func TestGORMRepositoryMapsJobWithoutPayloadOrSecrets(t *testing.T) {
	now := time.Date(2026, 8, 23, 1, 2, 3, 0, time.UTC)
	job := importsapp.Job{ID: "job-1", Kind: importsapp.JobKindImport, TenantID: "tenant-a", OrgID: "org-a", ActorID: "actor-a", PreviewID: "preview-1", QueueTaskID: "queue-1", IdempotencyKey: "key-1", Status: importsapp.JobSucceeded, TotalRows: 2, ProcessedRows: 2, ErrorCount: 0, CreatedAt: now, UpdatedAt: now}
	record := fromJob(job)
	got := toJob(record)
	if got != job {
		t.Fatalf("round trip got=%+v want=%+v", got, job)
	}
	if _, err := (*GORMRepository)(nil).Get(context.Background(), "id", "tenant", ""); !errors.Is(err, importsapp.ErrJobNotFound) {
		t.Fatalf("nil get err=%v", err)
	}
}
