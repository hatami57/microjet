package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/httpx/middleware"
	"github.com/hatami57/microjet/types"
)

// RequestID returns the correlation id for the current request (set by the
// RequestID middleware), or "" if none.
func RequestID(c *gin.Context) string {
	return middleware.RequestIDFromContext(c.Request.Context())
}

// LoggerFrom returns the request-scoped logger carried by ctx (tagged with the
// request id), falling back to slog.Default() when none is present.
func LoggerFrom(ctx context.Context) *slog.Logger {
	return middleware.LoggerFromContext(ctx)
}

func GetTenantID(c *gin.Context) (uuid.UUID, error) {
	return middleware.GetTenantID(c)
}

func GetUserID(c *gin.Context) (uuid.UUID, error) {
	return GetUUIDQuery(c, "UserId")
}

func GetTenantUserID(c *gin.Context) (uuid.UUID, uuid.UUID, error) {
	tenantID, err := GetTenantID(c)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	userID, err := GetUserID(c)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return tenantID, userID, nil
}

func GetParam(c *gin.Context, key string) (string, error) {
	value := c.Param(key)
	if value == "" {
		return "", core.ErrNotFound.WithMessage(fmt.Sprintf("Param '%s' is not provided", key))
	}
	return value, nil
}

func GetUUIDParam(c *gin.Context, key string) (uuid.UUID, error) {
	value, err := GetParam(c, key)
	if err != nil {
		return uuid.Nil, err
	}
	return parseUUID(value, key)
}

func GetInt64Param(c *gin.Context, key string) (int64, error) {
	value, err := GetParam(c, key)
	if err != nil {
		return 0, err
	}
	return parseInt64(value, key)
}

func GetInt32Param(c *gin.Context, key string) (int32, error) {
	v, err := GetInt64Param(c, key)
	return int32(v), err
}

func GetQuery(c *gin.Context, key string) (*string, error) {
	value, ok := c.GetQuery(key)
	if !ok {
		return nil, core.ErrNotFound.WithMessage(fmt.Sprintf("Query param '%s' is not provided", key))
	}
	return &value, nil
}

func GetUUIDQuery(c *gin.Context, key string) (uuid.UUID, error) {
	value, err := GetQuery(c, key)
	if err != nil {
		return uuid.Nil, err
	}
	return parseUUID(*value, key)
}

func GetInt64Query(c *gin.Context, key string) (int64, error) {
	value, err := GetQuery(c, key)
	if err != nil {
		return 0, err
	}
	return parseInt64(*value, key)
}

func GetInt32Query(c *gin.Context, key string) (int32, error) {
	v, err := GetInt64Query(c, key)
	return int32(v), err
}

func GetBoolQuery(c *gin.Context, key string) (bool, error) {
	value, err := GetQuery(c, key)
	if err != nil {
		return false, err
	}
	v, err := strconv.ParseBool(*value)
	if err != nil {
		return false, core.ErrBadRequest.WithMessage(fmt.Sprintf("Invalid bool for '%s'", key))
	}
	return v, nil
}

// Body decodes and validates the JSON request body into a value of type T.
// It returns the value directly (not a pointer): request bodies are value-shaped
// data, and returning T avoids a nil-pointer footgun on the error path.
func Body[T any](c *gin.Context) (T, error) {
	var body T
	if err := c.ShouldBindJSON(&body); err != nil {
		return body, core.ErrBadRequest.WithMessage(fmt.Sprintf("Invalid body: %s", err.Error()))
	}
	return body, nil
}

// PagedRequest builds a pagination request from the query string. The pageSize
// query param defaults to 10 when absent, and a malformed pageSize also falls
// back to 10 rather than erroring — listing endpoints favor a sensible default
// over rejecting the request.
func PagedRequest(c *gin.Context) *types.PagedResultRequest {
	pageSize, err := strconv.ParseInt(c.DefaultQuery("pageSize", "10"), 10, 32)
	if err != nil {
		pageSize = 10
	}
	nextToken, _ := GetQuery(c, "nextPageToken")
	return &types.PagedResultRequest{
		PageSize:      int32(pageSize),
		NextPageToken: nextToken,
	}
}

func parseInt64(value, key string) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, core.ErrBadRequest.WithMessage(fmt.Sprintf("Invalid integer for '%s'", key))
	}
	return n, nil
}

func parseUUID(value, key string) (uuid.UUID, error) {
	v, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, core.ErrBadRequest.WithMessage(fmt.Sprintf("Invalid UUID for '%s'", key))
	}
	return v, nil
}
