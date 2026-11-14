package campaign

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/audience"
)

// Recipient is one message of a campaign as the marketer sees it: who, on
// which channel, and what became of it.
type Recipient struct {
	UserID  int64  `json:"user_id"`
	Name    string `json:"name"`
	Channel string `json:"channel"`
	// Address is masked: enough to recognise the recipient, not enough to
	// copy a phone number or an email address off the screen.
	Address  string     `json:"address"`
	Language string     `json:"language"`
	City     string     `json:"city"`
	Status   string     `json:"status"`
	Attempts int        `json:"attempts"`
	SentAt   *time.Time `json:"sent_at,omitempty"`
	// Error is the last failure reported for this message, if any. A message
	// that was retried and then delivered keeps none.
	Error string `json:"error,omitempty"`
	// Suppression says why this person will not be sent to on this channel
	// from now on, if they will not: "unsubscribed" or "invalid_address".
	Suppression string `json:"suppression,omitempty"`
}

// RecipientFilter narrows a recipient list. Empty fields do not filter.
type RecipientFilter struct {
	Status   string // a message status
	Channel  string // "sms" or "email"
	Language string // a user language code
	City     string
	// Query matches part of the recipient's name, ignoring case.
	Query string
	// Suppression keeps only recipients suppressed for this reason.
	Suppression string
}

// MaxRecipientQuery bounds the name search.
const MaxRecipientQuery = 60

// RecipientPage is one page of a campaign's recipients.
type RecipientPage struct {
	Recipients []Recipient `json:"recipients"`
	// Total is how many messages match the filter, not how many are on this page.
	Total  int `json:"total"`
	Offset int `json:"offset"`
	// ByStatus counts the messages that match everything in the filter except
	// its status, so the status buttons can show what each of them would list.
	ByStatus map[string]int `json:"by_status"`
}

const (
	DefaultRecipientPage = 50
	MaxRecipientPage     = 100
)

// ErrInvalidFilter is returned for a filter value outside its closed set.
var ErrInvalidFilter = errors.New("campaign: invalid recipient filter")

// Recipients lists the messages of a campaign, the ones that need attention
// first: failed, skipped and cancelled before those still on their way, and
// delivered ones last. The list is read from the queue table itself, so it
// can never disagree with the counts on the dashboard.
//
// Every filter value travels as a query parameter; none of them becomes part
// of the SQL text.
func (s Store) Recipients(ctx context.Context, id int64, f RecipientFilter, limit, offset int) (RecipientPage, error) {
	f.Query = strings.TrimSpace(f.Query)
	switch {
	case f.Status != "" && !slices.Contains(Statuses, f.Status),
		f.Channel != "" && !slices.Contains(Channels, f.Channel),
		f.Suppression != "" && f.Suppression != audience.ReasonUnsubscribed && f.Suppression != audience.ReasonInvalidAddress,
		utf8.RuneCountInString(f.Query) > MaxRecipientQuery,
		utf8.RuneCountInString(f.City) > MaxRecipientQuery,
		utf8.RuneCountInString(f.Language) > 8:
		return RecipientPage{}, ErrInvalidFilter
	}
	if limit <= 0 {
		limit = DefaultRecipientPage
	}
	limit = min(limit, MaxRecipientPage)
	offset = max(offset, 0)

	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM campaigns WHERE id = $1)`, id).Scan(&exists); err != nil {
		return RecipientPage{}, fmt.Errorf("load campaign: %w", err)
	}
	if !exists {
		return RecipientPage{}, ErrNotFound
	}

	// $1 campaign, $2 channel, $3 language, $4 city, $5 name pattern, $6
	// suppression reason. The pattern's own wildcards are escaped, so "%" in
	// a search is just a character.
	const who = `
FROM messages m
JOIN users u ON u.id = m.user_id
LEFT JOIN suppressions sp ON sp.user_id = m.user_id AND sp.channel = m.channel
WHERE m.campaign_id = $1
  AND ($2 = '' OR m.channel = $2)
  AND ($3 = '' OR u.language = $3)
  AND ($4 = '' OR u.city = $4)
  AND ($5 = '' OR (u.first_name || ' ' || u.last_name) ILIKE $5)
  AND ($6 = '' OR sp.reason = $6)`
	pattern := ""
	if f.Query != "" {
		pattern = "%" + likeEscaper.Replace(f.Query) + "%"
	}
	args := []any{id, f.Channel, f.Language, f.City, pattern, f.Suppression}

	page := RecipientPage{Recipients: []Recipient{}, Offset: offset, ByStatus: map[string]int{}}
	counts, err := s.DB.Query(ctx, `SELECT m.status, count(*)`+who+` GROUP BY m.status`, args...)
	if err != nil {
		return RecipientPage{}, fmt.Errorf("count recipients: %w", err)
	}
	for counts.Next() {
		var status string
		var n int
		if err := counts.Scan(&status, &n); err != nil {
			counts.Close()
			return RecipientPage{}, err
		}
		page.ByStatus[status] = n
		if f.Status == "" || f.Status == status {
			page.Total += n
		}
	}
	counts.Close()
	if err := counts.Err(); err != nil {
		return RecipientPage{}, err
	}

	// Recipients were frozen at approval, so a plain offset is stable enough:
	// rows only move between statuses, they are never added or removed.
	rows, err := s.DB.Query(ctx, `
