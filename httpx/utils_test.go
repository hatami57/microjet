package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func ctxWithURL(target string) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return c
}

func TestFindQuery(t *testing.T) {
	c := ctxWithURL("/?name=alice")
	got, err := FindQuery(c, "name")
	if err != nil || got == nil || *got != "alice" {
		t.Fatalf("FindQuery present = %v, %v", got, err)
	}
	if _, err := FindQuery(c, "missing"); err == nil {
		t.Error("expected error for missing query param")
	}
}

func TestFindInt64Query(t *testing.T) {
	c := ctxWithURL("/?n=42&bad=xyz")
	if got, err := FindInt64Query(c, "n"); err != nil || got != 42 {
		t.Errorf("FindInt64Query = %d, %v; want 42", got, err)
	}
	if _, err := FindInt64Query(c, "bad"); err == nil {
		t.Error("expected error for non-integer value")
	}
}

func TestFindUUIDQuery(t *testing.T) {
	c := ctxWithURL("/?id=4c7f6f1e-9e0b-4d8a-9b2e-2d3a1c5e7f90&bad=nope")
	if _, err := FindUUIDQuery(c, "id"); err != nil {
		t.Errorf("valid UUID returned error: %v", err)
	}
	if _, err := FindUUIDQuery(c, "bad"); err == nil {
		t.Error("expected error for invalid UUID")
	}
}

func TestFindBoolQuery(t *testing.T) {
	c := ctxWithURL("/?ok=true&bad=maybe")
	if got, err := FindBoolQuery(c, "ok"); err != nil || !got {
		t.Errorf("FindBoolQuery = %v, %v; want true", got, err)
	}
	if _, err := FindBoolQuery(c, "bad"); err == nil {
		t.Error("expected error for invalid bool")
	}
}

func TestBodyBinding(t *testing.T) {
	type createUser struct {
		Name string `json:"name"`
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"bob"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	body, err := Body[createUser](c)
	if err != nil || body.Name != "bob" {
		t.Fatalf("Body = %v, %v; want {bob}", body, err)
	}

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{not json`))
	c2.Request.Header.Set("Content-Type", "application/json")
	if _, err := Body[createUser](c2); err == nil {
		t.Error("expected error for malformed JSON body")
	}
}

func TestPagedRequestDefaults(t *testing.T) {
	c := ctxWithURL("/")
	req := PagedRequest(c)
	if req.PageSize != 10 {
		t.Errorf("default PageSize = %d, want 10", req.PageSize)
	}

	c2 := ctxWithURL("/?pageSize=25&nextPageToken=abc")
	req2 := PagedRequest(c2)
	if req2.PageSize != 25 {
		t.Errorf("PageSize = %d, want 25", req2.PageSize)
	}
	if req2.NextPageToken == nil || *req2.NextPageToken != "abc" {
		t.Errorf("NextPageToken = %v, want abc", req2.NextPageToken)
	}
}
