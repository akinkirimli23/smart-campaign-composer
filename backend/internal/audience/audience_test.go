package audience_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/audience"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/seed"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/testdb"
)

// The data is generated relative to refNow and queried as of refNow, so these
// numbers do not depend on when the test runs.
var refNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

var exampleRule = segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
	{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"İstanbul"}},
	{Type: segment.TypeEvent, Event: "cart_add", WithinDays: 30, Exists: true},
	{Type: segment.TypeEvent, Event: "purchase", WithinDays: 30, Exists: false},
}}

// The example brief on the default seed: the reference numbers of CLAUDE.md rule 8.
func TestExampleBriefCounts(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, 5000, 42, refNow); err != nil {
		t.Fatal(err)
	}
	got, err := audience.Counter{DB: pool}.Count(ctx, exampleRule, refNow)
	if err != nil {
		t.Fatal(err)
	}
	want := audience.Counts{Total: 381, SMS: 222, Email: 254, Unreachable: 58}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// The compiled SQL must agree with a straightforward Go evaluation of the same
// rule over the same generated data, for every condition variant and both
// match modes.
func TestCountsMatchReferenceEvaluation(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	const n, randomSeed = 1500, 7
	if _, err := seed.Load(ctx, pool, n, randomSeed, refNow); err != nil {
		t.Fatal(err)
	}
	users, events := seed.Generate(n, randomSeed, refNow)
	byUser := map[int64][]seed.Event{}
	for _, e := range events {
		byUser[e.UserID] = append(byUser[e.UserID], e)
	}

	cond := func(c ...segment.Condition) []segment.Condition { return c }
	rules := map[string]segment.Rule{
		"example": exampleRule,
		"city not_in": {Match: segment.MatchAll, Conditions: cond(
			segment.Condition{Type: segment.TypeAttribute, Field: "city", Op: segment.OpNotIn, Values: []string{"İstanbul", "Ankara"}})},
		"language and gender": {Match: segment.MatchAll, Conditions: cond(
			segment.Condition{Type: segment.TypeAttribute, Field: "language", Op: segment.OpIn, Values: []string{"en"}},
			segment.Condition{Type: segment.TypeAttribute, Field: "gender", Op: segment.OpIn, Values: []string{"female"}})},
		"age": {Match: segment.MatchAll, Conditions: cond(
			segment.Condition{Type: segment.TypeAttribute, Field: segment.FieldAge, Op: segment.OpBetween, Min: 25, Max: 34})},
		"category purchase": {Match: segment.MatchAll, Conditions: cond(
			segment.Condition{Type: segment.TypeEvent, Event: "purchase", Category: "cosmetics", WithinDays: 60, Exists: true})},
		"lapsed": {Match: segment.MatchAll, Conditions: cond(
			segment.Condition{Type: segment.TypeEvent, Event: "purchase", WithinDays: 90, Exists: true},
			segment.Condition{Type: segment.TypeEvent, Event: "purchase", WithinDays: 30, Exists: false})},
		"any": {Match: segment.MatchAny, Conditions: cond(
			segment.Condition{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"Trabzon"}},
			segment.Condition{Type: segment.TypeEvent, Event: "cart_add", Category: "books", WithinDays: 7, Exists: true},
			segment.Condition{Type: segment.TypeEvent, Event: "purchase", WithinDays: 120, Exists: false})},
	}

	counter := audience.Counter{DB: pool}
	for name, rule := range rules {
		t.Run(name, func(t *testing.T) {
			var want audience.Counts
			for _, u := range users {
				if !matches(rule, u, byUser[u.ID], refNow) {
					continue
				}
				want.Total++
				sms := u.SMSOptIn && u.Phone != nil
				email := u.EmailOptIn && u.Email != nil
				if sms {
					want.SMS++
				}
				if email {
					want.Email++
				}
				if !sms && !email {
					want.Unreachable++
				}
			}
			got, err := counter.Count(ctx, rule, refNow)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("SQL %+v, reference %+v", got, want)
			}
			if want.Total == 0 || want.Total == n {
				t.Fatalf("rule selects %d of %d users; the comparison proves nothing", want.Total, n)
			}
		})
	}
}

// matches is the reference semantics of a rule, written without SQL.
func matches(r segment.Rule, u seed.User, events []seed.Event, asOf time.Time) bool {
	hit := 0
	for _, c := range r.Conditions {
		var ok bool
		switch {
		case c.Type == segment.TypeEvent:
			from := asOf.AddDate(0, 0, -c.WithinDays)
			found := slices.ContainsFunc(events, func(e seed.Event) bool {
				return e.Type == c.Event && !e.OccurredAt.Before(from) && !e.OccurredAt.After(asOf) &&
					(c.Category == "" || e.Category == c.Category)
			})
			ok = found == c.Exists
		case c.Field == segment.FieldAge:
			age := asOf.Year() - u.BirthYear
			ok = age >= c.Min && age <= c.Max
		default:
			value := map[string]string{"city": u.City, "language": u.Language, "gender": u.Gender}[c.Field]
			ok = slices.Contains(c.Values, value) == (c.Op == segment.OpIn)
		}
		if ok {
			hit++
		}
	}
	if r.Match == segment.MatchAny {
		return hit > 0
	}
	return hit == len(r.Conditions)
}

// An invalid rule never reaches the database.
func TestCountRejectsInvalidRule(t *testing.T) {
	bad := segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
		{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"İstanbul'; DROP TABLE users;--"}}}}
	_, err := audience.Counter{DB: nil}.Count(context.Background(), bad, refNow) // nil DB: must fail before querying
	if _, ok := err.(segment.ValidationError); !ok {
		t.Fatalf("want ValidationError, got %v", err)
	}
}
