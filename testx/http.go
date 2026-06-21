package testx

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Request issues an HTTP request against handler (typically httpx.Of(app).Router
// or any http.Handler) and returns the recorder. body is JSON-encoded when
// non-nil. It is the one-liner most handler tests need.
func Request(t testing.TB, handler http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("testx: encoding request body: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// DecodeJSON decodes the recorder's JSON body into out, failing the test on a
// decode error.
func DecodeJSON(t testing.TB, w *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("testx: decoding response body %q: %v", w.Body.String(), err)
	}
}

// AssertStatus fails the test when the recorder's status is not want, including
// the response body in the message to aid debugging.
func AssertStatus(t testing.TB, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("testx: status = %d, want %d; body: %s", w.Code, want, w.Body.String())
	}
}

// NewRouter returns a gin engine in test mode with no middleware, for unit tests
// that exercise a handler in isolation.
func NewRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}
