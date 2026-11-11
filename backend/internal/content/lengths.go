package content

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Querier is the part of pgxpool.Pool the length lookup needs.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DBLengths measures the longest value of every variable over all users
// (DECISIONS 19). That is an upper bound for any audience, so content that
// passes validation stays valid whichever segment it is sent to.
//
// The result is cached briefly: the query scans the users table and the
// answer only changes when users are added.
type DBLengths struct {
	DB  Querier
	TTL time.Duration // 0 means one minute

	mu      sync.Mutex
	cached  VarLengths
	expires time.Time
}

func (d *DBLengths) MaxLengths(ctx context.Context) (VarLengths, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cached != nil && time.Now().Before(d.expires) {
		return d.cached, nil
	}

	// Column names come from the Variables table (package constants), never
	// from input. char_length counts characters, like the validator counts runes.
	cols := make([]string, len(Variables))
	dest := make([]any, len(Variables))
	lengths := make([]int, len(Variables))
	for i, v := range Variables {
		cols[i] = "COALESCE(max(char_length(" + v.Column + ")), 0)"
		dest[i] = &lengths[i]
	}
	sql := "SELECT " + strings.Join(cols, ", ") + " FROM users"
	if err := d.DB.QueryRow(ctx, sql).Scan(dest...); err != nil {
		return nil, fmt.Errorf("measure variable lengths: %w", err)
	}

	out := make(VarLengths, len(Variables))
	for i, v := range Variables {
		out[v.Name] = lengths[i]
	}
	ttl := d.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	d.cached, d.expires = out, time.Now().Add(ttl)
	return out, nil
}
