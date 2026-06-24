package gormx

import (
	"context"
	"errors"
	"testing"

	glebarez "github.com/glebarez/sqlite"
	"github.com/hatami57/microjet/core/errorx"
	"gorm.io/gorm"
)

type widget struct {
	ID    int `gorm:"primaryKey"`
	Name  string
	Color string
	Price int
}

func openWidgets(t *testing.T, seed ...widget) *Table[widget] {
	t.Helper()
	db, err := gorm.Open(glebarez.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&widget{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	table := NewTable[widget](db)
	for i := range seed {
		w := seed[i]
		if err := table.Create(context.Background(), &w); err != nil {
			t.Fatalf("seed %v: %v", w, err)
		}
	}
	return table
}

func TestFirstRespectsOrder(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Name: "a", Price: 30},
		widget{ID: 2, Name: "b", Price: 10},
		widget{ID: 3, Name: "c", Price: 20},
	)

	got, err := table.Order("price ASC").First(context.Background())
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if got == nil || got.Name != "b" {
		t.Fatalf("cheapest widget = %v, want b", got)
	}
}

func TestLastByPrimaryKey(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Name: "a"},
		widget{ID: 2, Name: "b"},
	)

	got, err := table.Last(context.Background())
	if err != nil {
		t.Fatalf("Last: %v", err)
	}
	if got == nil || got.ID != 2 {
		t.Fatalf("Last = %v, want ID 2", got)
	}
}

func TestTakeReturnsNilWhenEmpty(t *testing.T) {
	table := openWidgets(t)

	got, err := table.Take(context.Background())
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if got != nil {
		t.Fatalf("Take on empty table = %v, want nil", got)
	}
}

func TestChainedScopesAccumulate(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Name: "a", Color: "red", Price: 5},
		widget{ID: 2, Name: "b", Color: "red", Price: 15},
		widget{ID: 3, Name: "c", Color: "blue", Price: 25},
	)

	got, err := table.
		Where("color = ?", "red").
		Order("price DESC").
		Limit(1).
		ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("chained scopes = %v, want single widget b", got)
	}
}

func TestExists(t *testing.T) {
	table := openWidgets(t, widget{ID: 1, Name: "a", Color: "red"})

	ok, err := table.Exists(context.Background(), "color = ?", "red")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Fatal("Exists(red) = false, want true")
	}

	ok, err = table.Exists(context.Background(), "color = ?", "green")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if ok {
		t.Fatal("Exists(green) = true, want false")
	}
}

func TestGetReturnsNotFoundError(t *testing.T) {
	table := openWidgets(t, widget{ID: 1, Name: "a"})

	got, err := table.Get(context.Background(), "id = ?", 1)
	if err != nil {
		t.Fatalf("Get(existing): %v", err)
	}
	if got == nil || got.ID != 1 {
		t.Fatalf("Get(existing) = %v, want ID 1", got)
	}

	_, err = table.Get(context.Background(), "id = ?", 999)
	if !errors.Is(err, errorx.ErrNotFound) {
		t.Fatalf("Get(missing) err = %v, want errorx.ErrNotFound", err)
	}
	if subj := errorx.GetError(err).Subject; subj != "widget" {
		t.Fatalf("not-found subject = %q, want widget", subj)
	}
}

func TestSum(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Color: "red", Price: 5},
		widget{ID: 2, Color: "red", Price: 15},
		widget{ID: 3, Color: "blue", Price: 25},
	)
	ctx := context.Background()

	var total int
	if err := table.Sum(ctx, "price", &total); err != nil {
		t.Fatalf("Sum(all): %v", err)
	}
	if total != 45 {
		t.Fatalf("Sum(all) = %d, want 45", total)
	}

	// where-arg form
	if err := table.Sum(ctx, "price", &total, "color = ?", "red"); err != nil {
		t.Fatalf("Sum(where): %v", err)
	}
	if total != 20 {
		t.Fatalf("Sum(red) = %d, want 20", total)
	}

	// chained-scope form matches the where-arg form
	if err := table.Where("color = ?", "red").Sum(ctx, "price", &total); err != nil {
		t.Fatalf("Sum(chained): %v", err)
	}
	if total != 20 {
		t.Fatalf("Sum(chained red) = %d, want 20", total)
	}
}

func TestSumEmptyIsZero(t *testing.T) {
	table := openWidgets(t)

	var total int
	if err := table.Sum(context.Background(), "price", &total); err != nil {
		t.Fatalf("Sum(empty): %v", err)
	}
	if total != 0 {
		t.Fatalf("Sum(empty) = %d, want 0", total)
	}
}

func TestAvgMaxMin(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Price: 10},
		widget{ID: 2, Price: 20},
		widget{ID: 3, Price: 30},
	)
	ctx := context.Background()

	var avg float64
	if err := table.Avg(ctx, "price", &avg); err != nil {
		t.Fatalf("Avg: %v", err)
	}
	if avg != 20 {
		t.Fatalf("Avg = %v, want 20", avg)
	}

	var max, min int
	if err := table.Max(ctx, "price", &max); err != nil {
		t.Fatalf("Max: %v", err)
	}
	if err := table.Min(ctx, "price", &min); err != nil {
		t.Fatalf("Min: %v", err)
	}
	if max != 30 || min != 10 {
		t.Fatalf("Max/Min = %d/%d, want 30/10", max, min)
	}
}

