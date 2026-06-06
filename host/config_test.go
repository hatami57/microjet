package host

import "testing"

func TestGetExtraConfig(t *testing.T) {
	type myExtra struct {
		Workers int
		Queue   string
	}

	want := myExtra{Workers: 4, Queue: "jobs"}
	cfg := &Config{Extra: want}

	got, ok := GetExtraConfig[myExtra](cfg)
	if !ok || got != want {
		t.Errorf("GetExtraConfig = %v, %v; want %v, true", got, ok, want)
	}

	_, ok = GetExtraConfig[string](cfg)
	if ok {
		t.Error("GetExtraConfig[string] should return false for wrong type")
	}
}

func TestMustGetExtraConfigPanicsOnWrongType(t *testing.T) {
	cfg := &Config{Extra: 42}
	defer func() {
		if recover() == nil {
			t.Error("expected panic for wrong extra type")
		}
	}()
	MustGetExtraConfig[string](cfg)
}