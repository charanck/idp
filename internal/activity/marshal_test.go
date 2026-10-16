package activity

import "testing"

func TestMarshalDetails(t *testing.T) {
	tests := []struct {
		name    string
		details any
		traceID string
		want    string
	}{
		{"nothing", nil, "", ""},
		{"no trace", map[string]any{"a": 1}, "", `{"a":1}`},
		{"nil details", nil, "abc", `{"trace_id":"abc"}`},
		{"object", map[string]any{"a": 1}, "abc", `{"a":1,"trace_id":"abc"}`},
		{"non-object", []int{1, 2}, "abc", `{"details":[1,2],"trace_id":"abc"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := marshalDetails(tt.details, tt.traceID)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tt.want {
				t.Fatalf("got %q, want %q", b, tt.want)
			}
		})
	}
}
