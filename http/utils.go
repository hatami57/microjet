package httpx

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hatami57/microjet/core"
	"github.com/hatami57/microjet/http/middleware"
	"github.com/hatami57/microjet/types"
)

func FindTenantID(c *gin.Context) (uuid.UUID, error) {
	return middleware.FindTenantID(c)
}

func FindUserID(c *gin.Context) (uuid.UUID, error) {
	return FindUUIDQuery(c, "UserId")
}

func FindTenantUserID(c *gin.Context) (uuid.UUID, uuid.UUID, error) {
	tenantID, err := FindTenantID(c)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	userID, err := FindUserID(c)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return tenantID, userID, nil
}

func FindParam(c *gin.Context, key string) (string, error) {
	value := c.Param(key)
	if value == "" {
		return "", core.ErrNotFound.WithMessage(fmt.Sprintf("Param '%s' is not provided", key))
	}
	return value, nil
}

func FindUUIDParam(c *gin.Context, key string) (uuid.UUID, error) {
	value, err := FindParam(c, key)
	if err != nil {
		return uuid.Nil, err
	}
	return parseUUID(value, key)
}

func FindInt64Param(c *gin.Context, key string) (int64, error) {
	value, err := FindParam(c, key)
	if err != nil {
		return 0, err
	}
	return parseInt64(value, key)
}

func FindInt32Param(c *gin.Context, key string) (int32, error) {
	v, err := FindInt64Param(c, key)
	return int32(v), err
}

func FindQuery(c *gin.Context, key string) (*string, error) {
	value, ok := c.GetQuery(key)
	if !ok {
		return nil, core.ErrNotFound.WithMessage(fmt.Sprintf("Query param '%s' is not provided", key))
	}
	return &value, nil
}

func FindUUIDQuery(c *gin.Context, key string) (uuid.UUID, error) {
	value, err := FindQuery(c, key)
	if err != nil {
		return uuid.Nil, err
	}
	return parseUUID(*value, key)
}

func FindInt64Query(c *gin.Context, key string) (int64, error) {
	value, err := FindQuery(c, key)
	if err != nil {
		return 0, err
	}
	return parseInt64(*value, key)
}

func FindInt32Query(c *gin.Context, key string) (int32, error) {
	v, err := FindInt64Query(c, key)
	return int32(v), err
}

func FindBoolQuery(c *gin.Context, key string) (bool, error) {
	value, err := FindQuery(c, key)
	if err != nil {
		return false, err
	}
	v, err := strconv.ParseBool(*value)
	if err != nil {
		return false, core.ErrBadRequest.WithMessage(fmt.Sprintf("Invalid bool for '%s'", key))
	}
	return v, nil
}

func Body[T any](c *gin.Context) (*T, error) {
	var body T
	if err := c.ShouldBindJSON(&body); err != nil {
		return nil, core.ErrBadRequest.WithMessage(fmt.Sprintf("Invalid body: %s", err.Error()))
	}
	return &body, nil
}

func PagedRequest(c *gin.Context) *types.PagedResultRequest {
	pageSize, err := strconv.ParseInt(c.DefaultQuery("pageSize", "10"), 10, 32)
	if err != nil {
		pageSize = 10
	}
	nextToken, _ := FindQuery(c, "nextPageToken")
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
