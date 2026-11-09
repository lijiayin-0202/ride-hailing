package data

import (
	"encoding/json"
	"testing"
)

func TestDefaultJSON(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{name: "empty", want: `{}`},
		{name: "invalid", in: []byte(`not-json`), want: `{}`},
		{name: "valid", in: []byte("{\"speed\":12}"), want: `{"speed":12}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultJSON(tt.in)
			if !json.Valid(got) || string(got) != tt.want {
				t.Fatalf("defaultJSON() = %s, want %s", got, tt.want)
			}
		})
	}
}
