package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// CatalogueImport is a whole catalogue to write into an empty database: every entity with the IDs it will have
// (1..N per table, so that documents can reference final IDs and a dry run shows exactly what is written), its
// system fields and its first revision, which is approved and has source import. Links holds one row per
// normalised URL: the link fields every document attaching the URL carries (one canonical set) and the
// moderator state the import writes directly. Liveness is not imported: the wiki records none, so it is the
// column's default (unknown).
//
// Conservation history follows from each piece's document: a piece whose status is not unassessed gets one
// history row, with ImportConservationReason and the document's source, at its revision.
type CatalogueImport struct {
	Terms       []ImportedTerm       `json:"terms"`
	Collections []ImportedCollection `json:"collections"`
	Events      []ImportedEvent      `json:"events"`
	Persons     []ImportedPerson     `json:"persons"`
	Pieces      []ImportedPiece      `json:"pieces"`
	Links       []ImportedLink       `json:"links"`
}

// TermAncestors is the term_ancestors closure of the import's terms: TermClosure of their parents.
func (imp CatalogueImport) TermAncestors() ([]TermAncestor, error) {
	parents := make(map[int64]*int64, len(imp.Terms))
	for _, t := range imp.Terms {
		parents[t.ID] = t.Doc.ParentID
	}

	return TermClosure(parents)
}

// ErrInvalidImport is returned by CatalogueImport.Validate. Its messages name the entity type, ID and field.
var ErrInvalidImport = errors.New("invalid catalogue import")

// WithheldEmail is what the import writes in free text in place of an email address it withholds.
const WithheldEmail = "[email withheld]"

// HoldsWithheldText reports whether s holds a withheld-text placeholder, as written or slugified
// ("email-withheld").
func HoldsWithheldText(s string) bool {
	return strings.Contains(s, WithheldEmail) || strings.Contains(s, Slugify(WithheldEmail))
}

// ImportedRevision is an imported entity's first revision: approved, of source import, with no author. Summary is
// one of the neutral import summaries (ImportSummary…), and Contributors are the wiki users credited for the page
// (revision_contributors, in order).
type ImportedRevision struct {
	ID           int64     `json:"id"`
	Summary      string    `json:"summary"`
	Contributors []string  `json:"contributors"`
	CreatedAt    time.Time `json:"created_at"`
}

// LegacyPage is where an imported entity was on the wiki (legacy_wiki_pageid, legacy_title): a page, or, with no
// PageID, the title a red link gave.
type LegacyPage struct {
	PageID *uint32 `json:"page_id"`
	Title  string  `json:"title"`
}

// ImportedTerm is an imported term. Legacy is its category page, if it has one.
type ImportedTerm struct {
	ID       int64            `json:"id"`
	Slug     string           `json:"slug"`
	Doc      TermDocument     `json:"doc"`
	Legacy   *LegacyPage      `json:"legacy"`
	Revision ImportedRevision `json:"revision"`
}

// ImportedCollection is an imported collection. Collections have no slug and no legacy columns.
type ImportedCollection struct {
	ID         int64              `json:"id"`
	SortName   string             `json:"sort_name"`
	SortLetter Letter             `json:"sort_letter"`
	Doc        CollectionDocument `json:"doc"`
	Revision   ImportedRevision   `json:"revision"`
}

// ImportedEvent is an imported event or venue. Legacy is its venue page, if it has one.
type ImportedEvent struct {
	ID       int64            `json:"id"`
	Slug     string           `json:"slug"`
	Doc      EventDocument    `json:"doc"`
	Legacy   *LegacyPage      `json:"legacy"`
	Revision ImportedRevision `json:"revision"`
}

// ImportedPerson is an imported person. Legacy is their bard page, or the title a red link gave a stub.
type ImportedPerson struct {
	ID         int64            `json:"id"`
	Slug       string           `json:"slug"`
	SortName   string           `json:"sort_name"`
	SortLetter Letter           `json:"sort_letter"`
	Doc        PersonDocument   `json:"doc"`
	Legacy     *LegacyPage      `json:"legacy"`
	Revision   ImportedRevision `json:"revision"`
}

// ImportedPiece is an imported piece. Legacy is its page, or the title a bard's works list gave a stub;
// LegacyHadInlineLyrics records that the page had lyrics, which are never imported.
type ImportedPiece struct {
	ID                    int64            `json:"id"`
	Slug                  string           `json:"slug"`
	SortTitle             string           `json:"sort_title"`
	SortLetter            Letter           `json:"sort_letter"`
	Doc                   PieceDocument    `json:"doc"`
	LegacyHadInlineLyrics bool             `json:"legacy_had_inline_lyrics"`
	Legacy                *LegacyPage      `json:"legacy"`
	Revision              ImportedRevision `json:"revision"`
}

// ImportedLink is a links row: Link holds the fields every document attaching its normalised URL carries (Roles is
// empty: roles belong to a document), and IsHidden is the moderator takedown state, written directly.
type ImportedLink struct {
	ID       int64   `json:"id"`
	Link     DocLink `json:"link"`
	IsHidden bool    `json:"is_hidden"`
}

// ImportConservationReason is the reason of the conservation history row of an imported piece's status.
const ImportConservationReason = "Imported from the wiki's Conservation Status category."

// The neutral summaries of imported revisions. They name no one: a summary is shown on every approved revision,
// including those of persons without a page, and nothing checks it against aliases.
const (
	// ImportSummaryStub is the summary of a stub piece or person, which the wiki only names.
	ImportSummaryStub = "Imported from the wiki as a stub."
	// ImportSummaryCollections is the summary of a collection of the wiki's Collections page.
	ImportSummaryCollections = "Imported from the wiki's Collections page."
	// ImportSummaryCategory is the summary of a term whose category has no page.
	ImportSummaryCategory = "Imported from the wiki's categories."
	// ImportSummaryPerformances is the summary of an event the wiki's performance listings name.
	ImportSummaryPerformances = "Imported from the wiki's performance listings."
)

// ImportSummaryPage is the summary of an entity imported from a wiki page at its revision revID.
func ImportSummaryPage(revID int64) string {
	return fmt.Sprintf("Imported from the wiki (page revision %d).", revID)
}

var pageSummary = regexp.MustCompile(`^Imported from the wiki \(page revision [1-9][0-9]*\)\.$`)

// IsImportSummary reports whether s is one of the neutral import summaries.
func IsImportSummary(s string) bool {
	switch s {
	case ImportSummaryStub, ImportSummaryCollections, ImportSummaryCategory, ImportSummaryPerformances:
		return true
	}

	return pageSummary.MatchString(s)
}

// CollationKey is the key text columns compare by (utf8mb4_0900_ai_ci): letters folded (FoldLetters) and lower
// case, so that two names a UNIQUE index would take for one have the same key. It folds at least as much as the
// collation does, so that a value Validate accepts is never a duplicate entry.
func CollationKey(s string) string {
	return strings.ToLower(FoldLetters(s))
}
