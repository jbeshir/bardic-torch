package domain

import "time"

// The limits on the text of link reports and moderators' notes.
const (
	// MaxLinkReportMessageLength is the most characters a link report's message may have.
	MaxLinkReportMessageLength = 2000
	// MaxModeratorNoteLength is the most characters a moderator's review note or resolution note may have.
	MaxModeratorNoteLength = 2000
)

// LinkHiddenNote is the resolution note of the open reports of a link that a moderator hides.
const LinkHiddenNote = "Link hidden."

// LinkReportReceipt is what the reporter of a link learns of their report.
type LinkReportReceipt struct {
	ID        int64
	Status    LinkReportStatus
	CreatedAt time.Time
}

// LinkReport is a user's report of a link, as moderators see it: with the link as it is now and who reported it.
type LinkReport struct {
	ID         int64
	Link       ModerationLink
	Reason     LinkReportReason
	Message    *string
	Reporter   ModerationAuthor
	Status     LinkReportStatus
	CreatedAt  time.Time
	ResolvedAt *time.Time
	ResolvedBy *ModerationAuthor
	Note       *string
}

// LinkReportList is one page of link reports, oldest first, with how many there are in all.
type LinkReportList struct {
	Reports []LinkReport
	Total   int64
}
