package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hatami57/microjet/core"
)

func init() { gin.SetMode(gin.TestMode) }

func runWith(debug bool, handler gin.HandlerFunc) (*httptest.ResponseRecorder, core.ErrorResponse) {
	r := gin.New()
	r.Use(Error(debug))
	r.GET("/", handler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	var resp core.ErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

func TestTypedErrorMapsToStatus(t *testing.T) {
	w, resp := runWith(false, func(c *gin.Context) {
		c.Error(core.ErrNotFound.WithSubject("User"))
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if resp.Error != "not_found" || resp.Subject != "User" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestTypedErrorIncludesParams(t *testing.T) {
	_, resp := runWith(false, func(c *gin.Context) {
		c.Error(core.ErrBadRequest.WithSubject("email").WithParams("field", "email"))
	})
	if resp.Params == nil || resp.Params["field"] != "email" {
		t.Errorf("params not propagated: %+v", resp.Params)
	}
}

func TestInnerCauseHiddenWithoutDebug(t *testing.T) {
	secret := "connection string user:pass@db"
	_, resp := runWith(false, func(c *gin.Context) {
		c.Error(core.ErrInternal.WithInner(errors.New(secret)))
	})
	if resp.InnerError != nil {
		t.Errorf("inner cause leaked in production: %q", *resp.InnerError)
	}
}

func TestInnerCauseShownWithDebug(t *testing.T) {
	cause := "boom"
	_, resp := runWith(true, func(c *gin.Context) {
		c.Error(core.ErrInternal.WithInner(errors.New(cause)))
	})
	if resp.InnerError == nil || *resp.InnerError != cause {
		t.Errorf("inner cause not exposed in debug: %v", resp.InnerError)
	}
}

func TestUntypedErrorIsGenericInProduction(t *testing.T) {
	_, resp := runWith(false, func(c *gin.Context) {
		c.Error(errors.New("raw internal detail"))
	})
	if resp.InnerError != nil {
		t.Errorf("untyped error leaked detail: %q", *resp.InnerError)
	}
	if resp.Message != "An internal server error occurred" {
		t.Errorf("message = %q", resp.Message)
	}
}
