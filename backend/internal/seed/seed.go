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
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/db"
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
	// LoyaltyTier is one of LoyaltyTiers, derived from the user's purchases.
	LoyaltyTier string
}

// LoyaltyTiers is the closed set of loyalty tiers, lowest first. The segment
// rule validator accepts only these values (DECISIONS 45).
var LoyaltyTiers = []string{"bronze", "silver", "gold"}

// Total purchase amount, in TL, from which a user is silver or gold. With
// the default seed about three in ten users are silver and one in ten gold.
const (
	SilverSpend = 3000
	GoldSpend   = 8000
)

// TierFor returns the loyalty tier of a user who has spent this much, in
// kuruş. The tier is computed from the generated purchases, so it draws
// nothing from the random source: adding it left every other value of the
// seed, and the reference counts that depend on them, as they were.
func TierFor(spentKurus int64) string {
	switch {
	case spentKurus >= GoldSpend*100:
		return "gold"
	case spentKurus >= SilverSpend*100:
		return "silver"
	}
	return "bronze"
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

// otherProvinces are Turkey's remaining 66 provinces, in alphabetical order.
// Together with cityWeights they make all 81 (DECISIONS 39).
var otherProvinces = []string{
	"Adıyaman", "Afyonkarahisar", "Ağrı", "Aksaray", "Amasya", "Ardahan", "Artvin", "Aydın", "Balıkesir", "Bartın",
	"Batman", "Bayburt", "Bilecik", "Bingöl", "Bitlis", "Bolu", "Burdur", "Çanakkale", "Çankırı", "Çorum",
	"Denizli", "Düzce", "Edirne", "Elazığ", "Erzincan", "Erzurum", "Giresun", "Gümüşhane", "Hakkari", "Hatay",
	"Iğdır", "Isparta", "Kahramanmaraş", "Karabük", "Karaman", "Kars", "Kastamonu", "Kilis", "Kırıkkale", "Kırklareli",
	"Kırşehir", "Kütahya", "Malatya", "Manisa", "Mardin", "Muğla", "Muş", "Nevşehir", "Niğde", "Ordu",
	"Osmaniye", "Rize", "Sakarya", "Sinop", "Sivas", "Siirt", "Şanlıurfa", "Şırnak", "Tekirdağ", "Tokat",
	"Tunceli", "Uşak", "Van", "Yalova", "Yozgat", "Zonguldak",
}

// resettledPercent is the share of people outside İstanbul who live in one of
// the other provinces instead of the big city the weighted draw gave them.
const resettledPercent = 22

// Cities returns the closed set of city names present in the data: the
// fifteen large cities first, then the other provinces.
func Cities() []string {
	out := make([]string, 0, len(cityWeights)+len(otherProvinces))
	for _, c := range cityWeights {
		out = append(out, c.Name)
	}
	return append(out, otherProvinces...)
}

// resettle moves some users to one of the other provinces.
//
// It is decided from the user's id and the random seed alone, without
// drawing from the generator: every other attribute of every user stays
// exactly what it was when the data had fifteen cities. İstanbul is left
// untouched, so the reference numbers of the example brief (381 / 222 / 254 /
// 58) are the same as before.
func resettle(city string, id, randomSeed int64) string {
	if city == cityWeights[0].Name {
		return city
	}
	// splitmix64: a well-mixed number from the two inputs.
	h := uint64(id)*0x9E3779B97F4A7C15 + uint64(randomSeed)
	h ^= h >> 30
	h *= 0xBF58476D1CE4E5B9
	h ^= h >> 27
	h *= 0x94D049BB133111EB
	h ^= h >> 31
	if h%100 >= resettledPercent {
		return city
	}
	return otherProvinces[(h/100)%uint64(len(otherProvinces))]
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
		u.City = resettle(u.City, u.ID, randomSeed)

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

		own := genEvents(r, u, now)
		var spent int64
		for _, e := range own {
			if e.Type == "purchase" && e.Amount != nil {
				spent += int64(math.Round(*e.Amount * 100))
			}
		}
		u.LoyaltyTier = TierFor(spent)

		users = append(users, u)
		events = append(events, own...)
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
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, db.SeedLockID); err != nil {
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
				"birth_year", "signup_at", "email_opt_in", "sms_opt_in", "loyalty_tier"},
			pgx.CopyFromSlice(len(users), func(i int) ([]any, error) {
				u := users[i]
				return []any{u.ID, u.FirstName, u.LastName, u.Email, u.Phone, u.City, u.Language, u.Gender,
					u.BirthYear, u.SignupAt, u.EmailOptIn, u.SMSOptIn, u.LoyaltyTier}, nil
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
