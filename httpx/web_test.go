package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMergeParams(t *testing.T) {
	body := strings.NewReader("b=form&shared=postwins")
	r := httptest.NewRequest(http.MethodPost, "/?a=query&shared=querywins", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	got := MergeParams(r)
	if got["a"] != "query" {
		t.Errorf("query param a = %q, want query", got["a"])
	}
	if got["b"] != "form" {
		t.Errorf("form param b = %q, want form", got["b"])
	}
	if got["shared"] != "postwins" {
		t.Errorf("shared = %q, want postwins (POST form should win)", got["shared"])
	}
}

func TestWriteAutoPostForm(t *testing.T) {
	w := httptest.NewRecorder()
	if err := WriteAutoPostForm(w, "https://bank.example/pay", map[string]string{
		"Token": "abc123",
		"evil":  `"><script>`,
	}); err != nil {
		t.Fatalf("WriteAutoPostForm: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, `action="https://bank.example/pay"`) {
		t.Error("form action missing")
	}
	if !strings.Contains(body, "document.forms[0].submit()") {
		t.Error("auto-submit script missing")
	}
	if !strings.Contains(body, `name="Token" value="abc123"`) {
		t.Error("hidden field missing")
	}
	// The dangerous value must be HTML-escaped, not emitted raw.
	if strings.Contains(body, "<script>") {
		t.Error("value was not HTML-escaped")
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}
