package data

import "testing"

func TestDispatchSnowflakeNodeIDFromEnvironment(t *testing.T) {
	testCases := []struct {
		name    string
		value   string
		present bool
		want    int64
		wantErr bool
	}{
		{name: "uses configured node ID", value: "1", present: true, want: 1},
		{name: "rejects missing variable", wantErr: true},
		{name: "rejects non numeric variable", value: "one", present: true, wantErr: true},
		{name: "rejects out of range variable", value: "1024", present: true, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			lookupEnv := func(string) (string, bool) {
				return testCase.value, testCase.present
			}

			got, err := dispatchSnowflakeNodeIDFromEnvironment(lookupEnv)
			if testCase.wantErr {
				if err == nil {
					t.Fatal("dispatchSnowflakeNodeIDFromEnvironment() error = nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("dispatchSnowflakeNodeIDFromEnvironment() error = %v", err)
			}
			if got != testCase.want {
				t.Errorf("dispatchSnowflakeNodeIDFromEnvironment() = %d, want %d", got, testCase.want)
			}
		})
	}
}

func TestNewDispatchIDGeneratorUsesEnvironmentNodeID(t *testing.T) {
	t.Setenv(dispatchSnowflakeNodeIDEnvironmentVariable, "1")

	generator, err := NewDispatchIDGenerator()
	if err != nil {
		t.Fatalf("NewDispatchIDGenerator() error = %v", err)
	}
	if generator.nodeID != 1 {
		t.Errorf("NewDispatchIDGenerator().nodeID = %d, want 1", generator.nodeID)
	}
}

func TestNewDispatchIDGeneratorRejectsMissingEnvironmentNodeID(t *testing.T) {
	t.Setenv(dispatchSnowflakeNodeIDEnvironmentVariable, "")

	if _, err := NewDispatchIDGenerator(); err == nil {
		t.Fatal("NewDispatchIDGenerator() error = nil, want missing environment variable error")
	}
}
