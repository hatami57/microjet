package gormx

import (
	"context"
	"github.com/hatami57/microjet/core/types"
	"testing"
)

func TestPaginationSizeBounds(t *testing.T) {
	table := openWidgets(t, widget{ID: 1}, widget{ID: 2})
	for _, size := range []int{-2, -1, 0, 1, types.MaxPageSize, types.MaxPageSize + 1} {
		valid := size >= 0 && size <= types.MaxPageSize
		for _, page := range []*int32{nil, new(int32(1))} {
			req := NewPageRequest(&types.PagedResultRequest{PageSize: int32(size), Page: page}, "id", func(w widget) int { return w.ID })
			got, err := table.List(context.Background(), req)
			if (err == nil) != valid {
				t.Fatalf("List size=%d: %v", size, err)
			}
			if size == 0 && got.PageSize != types.DefaultPageSize {
				t.Fatalf("default size = %d", got.PageSize)
			}
		}
		got, err := table.ListPage(context.Background(), NewPageOptions[widget]().PageSize(size))
		if (err == nil) != valid {
			t.Fatalf("ListPage size=%d: %v", size, err)
		}
		if size == 0 && got.PageSize != types.DefaultPageSize {
			t.Fatalf("default size = %d", got.PageSize)
		}
	}
}
