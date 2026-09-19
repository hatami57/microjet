package httpx

import (
	"github.com/gin-gonic/gin"
	"math"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestInt32HelpersRejectOverflow(t *testing.T) {
	for _, value := range []int64{math.MinInt32 - 1, math.MinInt32, math.MaxInt32, math.MaxInt32 + 1} {
		raw := strconv.FormatInt(value, 10)
		valid := value >= math.MinInt32 && value <= math.MaxInt32
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/?n="+raw, nil)
		c.Params = gin.Params{{Key: "n", Value: raw}}
		for _, helper := range []func(*gin.Context, string) (int32, error){GetInt32Param, GetInt32Query} {
			got, err := helper(c, "n")
			if (err == nil) != valid {
				t.Errorf("%s: error = %v", raw, err)
			}
			if valid && int64(got) != value {
				t.Errorf("%s: got %d", raw, got)
			}
		}
		var q QueryParamBase
		target := struct {
			N int32 `query:"n"`
		}{}
		err := q.BindQueryParams(c, &target)
		if (err == nil) != valid {
			t.Errorf("binding %s: error = %v", raw, err)
		}
	}
}
