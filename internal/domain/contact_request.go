package domain

import (
	"strings"
	"time"
	"unicode"
)

// The limits on a contact request's fields (D21). Name and email are the widths of their columns; a Turnstile
// token is at most Cloudflare's maximum.
const (
	// MaxContactNameLength is the most characters a contact request's name may have.
	MaxContactNameLength = 255
	// MaxContactEmailLength is the most characters a contact request's email address may have.
	MaxContactEmailLength = 254
	// MaxContactMessageLength is the most characters a contact request's message may have.
	MaxContactMessageLength = 5000
	// MaxTurnstileTokenLength is the most characters a Turnstile token may have.
	MaxTurnstileTokenLength = 2048
)

// The Turnstile settings of the contact form.
const (
	// TurnstileActionContact is the action the contact form's Turnstile widget sets (data-action), which a token
	// must carry to be accepted, so that a token minted for another form is refused.
	TurnstileActionContact = "contact"
	// MaxConcurrentTurnstileVerifications is how many Turnstile verifications may run at once. Each is an
	// outbound call of up to several seconds made for an anonymous request.
	MaxConcurrentTurnstileVerifications = 8
)

// ValidContactEmail reports whether s is shaped like an email address: local@domain, with neither part empty,
// one "@", and no spaces or control characters. Whether the address exists is not checked.
func ValidContactEmail(s string) bool {
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return false
	}

	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// ContactRequest is a message sent through the contact form, as moderators see it. Name, Email, Message and URL
// are personal data: they are nil once purged (Purged), ContactRetention after the request was resolved, and Name
// and URL are nil when the sender gave none.
type ContactRequest struct {
	ID         int64
	Kind       ContactRequestKind
	Name       *string
	Email      *string
	Message    *string
	URL        *string
	Status     ContactRequestStatus
	CreatedAt  time.Time
	ResolvedAt *time.Time
	ResolvedBy *ModerationAuthor
	Note       *string
	Purged     bool
}

// ContactRequestList is one page of contact requests, newest first, with how many there are in all.
type ContactRequestList struct {
	Requests []ContactRequest
	Total    int64
}
