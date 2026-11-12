package mockllm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
)

// The mock reads a send time only from a few fixed shapes: "yarın",
// "N gün sonra", a weekday, or a day and month, optionally with an hour
// ("saat 10", "09:30") or a part of the day. A real model reads free text;
// this is enough to show the flow offline.

var (
	inDaysRe  = regexp.MustCompile(`\b(\d{1,2}) gun sonra\b`)
	tomorrow  = regexp.MustCompile(`\byarin\b`)
	weekdayRe = regexp.MustCompile(`\b(pazartesi|sali|carsamba|persembe|cumartesi|cuma|pazar)(ya|ye|a|e)?\b`)
	dateRe    = regexp.MustCompile(`\b(\d{1,2}) (ocak|subat|mart|nisan|mayis|haziran|temmuz|agustos|eylul|ekim|kasim|aralik)\b`)
	hourRe    = regexp.MustCompile(`\bsaat (\d{1,2})(?:[:.](\d{2}))?|\b(\d{1,2}):(\d{2})\b`)

	weekdays = map[string]time.Weekday{
		"pazar": time.Sunday, "pazartesi": time.Monday, "sali": time.Tuesday, "carsamba": time.Wednesday,
		"persembe": time.Thursday, "cuma": time.Friday, "cumartesi": time.Saturday,
	}
	months = []string{"ocak", "subat", "mart", "nisan", "mayis", "haziran", "temmuz", "agustos", "eylul", "ekim", "kasim", "aralik"}

	monthNames = []string{"Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran", "Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık"}
	dayNames   = []string{"Pazar", "Pazartesi", "Salı", "Çarşamba", "Perşembe", "Cuma", "Cumartesi"}
)

// scheduleResult returns the "schedule" value of a rule reply (nil for none)
// and the folded brief with the day phrase blanked out, so "3 gün sonra" is
// not also read as a three-day look-back window.
func scheduleResult(brief, folded string, clock segment.Clock) (any, string) {
	now := clock.Now
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	var day time.Time
	var cue string
	switch {
	case inDaysRe.MatchString(folded):
		m := inDaysRe.FindStringSubmatch(folded)
		n, _ := strconv.Atoi(m[1])
		day, cue = today.AddDate(0, 0, n), m[0]
	case tomorrow.MatchString(folded):
		day, cue = today.AddDate(0, 0, 1), "yarin"
	case dateRe.MatchString(folded):
		m := dateRe.FindStringSubmatch(folded)
		d, _ := strconv.Atoi(m[1])
		month := time.January
		for i, name := range months {
			if name == m[2] {
				month = time.Month(i + 1)
			}
		}
		day, cue = time.Date(now.Year(), month, d, 0, 0, 0, 0, now.Location()), m[0]
		if day.Before(today) {
			day = day.AddDate(1, 0, 0)
		}
	// "Kara Cuma" is a sale, not a send day.
	case weekdayRe.MatchString(folded) && !strings.Contains(folded, "kara cuma"):
		m := weekdayRe.FindStringSubmatch(folded)
		ahead := (int(weekdays[m[1]]) - int(today.Weekday()) + 7) % 7
		if ahead == 0 {
			ahead = 7 // the next one, not today
		}
		day, cue = today.AddDate(0, 0, ahead), m[1]
	default:
		return nil, folded
	}
	cleaned := strings.Replace(folded, cue, strings.Repeat(" ", len(cue)), 1)

	hour, minute, assumed := 10, 0, "saat belirtilmediği için 10:00 varsayıldı"
	if m := hourRe.FindStringSubmatch(folded); m != nil {
		h, mm := m[1], m[2]
		if h == "" {
			h, mm = m[3], m[4]
		}
		hour, _ = strconv.Atoi(h)
		minute, _ = strconv.Atoi(mm)
		assumed = ""
	} else {
		for word, at := range map[string]int{"sabah": 9, "ogle": 12, "aksam": 18} {
			if strings.Contains(folded, word) {
				hour, assumed = at, fmt.Sprintf("%q için %02d:00 varsayıldı", word, at)
			}
		}
	}
	if hour > 23 || minute > 59 {
		return nil, folded
	}
	at := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, now.Location())
	// Outside what the validator accepts: say nothing rather than be rejected.
	if !at.After(now) || at.After(now.Add(segment.MaxScheduleAhead)) {
		return nil, folded
	}

	interpretation := fmt.Sprintf("%d %s %d %s %02d:%02d olarak yorumlandı", at.Day(), monthNames[at.Month()-1], at.Year(),
		dayNames[at.Weekday()], at.Hour(), at.Minute())
	if assumed != "" {
		interpretation += "; " + assumed
	}
	return map[string]any{
		"local_time":     at.Format("2006-01-02T15:04"),
		"phrase":         original(brief, folded, cue),
		"interpretation": interpretation + ".",
	}, cleaned
}
