package utils

import "testing"

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
