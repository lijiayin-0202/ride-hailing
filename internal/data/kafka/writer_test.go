package kafka

import (
	"testing"
	"time"
)

func TestNewWriterUsesLowLatencyBatchTimeout(t *testing.T) {
	writer := NewWriter([]string{"127.0.0.1:9092"})
	if writer == nil {
		t.Fatal("expected writer")
	}
	defer writer.Close()

	if expected, actual := 10*time.Millisecond, writer.client.BatchTimeout; expected != actual {
		t.Fatalf("expected batch timeout %s, got %s", expected, actual)
	}
}

func TestNewWriterWithoutBrokersReturnsNil(t *testing.T) {
	if writer := NewWriter(nil); writer != nil {
		t.Fatal("expected nil writer without brokers")
	}
}
