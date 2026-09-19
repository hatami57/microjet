package httpx

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestPagedRequestBounds(t *testing.T) {
	for raw, want := range map[string]int32{"-2": 10, "-1": 10, "0": 10, "1": 1, "1000": 1000, "2147483647": 1000} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/?pageSize="+raw, nil)
		if got := PagedRequest(c).PageSize; got != want {
			t.Errorf("%s: got %d, want %d", raw, got, want)
		}
	}
}
