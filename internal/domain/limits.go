package domain

import "time"

// The caps on what one user, or the public, may leave open at once. They bound how much unreviewed data and how
// many placeholder entities a single account can create.
const (
	// MaxOpenPendingRevisions is how many pending revisions one user may have at once.
	MaxOpenPendingRevisions = 30
	// MaxDrafts is how many drafts one user may hold at once.
	MaxDrafts = 50
	// MaxOpenLinkReports is how many open link reports one user may have at once.
	MaxOpenLinkReports = 20
	// MaxOpenContactRequests is how many open contact requests the site holds before it refuses more.
	MaxOpenContactRequests = 1000
)

// ContactRetention is how long after a contact request is resolved its personal data is kept.
const ContactRetention = 180 * 24 * time.Hour
