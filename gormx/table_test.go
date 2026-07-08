package gormx

import (
	"context"
	"errors"
	"reflect"
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

func newWidgetDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(glebarez.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&widget{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func openWidgets(t *testing.T, seed ...widget) *Table[widget] {
	t.Helper()
	table := NewTable[widget](newWidgetDB(t))
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

func TestOmitExcludesColumnOnRead(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Name: "a", Color: "red", Price: 5},
	)

	got, err := table.Omit("color").First(context.Background(), 1)
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if got == nil {
		t.Fatal("First returned nil")
	}
	// The omitted column is left out of the SELECT, so it comes back zero-valued...
	if got.Color != "" {
		t.Errorf("Color = %q, want empty (omitted from SELECT)", got.Color)
	}
	// ...while the other columns are still populated.
	if got.Name != "a" || got.Price != 5 {
		t.Errorf("got = %+v, want Name=a Price=5", got)
	}
}

func TestOmitExcludesColumnOnWrite(t *testing.T) {
	ctx := context.Background()
	table := openWidgets(t)

	w := widget{ID: 1, Name: "a", Color: "red", Price: 5}
	if err := table.Omit("price").Create(ctx, &w); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := table.Get(ctx, 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// price was omitted from the INSERT, so it fell back to the column default.
	if got.Price != 0 {
		t.Errorf("Price = %d, want 0 (omitted from INSERT)", got.Price)
	}
	if got.Name != "a" || got.Color != "red" {
		t.Errorf("got = %+v, want Name=a Color=red", got)
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

func TestPluckKeepsDuplicates(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Color: "red"},
		widget{ID: 2, Color: "red"},
		widget{ID: 3, Color: "blue"},
	)
	ctx := context.Background()

	var colors []string
	if err := table.Pluck(ctx, "color", &colors); err != nil {
		t.Fatalf("Pluck: %v", err)
	}
	if len(colors) != 3 {
		t.Fatalf("Pluck returned %v, want 3 values with duplicates", colors)
	}

	var distinct []string
	if err := table.PluckDistinct(ctx, "color", &distinct); err != nil {
		t.Fatalf("PluckDistinct: %v", err)
	}
	if len(distinct) != 2 {
		t.Fatalf("PluckDistinct returned %v, want 2 unique values", distinct)
	}

	// where-arg form scopes the column.
	var reds []string
	if err := table.Pluck(ctx, "color", &reds, "color = ?", "red"); err != nil {
		t.Fatalf("Pluck(where): %v", err)
	}
	if len(reds) != 2 {
		t.Fatalf("Pluck(red) = %v, want 2", reds)
	}
}

func TestUpdateMapReportsAffectedAndGuards(t *testing.T) {
	table := openWidgets(t, widget{ID: 1, Name: "a", Price: 5})
	ctx := context.Background()

	// Guard passes: one row updated.
	n, err := table.UpdateMap(ctx, map[string]any{"price": 8}, "id = ? AND price < ?", 1, 10)
	if err != nil {
		t.Fatalf("UpdateMap(pass): %v", err)
	}
	if n != 1 {
		t.Fatalf("UpdateMap(pass) affected = %d, want 1", n)
	}

	// Guard rejects: zero rows, no error.
	n, err = table.UpdateMap(ctx, map[string]any{"price": 99}, "id = ? AND price < ?", 1, 5)
	if err != nil {
		t.Fatalf("UpdateMap(reject): %v", err)
	}
	if n != 0 {
		t.Fatalf("UpdateMap(reject) affected = %d, want 0", n)
	}

	got, _ := table.Get(ctx, "id = ?", 1)
	if got.Price != 8 {
		t.Fatalf("price = %d, want 8 (reject must not write)", got.Price)
	}
}

func TestUpdateMapAtomicExpr(t *testing.T) {
	table := openWidgets(t, widget{ID: 1, Price: 5})
	ctx := context.Background()

	// gormx.Expr increments in-place; guard bounds it at < 7.
	n, err := table.UpdateMap(ctx, map[string]any{"price": Expr("price + 1")}, "id = ? AND price < ?", 1, 7)
	if err != nil {
		t.Fatalf("UpdateMap(expr pass): %v", err)
	}
	if n != 1 {
		t.Fatalf("UpdateMap(expr pass) affected = %d, want 1", n)
	}

	got, _ := table.Get(ctx, "id = ?", 1)
	if got.Price != 6 {
		t.Fatalf("price = %d, want 6", got.Price)
	}

	// price is now 6; the guard (< 7) still passes, reaching 7.
	if _, err := table.UpdateMap(ctx, map[string]any{"price": Expr("price + 1")}, "id = ? AND price < ?", 1, 7); err != nil {
		t.Fatalf("UpdateMap(expr second): %v", err)
	}
	// price is now 7; the guard rejects.
	n, err = table.UpdateMap(ctx, map[string]any{"price": Expr("price + 1")}, "id = ? AND price < ?", 1, 7)
	if err != nil {
		t.Fatalf("UpdateMap(expr reject): %v", err)
	}
	if n != 0 {
		t.Fatalf("UpdateMap(expr reject) affected = %d, want 0", n)
	}
	got, _ = table.Get(ctx, "id = ?", 1)
	if got.Price != 7 {
		t.Fatalf("price = %d, want 7 (bounded)", got.Price)
	}
}

func TestUpdateColumns(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Color: "red", Price: 5},
		widget{ID: 2, Color: "red", Price: 7},
		widget{ID: 3, Color: "blue", Price: 9},
	)
	ctx := context.Background()

	n, err := table.UpdateColumns(ctx, map[string]any{"price": 0, "name": "cleared"}, "color = ?", "red")
	if err != nil {
		t.Fatalf("UpdateColumns: %v", err)
	}
	if n != 2 {
		t.Fatalf("UpdateColumns affected = %d, want 2", n)
	}

	n, err = table.UpdateColumn(ctx, "price", 42, "id = ?", 3)
	if err != nil {
		t.Fatalf("UpdateColumn: %v", err)
	}
	if n != 1 {
		t.Fatalf("UpdateColumn affected = %d, want 1", n)
	}
	got, _ := table.Get(ctx, "id = ?", 3)
	if got.Price != 42 {
		t.Fatalf("price = %d, want 42", got.Price)
	}
}

// hooked exercises that UpdateColumn(s) bypass model hooks while UpdateMap runs them.
type hooked struct {
	ID    int `gorm:"primaryKey"`
	Value int
}

var beforeUpdateCalls int

func (hooked) BeforeUpdate(*gorm.DB) error {
	beforeUpdateCalls++
	return nil
}

func TestUpdateColumnsSkipHooks(t *testing.T) {
	db := newWidgetDB(t)
	if err := db.AutoMigrate(&hooked{}); err != nil {
		t.Fatalf("migrate hooked: %v", err)
	}
	table := NewTable[hooked](db)
	ctx := context.Background()
	if err := table.Create(ctx, &hooked{ID: 1, Value: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	beforeUpdateCalls = 0
	if _, err := table.UpdateColumns(ctx, map[string]any{"value": 2}, "id = ?", 1); err != nil {
		t.Fatalf("UpdateColumns: %v", err)
	}
	if beforeUpdateCalls != 0 {
		t.Fatalf("UpdateColumns ran BeforeUpdate %d times, want 0", beforeUpdateCalls)
	}

	beforeUpdateCalls = 0
	if _, err := table.UpdateMap(ctx, map[string]any{"value": 3}, "id = ?", 1); err != nil {
		t.Fatalf("UpdateMap: %v", err)
	}
	if beforeUpdateCalls != 1 {
		t.Fatalf("UpdateMap ran BeforeUpdate %d times, want 1", beforeUpdateCalls)
	}
}

func TestRawScansScalarSliceAndStruct(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Name: "a", Price: 5},
		widget{ID: 2, Name: "b", Price: 7},
	)
	ctx := context.Background()

	var total int
	if err := table.Raw(ctx, &total, "SELECT SUM(price) FROM widgets"); err != nil {
		t.Fatalf("Raw(scalar): %v", err)
	}
	if total != 12 {
		t.Fatalf("Raw scalar = %d, want 12", total)
	}

	var names []string
	if err := table.Raw(ctx, &names, "SELECT name FROM widgets ORDER BY id"); err != nil {
		t.Fatalf("Raw(slice): %v", err)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("Raw slice = %v, want [a b]", names)
	}

	var row widget
	if err := table.Raw(ctx, &row, "SELECT * FROM widgets WHERE id = ?", 2); err != nil {
		t.Fatalf("Raw(struct): %v", err)
	}
	if row.Name != "b" {
		t.Fatalf("Raw struct = %+v, want name b", row)
	}
}

func TestExecAffectsRows(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1, Color: "red", Price: 5},
		widget{ID: 2, Color: "red", Price: 7},
		widget{ID: 3, Color: "blue", Price: 9},
	)
	ctx := context.Background()

	n, err := table.Exec(ctx, "UPDATE widgets SET price = price + ? WHERE color = ?", 10, "red")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if n != 2 {
		t.Fatalf("Exec affected = %d, want 2", n)
	}
	got, _ := table.Get(ctx, "id = ?", 1)
	if got.Price != 15 {
		t.Fatalf("price = %d, want 15", got.Price)
	}
}