func TestAvgMaxMinEmptyIsZero(t *testing.T) {
	table := openWidgets(t)
	ctx := context.Background()

	var avg float64
	var max, min int
	if err := table.Avg(ctx, "price", &avg); err != nil {
		t.Fatalf("Avg(empty): %v", err)
	}
	if err := table.Max(ctx, "price", &max); err != nil {
		t.Fatalf("Max(empty): %v", err)
	}
	if err := table.Min(ctx, "price", &min); err != nil {
		t.Fatalf("Min(empty): %v", err)
	}
	if avg != 0 || max != 0 || min != 0 {
		t.Fatalf("empty Avg/Max/Min = %v/%d/%d, want 0/0/0", avg, max, min)
	}
}

func TestAggregateCustomExpr(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Color: "red", Price: 10},
		widget{ID: 2, Color: "red", Price: 35},
		widget{ID: 3, Color: "blue", Price: 100},
	)

	var spread int
	if err := table.Aggregate(context.Background(), "MAX(price) - MIN(price)", &spread, "color = ?", "red"); err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if spread != 25 {
		t.Fatalf("price spread = %d, want 25", spread)
	}
}

func TestProjectColumns(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Name: "a", Color: "red", Price: 5},
		widget{ID: 2, Name: "b", Color: "blue", Price: 15},
	)

	type nameOnly struct {
		Name  string
		Price int
	}
	var rows []nameOnly
	if err := table.Select("name, price").Order("price DESC").Project(context.Background(), &rows); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "b" || rows[0].Price != 15 {
		t.Fatalf("Project = %+v, want b/15 first", rows)
	}
}

func TestProjectGroupByAggregate(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Color: "red", Price: 10},
		widget{ID: 2, Color: "red", Price: 30},
		widget{ID: 3, Color: "blue", Price: 100},
	)

	type tally struct {
		Color string
		Total int
	}
	var rows []tally
	if err := table.Select("color, COALESCE(SUM(price), 0) AS total").
		Group("color").
		Order("color ASC").
		Project(context.Background(), &rows); err != nil {
		t.Fatalf("Project(group): %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("Project(group) returned %d rows, want 2", len(rows))
	}
	if rows[0].Color != "blue" || rows[0].Total != 100 {
		t.Fatalf("row[0] = %+v, want blue/100", rows[0])
	}
	if rows[1].Color != "red" || rows[1].Total != 40 {
		t.Fatalf("row[1] = %+v, want red/40", rows[1])
	}
}

func TestProjectFirst(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Name: "a", Price: 30},
		widget{ID: 2, Name: "b", Price: 10},
	)
	ctx := context.Background()

	type nameOnly struct{ Name string }
	var got nameOnly
	found, err := table.Select("name").Order("price ASC").ProjectFirst(ctx, &got)
	if err != nil {
		t.Fatalf("ProjectFirst: %v", err)
	}
	if !found || got.Name != "b" {
		t.Fatalf("ProjectFirst = %+v found=%v, want b/true", got, found)
	}

	var empty nameOnly
	found, err = table.Where("price > ?", 1000).ProjectFirst(ctx, &empty)
	if err != nil {
		t.Fatalf("ProjectFirst(empty): %v", err)
	}
	if found {
		t.Fatalf("ProjectFirst(empty) found = true, want false")
	}
}

func TestProjectToMap(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Color: "red", Price: 10},
		widget{ID: 2, Color: "red", Price: 30},
		widget{ID: 3, Color: "blue", Price: 100},
	)
	ctx := context.Background()

	var rows []map[string]any
	if err := table.Select("color, COALESCE(SUM(price), 0) AS total").
		Group("color").Order("color ASC").Project(ctx, &rows); err != nil {
		t.Fatalf("Project->[]map: %v", err)
	}
	if len(rows) != 2 || rows[0]["color"] != "blue" {
		t.Fatalf("map rows = %v, want blue first of 2", rows)
	}
	// SQLite returns SUM as int64 through the map path.
	if total, ok := rows[1]["total"].(int64); !ok || total != 40 {
		t.Fatalf("red total = %v, want 40", rows[1]["total"])
	}

	var one map[string]any
	found, err := table.Select("color, price").Order("price ASC").ProjectFirst(ctx, &one)
	if err != nil {
		t.Fatalf("ProjectFirst->map: %v", err)
	}
	if !found || one["color"] != "red" {
		t.Fatalf("ProjectFirst map = %v found=%v, want red/true", one, found)
	}
}

func TestScopesDoNotMutateOriginal(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Color: "red"},
		widget{ID: 2, Color: "blue"},
	)

	filtered := table.Where("color = ?", "red")

	all, err := table.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("original table returned %d rows, want 2 — Where mutated it", len(all))
	}

	red, err := filtered.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(red) != 1 {
		t.Fatalf("filtered table returned %d rows, want 1", len(red))
	}
}
