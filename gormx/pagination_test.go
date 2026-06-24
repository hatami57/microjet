package gormx

import (
	"testing"
	"time"

	"github.com/hatami57/microjet/core/types"
)

type pgUser struct {
	ID        int
	CreatedAt time.Time
	Name      string
}

func TestByID_NoTokenMeansFirstPage(t *testing.T) {
	r := NewPageRequest[pgUser, int](&types.PagedResultRequest{PageSize: 10}, "id", func(u pgUser) int { return u.ID })
	result, err := r.CurrentPageData()
	if err != nil {
		t.Fatalf("CurrentPageData: %v", err)
	}
	if len(result) > 0 {
		t.Fatalf("expected no cursor on first page, got %v", result)
	}
	if r.OrderBy() != "id ASC" || r.PageSize() != 10 {
		t.Errorf("OrderBy=%q PageSize=%d", r.OrderBy(), r.PageSize())
	}
}

func TestByID_CreateThenConsumeToken(t *testing.T) {
	r := NewPageRequest[pgUser, int](&types.PagedResultRequest{PageSize: 2}, "id", func(u pgUser) int { return u.ID })
	token, err := r.CreateNextPageToken([]pgUser{{ID: 1}, {ID: 2}})
	if err != nil {
		t.Fatalf("CreateNextPageToken: %v", err)
	}
	if token == nil {
		t.Fatal("expected a token for a full page")
	}

	next := NewPageRequest[pgUser, int](&types.PagedResultRequest{PageSize: 2, NextPageToken: token}, "id", func(u pgUser) int { return u.ID })
	result, err := next.CurrentPageData()
	if err != nil {
		t.Fatalf("CurrentPageData: %v", err)
	}
	if len(result) != 2 || result[0] != "id > ?" {
		t.Fatalf("result = %v, want [\"id > ?\", ...]", result)
	}
	if result[1].(int) != 2 {
		t.Fatalf("result[1] = %v, want int(2)", result[1])
	}
}

func TestByID_EmptyItemsYieldsNoToken(t *testing.T) {
	r := NewPageRequest[pgUser, int](&types.PagedResultRequest{PageSize: 10}, "id", func(u pgUser) int { return u.ID })
	token, err := r.CreateNextPageToken(nil)
	if err != nil || token != nil {
		t.Fatalf("expected nil token for empty page, got %v, %v", token, err)
	}
}

func TestByCreatedAt_CreateThenConsumeToken(t *testing.T) {
	ts := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	r := NewPageRequest[pgUser, time.Time](&types.PagedResultRequest{PageSize: 1}, "created_at", func(u pgUser) time.Time { return u.CreatedAt })
	token, err := r.CreateNextPageToken([]pgUser{{CreatedAt: ts}})
	if err != nil {
		t.Fatalf("CreateNextPageToken: %v", err)
	}

	next := NewPageRequest[pgUser, time.Time](&types.PagedResultRequest{PageSize: 1, NextPageToken: token}, "created_at", func(u pgUser) time.Time { return u.CreatedAt })
	result, err := next.CurrentPageData()
	if err != nil {
		t.Fatalf("CurrentPageData: %v", err)
	}
	if len(result) != 2 || result[0] != "created_at > ?" {
		t.Fatalf("result = %v, want [\"created_at > ?\", ...]", result)
	}
	if result[1].(time.Time) != ts {
		t.Fatalf("result[1] = %v, want %v", result[1], ts)
	}
}

func TestPageRequest_TypedCursor(t *testing.T) {
	r := NewPageRequest[pgUser, int](
		&types.PagedResultRequest{PageSize: 2},
		"id",
		func(u pgUser) int { return u.ID },
	)
	token, err := r.CreateNextPageToken([]pgUser{{ID: 1}, {ID: 2}})
	if err != nil {
		t.Fatalf("CreateNextPageToken: %v", err)
	}
	if token == nil {
		t.Fatal("expected a token")
	}

	next := NewPageRequest[pgUser, int](
		&types.PagedResultRequest{PageSize: 2, NextPageToken: token},
		"id",
		func(u pgUser) int { return u.ID },
	)
	result, err := next.CurrentPageData()
	if err != nil {
		t.Fatalf("CurrentPageData: %v", err)
	}
	if len(result) != 2 || result[0] != "id > ?" {
		t.Fatalf("result = %v, want [\"id > ?\", ...]", result)
	}
	if result[1].(int) != 2 {
		t.Fatalf("result[1] = %v (%T), want int(2)", result[1], result[1])
	}
}

func TestOffset_NoPageMeansCursorMode(t *testing.T) {
	r := NewPageRequest[pgUser, int](&types.PagedResultRequest{PageSize: 10}, "id", func(u pgUser) int { return u.ID })
	if offset, ok := r.Offset(); ok {
		t.Fatalf("expected cursor mode (ok=false), got offset=%d ok=true", offset)
	}
}

func TestOffset_PageNumberToOffset(t *testing.T) {
	cases := []struct {
		page int32
		want int
	}{
		{page: 1, want: 0},
		{page: 2, want: 20},
		{page: 5, want: 80},
		{page: 0, want: 0},  // clamped to first page
		{page: -3, want: 0}, // clamped to first page
	}
	for _, c := range cases {
		page := c.page
		r := NewPageRequest[pgUser, int](&types.PagedResultRequest{PageSize: 20, Page: &page}, "id", func(u pgUser) int { return u.ID })
		offset, ok := r.Offset()
		if !ok {
			t.Fatalf("page=%d: expected offset mode (ok=true)", c.page)
		}
		if offset != c.want {
			t.Errorf("page=%d: offset = %d, want %d", c.page, offset, c.want)
		}
	}
}

func TestPageRequest_DescOrder(t *testing.T) {
	r := NewPageRequest[pgUser, time.Time](
		&types.PagedResultRequest{PageSize: 5},
		"created_at",
		func(u pgUser) time.Time { return u.CreatedAt },
	).OrderDesc()

	if r.OrderBy() != "created_at DESC" {
		t.Errorf("OrderBy = %q, want \"created_at DESC\"", r.OrderBy())
	}

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	token, _ := r.CreateNextPageToken([]pgUser{{CreatedAt: ts}})
	next := NewPageRequest[pgUser, time.Time](
		&types.PagedResultRequest{PageSize: 5, NextPageToken: token},
		"created_at",
		func(u pgUser) time.Time { return u.CreatedAt },
	).OrderDesc()

	result, err := next.CurrentPageData()
	if err != nil {
		t.Fatalf("CurrentPageData: %v", err)
	}
	if len(result) != 2 || result[0] != "created_at < ?" {
		t.Fatalf("result = %v, want cursor with '<' operator", result)
	}
}
