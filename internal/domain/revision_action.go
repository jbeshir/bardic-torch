package domain

// RevisionAction is something done to a revision, which the state machine allows or refuses.
type RevisionAction string

const (
	// RevisionActionSaveDraft creates or updates a draft.
	RevisionActionSaveDraft RevisionAction = "save_draft"
	// RevisionActionDiscardDraft deletes a draft.
	RevisionActionDiscardDraft RevisionAction = "discard_draft"
	// RevisionActionSubmit turns a draft into a pending revision, or into an approved one for a moderator.
	RevisionActionSubmit RevisionAction = "submit"
	// RevisionActionApprove makes a pending revision public.
	RevisionActionApprove RevisionAction = "approve"
	// RevisionActionReject refuses a pending revision.
	RevisionActionReject RevisionAction = "reject"
	// RevisionActionWithdraw lets an author take back a pending revision.
	RevisionActionWithdraw RevisionAction = "withdraw"
	// RevisionActionRebase rewrites a pending revision onto the entity's current revision.
	RevisionActionRebase RevisionAction = "rebase"
	// RevisionActionRevert creates a revision restoring an earlier approved one.
	RevisionActionRevert RevisionAction = "revert"
)

// LinkReportReason is why a user reported a link.
type LinkReportReason string

const (
	// LinkReportReasonBroken is a link that no longer works.
	LinkReportReasonBroken LinkReportReason = "broken"
	// LinkReportReasonWrongLink is a link to something other than what the page says it is.
	LinkReportReasonWrongLink LinkReportReason = "wrong_link"
	// LinkReportReasonPrivacy is a link exposing private information.
	LinkReportReasonPrivacy LinkReportReason = "privacy"
	// LinkReportReasonCopyright is a link to material that infringes copyright.
	LinkReportReasonCopyright LinkReportReason = "copyright"
	// LinkReportReasonLyrics is a link to lyrics that must not be linked.
	LinkReportReasonLyrics LinkReportReason = "lyrics"
	// LinkReportReasonOther is any other reason.
	LinkReportReasonOther LinkReportReason = "other"
)

// LinkReportStatus is where a link report is in moderation.
type LinkReportStatus string

const (
	// LinkReportStatusOpen is a report no moderator has handled.
	LinkReportStatusOpen LinkReportStatus = "open"
	// LinkReportStatusResolved is a report a moderator acted on.
	LinkReportStatusResolved LinkReportStatus = "resolved"
	// LinkReportStatusDismissed is a report a moderator found no action was needed for.
	LinkReportStatusDismissed LinkReportStatus = "dismissed"
)

// ParseLinkReportStatus returns the LinkReportStatus named by s, or an error if s is not a known status.
func ParseLinkReportStatus(s string) (LinkReportStatus, error) {
	return parseEnum[LinkReportStatus](s, "link report status")
}

// RepertoireVisibility is who may see that a piece is in a user's repertoire.
type RepertoireVisibility string

const (
	// RepertoireVisibilityPrivate is an entry only its owner sees.
	RepertoireVisibilityPrivate RepertoireVisibility = "private"
	// RepertoireVisibilityPublic is an entry listed on the owner's public repertoire.
	RepertoireVisibilityPublic RepertoireVisibility = "public"
)

// ParseRepertoireVisibility returns the RepertoireVisibility named by s, or an error if s is not a known one.
func ParseRepertoireVisibility(s string) (RepertoireVisibility, error) {
	return parseEnum[RepertoireVisibility](s, "repertoire visibility")
}

// ContactRequestKind is what a contact request asks for.
type ContactRequestKind string

const (
	// ContactRequestKindTakedown asks for content to be removed.
	ContactRequestKindTakedown ContactRequestKind = "takedown"
	// ContactRequestKindOptOut asks for a person's page to be removed or hidden.
	ContactRequestKindOptOut ContactRequestKind = "opt_out"
	// ContactRequestKindCorrection asks for a correction.
	ContactRequestKindCorrection ContactRequestKind = "correction"
	// ContactRequestKindGeneral is any other message to the moderators.
	ContactRequestKindGeneral ContactRequestKind = "general"
)

// ParseContactRequestKind returns the ContactRequestKind named by s, or an error if s is not a known kind.
func ParseContactRequestKind(s string) (ContactRequestKind, error) {
	return parseEnum[ContactRequestKind](s, "contact request kind")
}

// ContactRequestStatus is where a contact request is in moderation.
type ContactRequestStatus string

const (
	// ContactRequestStatusOpen is a request no moderator has handled.
	ContactRequestStatusOpen ContactRequestStatus = "open"
	// ContactRequestStatusResolved is a request a moderator acted on.
	ContactRequestStatusResolved ContactRequestStatus = "resolved"
	// ContactRequestStatusDismissed is a request a moderator found no action was needed for.
	ContactRequestStatusDismissed ContactRequestStatus = "dismissed"
)

// ParseContactRequestStatus returns the ContactRequestStatus named by s, or an error if s is not a known status.
func ParseContactRequestStatus(s string) (ContactRequestStatus, error) {
	return parseEnum[ContactRequestStatus](s, "contact request status")
}

// ParseEntityType returns the EntityType named by s, or an error if s is not a known entity type.
func ParseEntityType(s string) (EntityType, error) {
	return parseEnum[EntityType](s, "entity type")
}

// ParseRevisionStatus returns the RevisionStatus named by s, or an error if s is not a known status.
func ParseRevisionStatus(s string) (RevisionStatus, error) {
	return parseEnum[RevisionStatus](s, "revision status")
}
