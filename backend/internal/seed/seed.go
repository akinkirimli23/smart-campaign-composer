// Package seed generates a deterministic population of fake recipients.
//
// Generation is a pure function of (count, random seed, reference time), so a
// given configuration always yields the same users and events. Tests rely on
// this, and it makes audience counts reproducible across machines.
package seed

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID         int64
	FirstName  string
	LastName   string
	Email      *string
	Phone      *string
	City       string
	Language   string
	Gender     string
	BirthYear  int
	SignupAt   time.Time
	EmailOptIn bool
	SMSOptIn   bool
}

type Event struct {
	UserID     int64
	Type       string // cart_add | purchase
	Category   string
	Amount     *float64
	OccurredAt time.Time
}

// Categories is the closed set of product categories present in the data.
// The segment rule validator accepts only these values.
var Categories = []string{"electronics", "fashion", "cosmetics", "home", "sports", "books"}

// Cities with rough population-like weights.
var cityWeights = []struct {
	Name   string
	Weight int
}{
	{"İstanbul", 30}, {"Ankara", 12}, {"İzmir", 10}, {"Bursa", 7}, {"Antalya", 6},
	{"Adana", 5}, {"Konya", 5}, {"Gaziantep", 4}, {"Kocaeli", 4}, {"Mersin", 4},
	{"Eskişehir", 3}, {"Kayseri", 3}, {"Samsun", 3}, {"Trabzon", 2}, {"Diyarbakır", 2},
}

// Cities returns the closed set of city names present in the data.
func Cities() []string {
	out := make([]string, len(cityWeights))
	for i, c := range cityWeights {
		out[i] = c.Name
	}
	return out
}

var (
	trFemale = []string{"Ayşe", "Elif", "Zeynep", "Fatma", "Merve", "Selin", "Ebru", "Deniz", "Derya", "Gizem",
		"Büşra", "Cansu", "Esra", "İrem", "Melis", "Nazlı", "Özge", "Pınar", "Seda", "Tuğba", "Yasemin", "Ece", "Dilara", "Şule"}
	trMale = []string{"Mehmet", "Ahmet", "Mustafa", "Emre", "Burak", "Can", "Murat", "Kerem", "Onur", "Oğuz",
		"Serkan", "Tolga", "Umut", "Yusuf", "Barış", "Cem", "Eren", "Hakan", "İlker", "Kaan", "Okan", "Selim", "Volkan", "Çağrı"}
	trLast = []string{"Yılmaz", "Kaya", "Demir", "Şahin", "Çelik", "Yıldız", "Yıldırım", "Öztürk", "Aydın", "Özdemir",
		"Arslan", "Doğan", "Kılıç", "Aslan", "Çetin", "Kara", "Koç", "Kurt", "Özkan", "Şimşek", "Polat", "Erdoğan",
		"Güneş", "Aksoy", "Tekin", "Ünal", "Bulut", "Korkmaz", "Avcı", "Uçar"}
	enFemale = []string{"Emma", "Olivia", "Sophia", "Mia", "Grace", "Chloe", "Lucy", "Hannah"}
	enMale   = []string{"James", "Oliver", "Jack", "Henry", "Leo", "Daniel", "Thomas", "Noah"}
	enLast   = []string{"Smith", "Brown", "Taylor", "Wilson", "Evans", "Walker", "Clarke", "Hughes", "Wright", "Hall"}
)

