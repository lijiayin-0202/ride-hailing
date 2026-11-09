package biz

import (
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestPendingCommandsCreatesTraceSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(t.Context())
	})

	repo := &fakeVehicleHMIRepo{command: &VehicleCommand{CommandNo: "cmd-1"}}
	uc := NewVehicleHMIUsecase(repo, log.DefaultLogger)
	if _, err := uc.PendingCommands(t.Context(), 1, ""); err != nil {
		t.Fatalf("PendingCommands() error = %v", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "vehicle_hmi.pending_commands" {
		t.Fatalf("ended spans = %v", spans)
	}
}