func TestFindInBatches(t *testing.T) {
	table := openWidgets(t,
		widget{ID: 1}, widget{ID: 2}, widget{ID: 3}, widget{ID: 4}, widget{ID: 5},
	)
	ctx := context.Background()

	var batches [][]int
	err := table.Order("id").FindInBatches(ctx, 2, func(batch []*widget) error {
		ids := make([]int, len(batch))
		for i, w := range batch {
			ids[i] = w.ID
		}
		batches = append(batches, ids)
		return nil
	})
	if err != nil {
		t.Fatalf("FindInBatches: %v", err)
	}
	want := [][]int{{1, 2}, {3, 4}, {5}}
	if !reflect.DeepEqual(batches, want) {
		t.Fatalf("batches = %v, want %v", batches, want)
	}

	// An error from the callback stops iteration and surfaces.
	stop := errors.New("stop")
	var seen int
	err = table.Order("id").FindInBatches(ctx, 2, func(batch []*widget) error {
		seen += len(batch)
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("FindInBatches err = %v, want stop", err)
	}
	if seen != 2 {
		t.Fatalf("processed %d rows before stop, want 2", seen)
	}
}

func TestLockForUpdateWithinTx(t *testing.T) {
	db := newWidgetDB(t)
	repo := NewBaseRepository(db)
	table := NewTable[widget](db)
	ctx := context.Background()
	if err := table.Create(ctx, &widget{ID: 1, Name: "a", Price: 5}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// On SQLite the FOR UPDATE clause is dropped, so this verifies the read-modify-write
	// still runs and commits inside RunTx without the locking clause breaking the query.
	err := repo.RunTx(ctx, func(ctx context.Context) error {
		w, err := table.LockForUpdate().Get(ctx, "id = ?", 1)
		if err != nil {
			return err
		}
		w.Price += 100
		return table.Update(ctx, w)
	})
	if err != nil {
		t.Fatalf("RunTx: %v", err)
	}

	got, _ := table.Get(ctx, "id = ?", 1)
	if got.Price != 105 {
		t.Fatalf("price = %d, want 105", got.Price)
	}
}
