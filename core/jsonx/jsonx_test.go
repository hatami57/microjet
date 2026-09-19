package jsonx

import "testing"

func TestFromJSONRequiresObservableDestination(t *testing.T) {
	type payload struct{ Name string }
	var nilTarget *payload
	for _, target := range []any{payload{}, nilTarget, nil} {
		if err := FromJSON(`{"Name":"alice"}`, target); err == nil {
			t.Fatalf("accepted %T", target)
		}
	}
	var target payload
	if err := FromJSON(`{"Name":"alice"}`, &target); err != nil {
		t.Fatal(err)
	}
	if target.Name != "alice" {
		t.Fatalf("target = %+v", target)
	}
	var allocated *payload
	if err := FromJSON(`{"Name":"alice"}`, &allocated); err != nil {
		t.Fatal(err)
	}
	if allocated == nil || allocated.Name != "alice" {
		t.Fatalf("target = %+v", allocated)
	}
	if err := FromJSON(`null`, &allocated); err != nil {
		t.Fatal(err)
	}
	if allocated != nil {
		t.Fatal("null did not clear pointer")
	}
}
