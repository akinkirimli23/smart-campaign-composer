package campaign

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
	Status   string     `json:"status"`
	Attempts int        `json:"attempts"`
	SentAt   *time.Time `json:"sent_at,omitempty"`
	// Error is the last failure reported for this message, if any. A message
	// that was retried and then delivered keeps none.
	Error string `json:"error,omitempty"`
}

// RecipientPage is one page of a campaign's recipients.
type RecipientPage struct {
	Recipients []Recipient `json:"recipients"`
	// Total is how many messages match the filter, not how many are on this page.
	Total  int `json:"total"`
	Offset int `json:"offset"`
}

const (
	DefaultRecipientPage = 50
	MaxRecipientPage     = 100
)

// ErrUnknownStatus is returned for a status filter that is not a message status.
var ErrUnknownStatus = errors.New("campaign: unknown message status")

// Recipients lists the messages of a campaign, the ones that need attention
// first: failed, skipped and cancelled before those still on their way, and
// delivered ones last. status narrows the list to one message status; ""
// means all. The list is read from the queue table itself, so it can never
// disagree with the counts on the dashboard.
func (s Store) Recipients(ctx context.Context, id int64, status string, limit, offset int) (RecipientPage, error) {
	if status != "" && !slices.Contains(Statuses, status) {
		return RecipientPage{}, ErrUnknownStatus
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

	// Recipients were frozen at approval, so a plain offset is stable enough:
	// rows only move between statuses, they are never added or removed.
	rows, err := s.DB.Query(ctx, `
SELECT m.user_id, u.first_name, u.last_name, m.channel, u.phone, u.email, u.language,
       m.status, m.attempts, m.sent_at, COALESCE(m.last_error, ''), count(*) OVER ()
FROM messages m
JOIN users u ON u.id = m.user_id
WHERE m.campaign_id = $1 AND ($2 = '' OR m.status = $2)
ORDER BY CASE m.status
           WHEN 'failed' THEN 0 WHEN 'skipped' THEN 1 WHEN 'cancelled' THEN 2
           WHEN 'sending' THEN 3 WHEN 'pending' THEN 4 ELSE 5
         END, m.id
LIMIT $3 OFFSET $4`, id, status, limit, offset)
	if err != nil {
		return RecipientPage{}, fmt.Errorf("list recipients: %w", err)
	}
	defer rows.Close()

	page := RecipientPage{Recipients: []Recipient{}, Offset: offset}
	for rows.Next() {
		var r Recipient
		var first, last string
		var phone, email *string
		if err := rows.Scan(&r.UserID, &first, &last, &r.Channel, &phone, &email, &r.Language,
			&r.Status, &r.Attempts, &r.SentAt, &r.Error, &page.Total); err != nil {
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
	if err := rows.Err(); err != nil {
		return RecipientPage{}, err
	}
	if len(page.Recipients) == 0 && offset > 0 {
		// Past the end: the window function saw no row, so count separately.
		err := s.DB.QueryRow(ctx,
			`SELECT count(*) FROM messages WHERE campaign_id = $1 AND ($2 = '' OR status = $2)`, id, status).Scan(&page.Total)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return RecipientPage{}, fmt.Errorf("count recipients: %w", err)
		}
	}
	return page, nil
}

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