SELECT m.user_id, u.first_name, u.last_name, m.channel, u.phone, u.email, u.language, u.city,
       m.status, m.attempts, m.sent_at, COALESCE(m.last_error, ''), COALESCE(sp.reason, '')`+who+`
  AND ($7 = '' OR m.status = $7)
ORDER BY CASE m.status
           WHEN 'failed' THEN 0 WHEN 'skipped' THEN 1 WHEN 'cancelled' THEN 2
           WHEN 'sending' THEN 3 WHEN 'pending' THEN 4 ELSE 5
         END, m.id
LIMIT $8 OFFSET $9`, append(args, f.Status, limit, offset)...)
	if err != nil {
		return RecipientPage{}, fmt.Errorf("list recipients: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r Recipient
		var first, last string
		var phone, email *string
		if err := rows.Scan(&r.UserID, &first, &last, &r.Channel, &phone, &email, &r.Language, &r.City,
			&r.Status, &r.Attempts, &r.SentAt, &r.Error, &r.Suppression); err != nil {
			return RecipientPage{}, err
		}
		r.Name = first + " " + last
		if r.Channel == ChannelSMS && phone != nil {
			r.Address = MaskPhone(*phone)
		} else if r.Channel == ChannelEmail && email != nil {
			r.Address = MaskEmail(*email)
		}
		page.Recipients = append(page.Recipients, r)
	}
	return page, rows.Err()
}

// ErrNotDelivered is returned when someone "unsubscribes" from a message that
// was never delivered to them.
var ErrNotDelivered = errors.New("campaign: no delivered message for this recipient")

// Unsubscribe records that a recipient asked not to be contacted on a channel
// any more (DECISIONS 37). It is accepted only as a reaction to a message of
// ours that was delivered: the campaign, the person and the channel must
// name a sent message. From then on reachability (audience.SMSReachable /
// EmailReachable) leaves them out: later campaigns do not queue a message for
// them on that channel, and one already queued is skipped at send time.
//
// The other channel is not affected. Unsubscribing twice changes nothing; it
// reports whether this call was the one that did it.
func (s Store) Unsubscribe(ctx context.Context, campaignID, userID int64, channel string, now time.Time) (bool, error) {
	if !slices.Contains(Channels, channel) {
		return false, ErrInvalidFilter
	}
	var delivered bool
	err := s.DB.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM messages WHERE campaign_id = $1 AND user_id = $2 AND channel = $3 AND status = 'sent')`,
		campaignID, userID, channel).Scan(&delivered)
	if err != nil {
		return false, fmt.Errorf("load message: %w", err)
	}
	if !delivered {
		return false, ErrNotDelivered
	}
	tag, err := s.DB.Exec(ctx, `
INSERT INTO suppressions (user_id, channel, reason, campaign_id, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, channel) DO NOTHING`, userID, channel, audience.ReasonUnsubscribed, campaignID, now.UTC())
	if err != nil {
		return false, fmt.Errorf("unsubscribe: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

const maskRune = "•"

// MaskPhone keeps the country code and the last two digits: "+90•••••••••67".
func MaskPhone(phone string) string {
	runes := []rune(phone)
	if len(runes) <= 5 {
		return strings.Repeat(maskRune, len(runes))
	}
	return string(runes[:3]) + strings.Repeat(maskRune, len(runes)-5) + string(runes[len(runes)-2:])
}

// MaskEmail keeps the first letter and the domain: "a•••@example.com".
func MaskEmail(email string) string {
	local, domain, found := strings.Cut(email, "@")
	if !found || local == "" {
		return strings.Repeat(maskRune, 3)
	}
	first := []rune(local)[:1]
	return string(first) + strings.Repeat(maskRune, 3) + "@" + domain
}
