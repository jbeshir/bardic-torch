package domain

import "time"

// A user's repertoire (docs/PLAN.md §5.1 "Repertoire") is their own list of the public pieces they perform. Each
// entry has private notes, which only its owner ever sees, and a visibility: a public entry is listed on the
// owner's public repertoire, addressed by their public ID; a private one, the default, is not.
const (
	// MaxRepertoireEntries is how many entries one user's repertoire may hold.
	MaxRepertoireEntries = 2000
	// MaxRepertoireNotesLength is the most characters the notes of a repertoire entry may have.
	MaxRepertoireNotesLength = 2000
)

// RepertoireEntry is an entry of a user's repertoire as its owner sees it: the public piece, the owner's notes,
// its visibility, when it was added and when it last changed.
type RepertoireEntry struct {
	Piece      PieceSummary
	Notes      *string
	Visibility RepertoireVisibility
	AddedAt    time.Time
	UpdatedAt  time.Time
}

// RepertoireList is one page of a user's own repertoire, newest entry first, with how many entries match in all.
type RepertoireList struct {
	Entries []RepertoireEntry
	Total   int64
}

// PublicRepertoireEntry is a public entry of a user's repertoire as anyone sees it: the piece and when it was
// added, never the notes.
type PublicRepertoireEntry struct {
	Piece   PieceSummary
	AddedAt time.Time
}

// RepertoireOwner is the owner of a public repertoire as anyone sees them: their display name and role, never
// their user ID.
type RepertoireOwner struct {
	DisplayName string
	Role        Role
}

// PublicRepertoire is one page of the public entries of a user's repertoire, newest first, with how many there are
// in all.
type PublicRepertoire struct {
	Owner   RepertoireOwner
	Entries []PublicRepertoireEntry
	Total   int64
}
