package domain

import (
	"net/mail"
	"slices"
	"time"
)

// The limits on the text moderators and the public send.
const (
	// MaxReportMessageLength is the most characters a link report's message may have.
	MaxReportMessageLength = 2000
	// MaxNoteLength is the most characters a moderator's resolution note may have.
	MaxNoteLength = 2000
	// MaxContactNameLength is the most characters a contact request's name may have.
	MaxContactNameLength = 255
	// MaxContactEmailLength is the most characters a contact request's email address may have.
	MaxContactEmailLength = 254
	// MaxContactMessageLength is the most characters a contact request's message may have.
	MaxContactMessageLength = 5000
	// MaxTurnstileTokenLength is the most characters a Turnstile token may have.
	MaxTurnstileTokenLength = 2048
)

// TurnstileActionContact is the action the contact form's Turnstile widget sets, which a token must carry.
const TurnstileActionContact = "contact"

// ContactRetention is how long after a contact request is resolved its personal data is kept.
const ContactRetention = 180 * 24 * time.Hour

// ReportStatus is where a link report or contact request stands: open until a moderator resolves or dismisses it.
type ReportStatus string

// The statuses of link reports and contact requests.
const (
	ReportStatusOpen      ReportStatus = "open"
	ReportStatusResolved  ReportStatus = "resolved"
	ReportStatusDismissed ReportStatus = "dismissed"
)

// LinkReportReason is why a link was reported.
type LinkReportReason string

// The reasons for reporting a link.
const (
	LinkReportReasonBroken    LinkReportReason = "broken"
	LinkReportReasonWrongLink LinkReportReason = "wrong_link"
	LinkReportReasonPrivacy   LinkReportReason = "privacy"
	LinkReportReasonCopyright LinkReportReason = "copyright"
	LinkReportReasonLyrics    LinkReportReason = "lyrics"
	LinkReportReasonOther     LinkReportReason = "other"
)

// ContactKind is what a contact request is about.
type ContactKind string

// The kinds of contact request.
const (
	ContactKindTakedown   ContactKind = "takedown"
	ContactKindOptOut     ContactKind = "opt_out"
	ContactKindCorrection ContactKind = "correction"
	ContactKindGeneral    ContactKind = "general"
)

// Valid reports whether s is a known ReportStatus.
func (s ReportStatus) Valid() bool {
	return slices.Contains([]ReportStatus{ReportStatusOpen, ReportStatusResolved, ReportStatusDismissed}, s)
}

// Closes reports whether a moderator may set s on an open report: resolved or dismissed.
func (s ReportStatus) Closes() bool { return s == ReportStatusResolved || s == ReportStatusDismissed }

// Valid reports whether r is a known LinkReportReason.
func (r LinkReportReason) Valid() bool {
	return slices.Contains([]LinkReportReason{LinkReportReasonBroken, LinkReportReasonWrongLink,
		LinkReportReasonPrivacy, LinkReportReasonCopyright, LinkReportReasonLyrics, LinkReportReasonOther}, r)
}

// Valid reports whether k is a known ContactKind.
func (k ContactKind) Valid() bool {
	return slices.Contains([]ContactKind{ContactKindTakedown, ContactKindOptOut, ContactKindCorrection,
		ContactKindGeneral}, k)
}

// ValidContactEmail reports whether s is a bare email address, such as name@example.org.
func ValidContactEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

// ModerationUser is a user as moderators see them in a report.
type ModerationUser struct {
	UserID      string
	DisplayName string
}

// Resolution is how a moderator closed a report or contact request.
type Resolution struct {
	ResolvedBy string
	ResolvedAt time.Time
	Note       *string
}

// ReportedLink is the link of a link report.
type ReportedLink struct {
	ID       uint64
	URL      string
	IsHidden bool
}

// LinkReport is a user's report of a link.
type LinkReport struct {
	ID         uint64
	Link       ReportedLink
	Reporter   ModerationUser
	Reason     LinkReportReason
	Message    *string
	Status     ReportStatus
	CreatedAt  time.Time
	Resolution *Resolution
}

// LinkReportList is one page of link reports, with how many there are in all.
type LinkReportList struct {
	Reports []LinkReport
	Total   int64
}

// LinkAttachment is an entity a link is attached to, and the roles it has there.
type LinkAttachment struct {
	EntityType EntityType
	Slug       string
	Title      string
	Roles      []LinkRole
}

// ModerationLink is a link as a moderator takes it down: hidden or not, with where it is attached.
type ModerationLink struct {
	ID          uint64
	URL         string
	Title       *string
	IsHidden    bool
	Liveness    Liveness
	Attachments []LinkAttachment
}

// ContactRequest is a message sent through the contact form. Name, Email, Message and URL are personal data, nil
// once purged and, for Name and URL, when the sender gave none.
type ContactRequest struct {
	ID         uint64
	Kind       ContactKind
	Name       *string
	Email      *string
	Message    *string
	URL        *string
	Status     ReportStatus
	CreatedAt  time.Time
	Purged     bool
	Resolution *Resolution
}

// ContactRequestList is one page of contact requests, with how many there are in all.
type ContactRequestList struct {
	Requests []ContactRequest
	Total    int64
}
