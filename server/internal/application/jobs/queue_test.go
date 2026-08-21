package jobs

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryQueueIsIdempotentAndMovesFailedTaskToDLQ(t *testing.T) {
	queue := NewMemoryQueue(2)
	task := Task{Type: "email.send", PayloadVersion: 1, IdempotencyKey: "mail-1", Payload: []byte(`{"to":"u@example.com"}`)}
	first, err := queue.Enqueue(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	second, err := queue.Enqueue(context.Background(), task)
	if err != nil || first.ID != second.ID {
		t.Fatalf("idempotency first=%+v second=%+v err=%v", first, second, err)
	}
	if err := queue.Fail(context.Background(), first.ID, errors.New("provider down")); err != nil {
		t.Fatal(err)
	}
	if err := queue.Fail(context.Background(), first.ID, errors.New("provider down")); err != nil {
		t.Fatal(err)
	}
	failed, err := queue.Get(context.Background(), first.ID)
	if err != nil || failed.Status != StatusDeadLetter {
		t.Fatalf("failed=%+v err=%v", failed, err)
	}
}
