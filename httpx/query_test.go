package httpx

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http/httptest"
	"testing"
)

func TestBindQueryAssignsStringAndUUID(t *testing.T) {
	type name string
	var target struct {
		Name name      `query:"name"`
		ID   uuid.UUID `query:"id"`
	}
	id := uuid.New()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/?name=alice&id="+id.String(), nil)
	var q QueryParamBase
	if err := q.BindQueryParams(c, &target); err != nil {
		t.Fatal(err)
	}
	if target.Name != "alice" || target.ID != id {
		t.Fatalf("target = %+v", target)
	}
	if q.GetMap()["name"] != target.Name || q.GetMap()["id"] != target.ID {
		t.Fatalf("map = %v", q.GetMap())
	}
}
