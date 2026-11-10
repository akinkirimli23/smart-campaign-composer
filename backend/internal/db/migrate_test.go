package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/db"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/testdb"
)

func TestMigrateIsIdempotent(t *testing.T) {
	pool := testdb.New(t) // already migrated once
	ctx := context.Background()

	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()
	if before == 0 {
		t.Fatal("no migrations recorded")
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if after := count(); after != before {
		t.Fatalf("second Migrate changed schema_migrations: %d -> %d rows", before, after)
	}
}

func TestEventAmountMatchesType(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	var userID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (first_name, last_name, city, language, gender, birth_year, signup_at)
		 VALUES ('A', 'B', 'Ankara', 'tr', 'male', 1990, now()) RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	amount := 120.50
	tests := []struct {
		name    string
		typ     string
		amount  *float64
		allowed bool
	}{
		{"purchase with amount", "purchase", &amount, true},
		{"purchase without amount", "purchase", nil, false},
		{"cart_add without amount", "cart_add", nil, true},
		{"cart_add with amount", "cart_add", &amount, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(ctx,
				`INSERT INTO events (user_id, event_type, category, amount, occurred_at)
				 VALUES ($1, $2, 'books', $3, now())`, userID, tt.typ, tt.amount)
			if tt.allowed {
				if err != nil {
					t.Fatalf("insert rejected: %v", err)
				}
				return
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "events_amount_iff_purchase" {
				t.Fatalf("want check_violation on events_amount_iff_purchase, got %v", err)
			}
		})
	}
}
