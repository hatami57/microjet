package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/core/errorx"
)

type signup struct {
	Email string `json:"emailAddress" binding:"required,email"`
	Age   int    `json:"age" binding:"required,gte=18"`
}

func postBody(t *testing.T, json string) (signup, error) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(json))
	c.Request.Header.Set("Content-Type", "application/json")
	return Body[signup](c)
}

func TestBodyValidationReportsFieldsByJSONName(t *testing.T) {
	_, err := postBody(t, `{"emailAddress":"nope","age":15}`)
	if err == nil {
		t.Fatal("expected a validation error")
	}
	if !errorx.IsBadRequestError(err) {
		t.Fatalf("error = %v, want BadRequest", err)
	}
	appErr := errorx.GetError(err)
	fields, ok := appErr.Params["fields"].(map[string]any)
	if !ok {
		t.Fatalf("missing fields param: %+v", appErr.Params)
	}
	// JSON tag names, not Go field names.
	if _, ok := fields["emailAddress"]; !ok {
		t.Errorf("expected emailAddress in fields, got %v", fields)
	}
	if msg, ok := fields["age"].(string); !ok || msg != "must be greater than or equal to 18" {
		t.Errorf("age message = %v", fields["age"])
	}
}

func TestBodyValidationRequiredField(t *testing.T) {
	_, err := postBody(t, `{"age":21}`)
	if err == nil {
		t.Fatal("expected a validation error")
	}
	fields := errorx.GetError(err).Params["fields"].(map[string]any)
	if fields["emailAddress"] != "is required" {
		t.Errorf("emailAddress message = %v, want \"is required\"", fields["emailAddress"])
	}
}

func TestBodyValidationTypeMismatch(t *testing.T) {
	_, err := postBody(t, `{"emailAddress":"a@b.com","age":"old"}`)
	if err == nil {
		t.Fatal("expected a type-mismatch error")
	}
	if !errorx.IsBadRequestError(err) {
		t.Errorf("error = %v, want BadRequest", err)
	}
	if _, hasFields := errorx.GetError(err).Params["fields"]; hasFields {
		t.Error("type mismatch should not carry per-field validation details")
	}
}

func TestBodyValidationPasses(t *testing.T) {
	got, err := postBody(t, `{"emailAddress":"a@b.com","age":30}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Email != "a@b.com" || got.Age != 30 {
		t.Errorf("decoded = %+v", got)
	}
}
