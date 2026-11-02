package data

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

func TestDispatchReassignmentDispatcherStartsAndStops(t *testing.T) {
	processor := &blockingReassignmentProcessor{started: make(chan struct{})}
	dispatcher := newDispatchReassignmentDispatcher(processor, nil, time.Hour, log.NewStdLogger(io.Discard))
	if err := dispatcher.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-processor.started:
	case <-time.After(time.Second):
		t.Fatal("reassignment worker did not process an iteration")
	}
	if err := dispatcher.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

type blockingReassignmentProcessor struct {
	started chan struct{}
}

func (processor *blockingReassignmentProcessor) ProcessReassignmentOnce(ctx context.Context, _ ReassignmentApplication, _ time.Time, _ time.Duration) (bool, error) {
	select {
	case <-processor.started:
	default:
		close(processor.started)
	}
	<-ctx.Done()
	return false, nil
}
