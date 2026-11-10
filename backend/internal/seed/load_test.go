package seed_test

import (
	"context"
	"testing"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/seed"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/testdb"
)

func TestLoadIsIdempotent(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := refNowForLoad

	seeded, err := seed.Load(ctx, pool, 300, 7, now)
	if err != nil || !seeded {
		t.Fatalf("first load: seeded=%v err=%v", seeded, err)
	}
	seeded, err = seed.Load(ctx, pool, 300, 7, now)
	if err != nil || seeded {
		t.Fatalf("second load must be a no-op: seeded=%v err=%v", seeded, err)
	}

	users, events := seed.Generate(300, 7, now)
	var nUsers, nEvents int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM events)`).
		Scan(&nUsers, &nEvents); err != nil {
		t.Fatal(err)
	}
	if nUsers != len(users) || nEvents != len(events) {
		t.Fatalf("db has %d users / %d events, generator produced %d / %d",
			nUsers, nEvents, len(users), len(events))
	}

	// The id sequence must continue after the explicit ids we copied in.
	var next int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (first_name,last_name,city,language,gender,birth_year,signup_at)
		 VALUES ('A','B','Ankara','tr','male',1990,now()) RETURNING id`).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next != int64(len(users))+1 {
		t.Fatalf("next id = %d, want %d", next, len(users)+1)
	}
}

var refNowForLoad = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
