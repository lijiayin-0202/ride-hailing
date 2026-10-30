package data

import (
	"context"
	"io"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/lijayin-0202/radish-ride-service/internal/conf"
)

func TestOrderDispatchOutboxDispatcherDisablesWithoutGatewayAddress(t *testing.T) {
	dispatcher := NewOrderDispatchOutboxDispatcher(&conf.Data{}, nil, log.NewStdLogger(io.Discard))
	if dispatcher.Enabled() {
		t.Fatal("dispatcher should be disabled without a module 2 gateway address")
	}
	if err := dispatcher.Start(context.Background()); err != nil {
		t.Fatalf("disabled Start() error = %v", err)
	}
	if err := dispatcher.Stop(context.Background()); err != nil {
		t.Fatalf("disabled Stop() error = %v", err)
	}
}
