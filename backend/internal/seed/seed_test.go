package seed

import (
	"reflect"
	"testing"
	"time"
)

var refNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func TestGenerateIsDeterministic(t *testing.T) {
	u1, e1 := Generate(500, 42, refNow)
	u2, e2 := Generate(500, 42, refNow)
	if !reflect.DeepEqual(u1, u2) || !reflect.DeepEqual(e1, e2) {
		t.Fatal("same inputs produced different populations")
	}
	u3, _ := Generate(500, 43, refNow)
	if reflect.DeepEqual(u1, u3) {
		t.Fatal("different seeds produced identical populations")
	}
}

func TestGenerateInvariants(t *testing.T) {
	users, events := Generate(5000, 42, refNow)
	if len(users) != 5000 {
		t.Fatalf("got %d users", len(users))
	}

	emails, phones := map[string]bool{}, map[string]bool{}
	byID := map[int64]User{}
	var istanbul, emailOpt, smsOpt int
	for _, u := range users {
		byID[u.ID] = u
		if u.Email != nil {
			if emails[*u.Email] {
				t.Fatalf("duplicate email %s", *u.Email)
			}
			emails[*u.Email] = true
		}
		if u.Phone != nil {
			if phones[*u.Phone] {
				t.Fatalf("duplicate phone %s", *u.Phone)
			}
			phones[*u.Phone] = true
		}
		// Consent without an address would let the sender pick a user it cannot reach.
		if u.EmailOptIn && u.Email == nil {
			t.Fatalf("user %d opted in to email without an address", u.ID)
		}
		if u.SMSOptIn && u.Phone == nil {
			t.Fatalf("user %d opted in to sms without a phone", u.ID)
		}
		if u.City == "İstanbul" {
			istanbul++
		}
		if u.EmailOptIn {
			emailOpt++
		}
		if u.SMSOptIn {
			smsOpt++
		}
	}

	// Loose distribution checks: catch a broken generator, not exact numbers.
	assertBetween(t, "istanbul share", float64(istanbul)/5000, 0.25, 0.35)
	assertBetween(t, "email opt-in share", float64(emailOpt)/5000, 0.65, 0.78)
	assertBetween(t, "sms opt-in share", float64(smsOpt)/5000, 0.50, 0.63)

	cats := map[string]bool{}
	for _, c := range Categories {
		cats[c] = true
	}
	for _, e := range events {
		u, ok := byID[e.UserID]
		if !ok {
			t.Fatalf("event for unknown user %d", e.UserID)
		}
		if e.OccurredAt.After(refNow) {
			t.Fatalf("event in the future: %v", e.OccurredAt)
		}
		if e.OccurredAt.Before(u.SignupAt) {
			t.Fatalf("event before signup for user %d", u.ID)
		}
		if !cats[e.Category] {
			t.Fatalf("unknown category %q", e.Category)
		}
		if (e.Type == "purchase") != (e.Amount != nil) {
			t.Fatalf("amount must be set exactly for purchases: %+v", e)
		}
	}
}

func assertBetween(t *testing.T, name string, v, lo, hi float64) {
	t.Helper()
	if v < lo || v > hi {
		t.Errorf("%s = %.3f, want between %.2f and %.2f", name, v, lo, hi)
	}
}

// All 81 provinces are in the closed set, each once, and the data reaches
// beyond the fifteen large cities without touching İstanbul.
func TestProvinces(t *testing.T) {
	cities := Cities()
	seen := map[string]bool{}
	for _, c := range cities {
		if seen[c] {
			t.Errorf("%s listed twice", c)
		}
		seen[c] = true
	}
	if len(cities) != 81 || cities[0] != "İstanbul" {
		t.Fatalf("%d cities, first %q", len(cities), cities[0])
	}

	users, _ := Generate(5000, 42, refNow)
	perCity := map[string]int{}
	for _, u := range users {
		if !seen[u.City] {
			t.Fatalf("user %d lives in %q, which is not in the set", u.ID, u.City)
		}
		perCity[u.City]++
	}
	if len(perCity) != 81 {
		t.Errorf("users live in %d provinces, want all 81", len(perCity))
	}
	// İstanbul keeps exactly the people the fifteen-city data gave it; the
	// example brief's reference numbers depend on that.
	if perCity["İstanbul"] != 1516 {
		t.Errorf("%d users in İstanbul, want 1516", perCity["İstanbul"])
	}
	other := 0
	for _, p := range otherProvinces {
		other += perCity[p]
	}
	if share := float64(other) / float64(len(users)-perCity["İstanbul"]); share < 0.17 || share > 0.27 {
		t.Errorf("%.0f%% of the users outside İstanbul live in the other provinces", share*100)
	}
	// Decided from the id and the seed, so it is the same on every run.
	if a, b := resettle("Ankara", 17, 42), resettle("Ankara", 17, 42); a != b {
		t.Errorf("resettle is not deterministic: %q, %q", a, b)
	}
	if resettle("İstanbul", 17, 42) != "İstanbul" {
		t.Error("someone was moved out of İstanbul")
	}
}