// Generate builds count users and their events relative to now.
func Generate(count int, randomSeed int64, now time.Time) ([]User, []Event) {
	r := rand.New(rand.NewPCG(uint64(randomSeed), 0x5eed))
	now = now.UTC().Truncate(time.Second)

	totalWeight := 0
	for _, c := range cityWeights {
		totalWeight += c.Weight
	}
	pickCity := func() string {
		n := r.IntN(totalWeight)
		for _, c := range cityWeights {
			if n < c.Weight {
				return c.Name
			}
			n -= c.Weight
		}
		return cityWeights[0].Name
	}
	pick := func(xs []string) string { return xs[r.IntN(len(xs))] }

	users := make([]User, 0, count)
	events := make([]Event, 0, count*4)

	for i := 1; i <= count; i++ {
		u := User{ID: int64(i), City: pickCity()}

		u.Language = "tr"
		if r.Float64() < 0.10 {
			u.Language = "en"
		}
		u.Gender = "female"
		if r.IntN(2) == 0 {
			u.Gender = "male"
		}
		switch {
		case u.Language == "en" && u.Gender == "female":
			u.FirstName, u.LastName = pick(enFemale), pick(enLast)
		case u.Language == "en":
			u.FirstName, u.LastName = pick(enMale), pick(enLast)
		case u.Gender == "female":
			u.FirstName, u.LastName = pick(trFemale), pick(trLast)
		default:
			u.FirstName, u.LastName = pick(trMale), pick(trLast)
		}

		// ~5% of users have no email address, ~5% no phone number.
		if r.Float64() >= 0.05 {
			e := fmt.Sprintf("%s.%s.%d@example.com", asciiLower(u.FirstName), asciiLower(u.LastName), u.ID)
			u.Email = &e
		}
		if r.Float64() >= 0.05 {
			// Unique, deterministic Turkish mobile number derived from the id.
			p := fmt.Sprintf("+90%d%07d", 530+r.IntN(30), u.ID)
			u.Phone = &p
		}
		u.EmailOptIn = u.Email != nil && r.Float64() < 0.75
		u.SMSOptIn = u.Phone != nil && r.Float64() < 0.60

		u.BirthYear = 1960 + r.IntN(47) // 1960..2006
		u.SignupAt = now.Add(-time.Duration(1+r.IntN(1000)) * 24 * time.Hour).
			Add(-time.Duration(r.IntN(86400)) * time.Second)

		users = append(users, u)
		events = append(events, genEvents(r, u, now)...)
	}
	return users, events
}

// genEvents simulates ~120 days of shopping behaviour for one user.
func genEvents(r *rand.Rand, u User, now time.Time) []Event {
	if r.Float64() < 0.25 { // a quarter of users are dormant
		return nil
	}
	const window = 120 * 24 * time.Hour
	earliest := now.Add(-window)
	if u.SignupAt.After(earliest) {
		earliest = u.SignupAt
	}
	span := now.Sub(earliest)
	if span <= 0 {
		return nil
	}

	var out []Event
	carts := 1 + r.IntN(8)
	for range carts {
		at := earliest.Add(time.Duration(r.Int64N(int64(span))))
		cat := Categories[r.IntN(len(Categories))]
		out = append(out, Event{UserID: u.ID, Type: "cart_add", Category: cat, OccurredAt: at})

		// About a third of carts convert within three days.
		if r.Float64() < 0.35 {
			pAt := at.Add(time.Duration(r.Int64N(int64(72 * time.Hour))))
			if pAt.Before(now) {
				amt := float64(50+r.IntN(4950)) + float64(r.IntN(100))/100
				out = append(out, Event{UserID: u.ID, Type: "purchase", Category: cat, Amount: &amt, OccurredAt: pAt})
			}
		}
	}
	return out
}

var trFold = strings.NewReplacer("ç", "c", "ğ", "g", "ı", "i", "İ", "i", "ö", "o", "ş", "s", "ü", "u",
	"Ç", "c", "Ğ", "g", "Ö", "o", "Ş", "s", "Ü", "u")

func asciiLower(s string) string {
	return strings.ToLower(trFold.Replace(s))
}

// Load inserts the generated population unless users already exist.
// It returns false when seeding was skipped.
func Load(ctx context.Context, pool *pgxpool.Pool, count int, randomSeed int64, now time.Time) (bool, error) {
	var seeded bool
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		// Serialise concurrent seeders; the lock is released at commit.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(774412)`); err != nil {
			return err
		}
		var n int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			slog.Info("seed skipped, users already present", "users", n)
			return nil
		}

		users, events := Generate(count, randomSeed, now)

		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"users"},
			[]string{"id", "first_name", "last_name", "email", "phone", "city", "language", "gender",
				"birth_year", "signup_at", "email_opt_in", "sms_opt_in"},
			pgx.CopyFromSlice(len(users), func(i int) ([]any, error) {
				u := users[i]
				return []any{u.ID, u.FirstName, u.LastName, u.Email, u.Phone, u.City, u.Language, u.Gender,
					u.BirthYear, u.SignupAt, u.EmailOptIn, u.SMSOptIn}, nil
			}),
		); err != nil {
			return fmt.Errorf("copy users: %w", err)
		}
		if _, err := tx.Exec(ctx, `SELECT setval('users_id_seq', (SELECT max(id) FROM users))`); err != nil {
			return err
		}

		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"events"},
			[]string{"user_id", "event_type", "category", "amount", "occurred_at"},
			pgx.CopyFromSlice(len(events), func(i int) ([]any, error) {
				e := events[i]
				return []any{e.UserID, e.Type, e.Category, e.Amount, e.OccurredAt}, nil
			}),
		); err != nil {
			return fmt.Errorf("copy events: %w", err)
		}

		slog.Info("seeded database", "users", len(users), "events", len(events))
		seeded = true
		return nil
	})
	return seeded, err
}
