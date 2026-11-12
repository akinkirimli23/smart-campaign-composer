package segment

import (
	"encoding/json"
	"strings"
	"time"
)

// MaxScheduleAhead bounds how far in the future a campaign may be scheduled.
// The audience is frozen at approval (DECISIONS 27), so a distant send date
// means a stale audience; a month is already generous.
const MaxScheduleAhead = 30 * 24 * time.Hour

// localTimeLayout is how the model writes a send time: the marketer's wall
// clock, to the minute, without a zone. The zone is ours to apply, so the
// model never does offset arithmetic.
const localTimeLayout = "2006-01-02T15:04"

const (
	clockOpen  = "<local_time>"
	clockClose = "</local_time>"
)

// Clock is the marketer's "now": the instant, in their time zone. A zero
// Clock means no send time is looked for in the brief.
type Clock struct {
	Now time.Time
}

func (c Clock) known() bool { return !c.Now.IsZero() }

// promptLine tells the model what "tomorrow" or "Friday" mean.
func (c Clock) promptLine() string {
	return clockOpen + c.Now.Format(localTimeLayout) + " " + c.Now.Weekday().String() +
		" " + c.Now.Location().String() + clockClose
}

// ClockFromRequest recovers the clock from a generation request (used by the
// mock). ok is false when the request carried none.
func ClockFromRequest(text string) (Clock, bool) {
	start := strings.Index(text, clockOpen)
	end := strings.Index(text, clockClose)
	if start < 0 || end < start {
		return Clock{}, false
	}
	fields := strings.Fields(text[start+len(clockOpen) : end])
	if len(fields) != 3 {
		return Clock{}, false
	}
	loc, err := time.LoadLocation(fields[2])
	if err != nil {
		return Clock{}, false
	}
	now, err := time.ParseInLocation(localTimeLayout, fields[0], loc)
	if err != nil {
		return Clock{}, false
	}
	return Clock{Now: now}, true
}

// Schedule is a send time the model read in the brief. It is a suggestion:
// the UI pre-fills the schedule field with it and the marketer confirms or
// changes it. Nothing is ever scheduled from this alone (DECISIONS 28).
type Schedule struct {
	SendAt time.Time `json:"send_at"`
	// Phrase is the part of the brief the time was read from.
	Phrase string `json:"phrase"`
	// Interpretation says how an imprecise phrase was read ("sabah" -> 09:00).
	Interpretation string `json:"interpretation"`
}

// parseSchedule validates the model's schedule object. Problems are reported
// under result.schedule; a nil Schedule with no problems means "none given".
func parseSchedule(raw json.RawMessage, clock Clock) (*Schedule, ValidationError) {
	if raw == nil || isNull(raw) || !clock.known() {
		return nil, nil
	}
	const path = "result.schedule"
	var col collector
	obj, ok := object(&col, path, raw)
	if !ok {
		return nil, col.errs
	}
	checkKeys(&col, path, obj, []string{"local_time", "phrase", "interpretation"}, nil)
	local, _ := str(&col, path+".local_time", obj["local_time"])
	s := Schedule{}
	s.Phrase, _ = str(&col, path+".phrase", obj["phrase"])
	checkText(&col, path+".phrase", s.Phrase, maxShortText, obj["phrase"] != nil)
	s.Interpretation, _ = str(&col, path+".interpretation", obj["interpretation"])
	checkText(&col, path+".interpretation", s.Interpretation, maxLongText, obj["interpretation"] != nil)
	if len(col.errs) > 0 {
		return nil, col.errs
	}

	at, err := time.ParseInLocation(localTimeLayout, local, clock.Now.Location())
	switch {
	case err != nil:
		col.add(path+".local_time", "%q geçerli bir zaman değil; biçim YYYY-MM-DDTHH:MM olmalı (ör. %s)",
			local, clock.Now.Add(24*time.Hour).Format(localTimeLayout))
	case !at.After(clock.Now):
		col.add(path+".local_time", "%s geçmişte kalıyor (şu an %s); gelecekteki bir zaman verin ya da schedule alanını null yapın",
			local, clock.Now.Format(localTimeLayout))
	case at.After(clock.Now.Add(MaxScheduleAhead)):
		col.add(path+".local_time", "%s 30 günden daha ileride; en geç %s olabilir, yoksa schedule alanını null yapın",
			local, clock.Now.Add(MaxScheduleAhead).Format(localTimeLayout))
	}
	if len(col.errs) > 0 {
		return nil, col.errs
	}
	s.SendAt = at.UTC()
	return &s, nil
}
