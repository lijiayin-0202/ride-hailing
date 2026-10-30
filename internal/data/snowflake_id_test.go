package data

import "testing"

func TestSnowflakeDispatchIDGeneratorProducesIncreasingPositiveIDs(t *testing.T) {
	generator, err := NewSnowflakeDispatchIDGenerator(42)
	if err != nil {
		t.Fatalf("NewSnowflakeDispatchIDGenerator() error = %v", err)
	}

	first := generator.New()
	second := generator.New()
	if first <= 0 {
		t.Fatalf("first ID = %d, want a positive ID", first)
	}
	if second <= first {
		t.Errorf("second ID = %d, want a value greater than first ID %d", second, first)
	}
}

func TestSnowflakeDispatchIDGeneratorRejectsOutOfRangeNodeID(t *testing.T) {
	if _, err := NewSnowflakeDispatchIDGenerator(1024); err == nil {
		t.Fatal("NewSnowflakeDispatchIDGenerator(1024) error = nil, want node ID range error")
	}
}
