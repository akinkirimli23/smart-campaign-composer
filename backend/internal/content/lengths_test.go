package content_test

import (
	"context"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/seed"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/testdb"
)

var refNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

// The database must count characters the way the validator does (runes), so
// the lengths are compared with a Go computation over the same seed data.
func TestDBLengthsMatchSeed(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	lengths := &content.DBLengths{DB: pool, TTL: time.Hour}
	empty, err := (&content.DBLengths{DB: pool}).MaxLengths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range content.Variables {
		if n, ok := empty[v.Name]; !ok || n != 0 {
			t.Errorf("empty table: %s = %d, %v", v.Name, n, ok)
		}
	}

	const n, randomSeed = 5000, 42
	if _, err := seed.Load(ctx, pool, n, randomSeed, refNow); err != nil {
		t.Fatal(err)
	}
	users, _ := seed.Generate(n, randomSeed, refNow)
	want := content.VarLengths{}
	bytesLonger := false
	for _, u := range users {
		for name, value := range map[string]string{"first_name": u.FirstName, "last_name": u.LastName, "city": u.City} {
			want[name] = max(want[name], utf8.RuneCountInString(value))
			bytesLonger = bytesLonger || len(value) > utf8.RuneCountInString(value)
		}
	}
	if !bytesLonger {
		t.Fatal("seed has no non-ASCII names; the test would not tell bytes from characters")
	}

	got, err := lengths.MaxLengths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range content.Variables {
		if got[v.Name] != want[v.Name] || got[v.Name] == 0 {
			t.Errorf("%s: db says %d, seed says %d", v.Name, got[v.Name], want[v.Name])
		}
	}

	// Cached: a later change is not seen until the TTL passes.
	if _, err := pool.Exec(ctx, "UPDATE users SET first_name = repeat('x', 40) WHERE id = (SELECT min(id) FROM users)"); err != nil {
		t.Fatal(err)
	}
	cached, _ := lengths.MaxLengths(ctx)
	if cached["first_name"] != want["first_name"] {
		t.Errorf("cache not used: %d", cached["first_name"])
	}
	fresh, _ := (&content.DBLengths{DB: pool}).MaxLengths(ctx)
	if fresh["first_name"] != 40 {
		t.Errorf("fresh lookup = %d, want 40", fresh["first_name"])
	}
}
