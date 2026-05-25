package utils

import "testing"

func TestJSONRoundTrip(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	in := payload{Name: "alice", N: 7}
	s, err := ToJSON(in)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	var out payload
	if err := FromJSON(s, &out); err != nil {
		t.Fatalf("FromJSON: %v", err)
	}
	if out != in {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

func TestMapConversions(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	m, err := ToMap(payload{Name: "bob"})
	if err != nil {
		t.Fatalf("ToMap: %v", err)
	}
	if m["name"] != "bob" {
		t.Errorf("ToMap = %v", m)
	}
	back, err := MapTo[payload](m)
	if err != nil || back.Name != "bob" {
		t.Errorf("MapTo = %+v, %v", back, err)
	}
}

func TestCoalesce(t *testing.T) {
	a, b := 1, 2
	if got := Coalesce[int](nil, &a, &b); got == nil || *got != 1 {
		t.Errorf("Coalesce = %v, want 1", got)
	}
	if got := Coalesce[int](nil, nil); got != nil {
		t.Errorf("Coalesce all-nil = %v, want nil", got)
	}
	if got := CoalesceVal(99, nil, &b); got != 2 {
		t.Errorf("CoalesceVal = %d, want 2", got)
	}
	if got := CoalesceVal(99, nil); got != 99 {
		t.Errorf("CoalesceVal default = %d, want 99", got)
	}
}
