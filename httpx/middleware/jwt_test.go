package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func signHS256(t *testing.T, secret []byte, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func jwtRouter(secret []byte) (*gin.Engine, *jwt.MapClaims) {
	seen := &jwt.MapClaims{}
	r := gin.New()
	// Error middleware turns the aborted request's recorded error into a real
	// 401 response; without it an aborted request would leave the default 200.
	r.Use(Error(false))
	r.Use(JWT(JWTConfig{Secret: secret}))
	r.GET("/", func(c *gin.Context) {
		if claims, ok := JWTClaimsFromContext(c.Request.Context()); ok {
			*seen = claims
		}
		c.Status(http.StatusOK)
	})
	return r, seen
}

func TestJWTValidToken(t *testing.T) {
	secret := []byte("s3cr3t")
	r, seen := jwtRouter(secret)
	token := signHS256(t, secret, jwt.MapClaims{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if (*seen)["sub"] != "user-1" {
		t.Errorf("claims sub = %v, want user-1", (*seen)["sub"])
	}
}

func TestJWTMissingTokenAborts(t *testing.T) {
	r, _ := jwtRouter([]byte("s3cr3t"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Error("request without a token should not reach the handler")
	}
}

func TestJWTWrongSecretRejected(t *testing.T) {
	r, _ := jwtRouter([]byte("right"))
	token := signHS256(t, []byte("wrong"), jwt.MapClaims{"sub": "x", "exp": time.Now().Add(time.Hour).Unix()})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Error("token signed with the wrong secret should be rejected")
	}
}

func TestJWTExpiredRejected(t *testing.T) {
	secret := []byte("s3cr3t")
	r, _ := jwtRouter(secret)
	token := signHS256(t, secret, jwt.MapClaims{"sub": "x", "exp": time.Now().Add(-time.Hour).Unix()})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Error("expired token should be rejected")
	}
}
