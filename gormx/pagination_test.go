package gormx

import (
	"testing"
	"time"

	"github.com/hatami57/microjet/types"
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

func TestSetWhere(t *testing.T) {
	r := NewPageRequest[pgUser, int](&types.PagedResultRequest{PageSize: 5}, "id", func(u pgUser) int { return u.ID }).
		SetWhere("name ILIKE ?", "%bob%")
	where := r.Where()
	if len(where) != 2 || where[0] != "name ILIKE ?" || where[1] != "%bob%" {
		t.Fatalf("Where = %v", where)
	}
}

func TestSetWhereMap(t *testing.T) {
	r := NewPageRequest[pgUser, int](&types.PagedResultRequest{PageSize: 5}, "id", func(u pgUser) int { return u.ID }).
		SetWhere(map[string]any{"active": true})
	where := r.Where()
	if len(where) != 1 {
		t.Fatalf("expected 1 element, got %v", where)
	}
	m, ok := where[0].(map[string]any)
	if !ok || m["active"] != true {
		t.Fatalf("WhereMap = %v", where[0])
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