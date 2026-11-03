package domain

// FlagCode is a moderator flag on a revision (D19): something the moderator must see, and for most flags
// acknowledge, before approving. Flags are never shown to the revision's contributor, so raising one is never an
// oracle for hidden data.
type FlagCode string

// The flag codes.
const (
	// FlagHiddenAliasInSummary is raised when the revision's summary holds a hidden alias.
	FlagHiddenAliasInSummary FlagCode = "hidden_alias_in_summary"
	// FlagHiddenAliasInText is raised when a string field the revision changed holds a hidden alias.
	FlagHiddenAliasInText FlagCode = "hidden_alias_in_text"
	// FlagHiddenAliasInReviewNote is raised when a moderator's review note holds a hidden alias.
	FlagHiddenAliasInReviewNote FlagCode = "hidden_alias_in_review_note"
	// FlagHiddenAliasCollision is raised when restore kept a submitted alias equal to a withheld hidden one.
	FlagHiddenAliasCollision FlagCode = "hidden_alias_collision"
	// FlagHiddenLinkCollision is raised when restore kept a submitted link equal to a withheld hidden one.
	FlagHiddenLinkCollision FlagCode = "hidden_link_collision"
	// FlagHiddenLinkAttached is raised when the document attaches a URL whose link is hidden.
	FlagHiddenLinkAttached FlagCode = "hidden_link_attached"
	// FlagHiddenDataConflict is raised when validation of the restored document finds a problem that validation
	// of the document as the contributor submitted it did not: one caused by data the contributor could not see.
	FlagHiddenDataConflict FlagCode = "hidden_data_conflict"
	// FlagPossibleDuplicate is raised when a new entity may duplicate another (D13).
	FlagPossibleDuplicate FlagCode = "possible_duplicate"
	// FlagReferencesPending is raised when the revision references pending new terms or persons.
	FlagReferencesPending FlagCode = "references_pending"
)

// Flag is a flag on a revision, with the path in the full document of what raised it, if anything in particular
// did. Like a DocumentProblem it never holds a value.
type Flag struct {
	Code FlagCode `json:"code"`
	Path string   `json:"path"`
}
