package middleware

import (
	"context"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/hatami57/microjet/core"
)

// DefaultJWTContextKey is the gin context key under which verified claims are stored.
const DefaultJWTContextKey = "jwtClaims"

type jwtCtxKey int

const claimsKey jwtCtxKey = 0

// JWTConfig configures the JWT authentication middleware. Provide either Secret
// (for HMAC algorithms) or Keyfunc (for RSA/ECDSA/JWKS or custom resolution).
type JWTConfig struct {
	// Secret is the HMAC key used to verify HS256/384/512 tokens. Ignored when
	// Keyfunc is set.
	Secret []byte
	// Keyfunc resolves the verification key; overrides Secret when set.
	Keyfunc jwt.Keyfunc
	// Algorithms lists accepted signing methods. Defaults to ["HS256"] when using
	// Secret. Always set this when supplying a Keyfunc to prevent algorithm
	// confusion attacks.
	Algorithms []string
	// ContextKey overrides where claims are stored on the gin context
	// (default "jwtClaims").
	ContextKey string
	// TokenExtractor overrides how the raw token is pulled from the request
	// (default: the "Bearer <token>" Authorization header).
	TokenExtractor func(*gin.Context) string
}

// JWT returns a middleware that verifies a JWT on each request and stores the
// claims on the context. On any failure it records core.ErrUnauthorized and
// aborts, leaving the response to the Error middleware.
func JWT(cfg JWTConfig) gin.HandlerFunc {
	keyfunc := cfg.Keyfunc
	if keyfunc == nil {
		secret := cfg.Secret
		keyfunc = func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return secret, nil
		}
	}

	algorithms := cfg.Algorithms
	if len(algorithms) == 0 && cfg.Keyfunc == nil {
		algorithms = []string{"HS256"}
	}
	var parserOpts []jwt.ParserOption
	if len(algorithms) > 0 {
		parserOpts = append(parserOpts, jwt.WithValidMethods(algorithms))
	}

	ctxKey := cfg.ContextKey
	if ctxKey == "" {
		ctxKey = DefaultJWTContextKey
	}
	extract := cfg.TokenExtractor
	if extract == nil {
		extract = bearerToken
	}

	return func(c *gin.Context) {
		tokenStr := extract(c)
		if tokenStr == "" {
			c.Error(core.ErrUnauthorized.WithMessage("missing bearer token"))
			c.Abort()
			return
		}
		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, keyfunc, parserOpts...)
		if err != nil || !token.Valid {
			c.Error(core.ErrUnauthorized.WithMessage("invalid or expired token"))
			c.Abort()
			return
		}
		c.Set(ctxKey, claims)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), claimsKey, claims))
		c.Next()
	}
}

func bearerToken(c *gin.Context) string {
	const prefix = "Bearer "
	h := c.GetHeader("Authorization")
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

// JWTClaimsFromContext returns the verified claims stored in ctx by the JWT
// middleware, or (nil, false) if the request was not authenticated.
func JWTClaimsFromContext(ctx context.Context) (jwt.MapClaims, bool) {
	claims, ok := ctx.Value(claimsKey).(jwt.MapClaims)
	return claims, ok
}
