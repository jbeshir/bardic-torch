package domain

// The catalogue's vocabularies. Each type mirrors a MySQL ENUM column (see migrations 003–008) or a request
// parameter; the integration test TestMigrations_EnumColumns keeps the two in step. Types parsed from requests
// have a Parse function; the others are only read from the database and from snapshot documents. Each type's values
// are listed once, in enum.go, which gives every type a Valid method.
//
// An ENUM grows by appending a value. The new value ships here one release before any data uses it, so that a
// rolled-back release never reads a value it does not know.

// EntityType is the kind of entity a revision edits.
type EntityType string

const (
	// EntityTypePiece is a piece (pieces).
	EntityTypePiece EntityType = "piece"
	// EntityTypePerson is a person (persons).
	EntityTypePerson EntityType = "person"
	// EntityTypeTerm is a classification term (terms).
	EntityTypeTerm EntityType = "term"
	// EntityTypeCollection is a collection (collections).
	EntityTypeCollection EntityType = "collection"
	// EntityTypeEvent is an event (events).
	EntityTypeEvent EntityType = "event"
)

// SlugEntityType is the type of an entity addressed by slug: a piece, person, term or event, but never a
// collection, which is addressed by ID. Outside this package it holds only one of the SlugEntityType values, or
// none (the zero value).
type SlugEntityType struct {
	entityType EntityType
}

// The types of entity addressed by slug.
var (
	SlugEntityTypePiece  = SlugEntityType{EntityTypePiece}
	SlugEntityTypePerson = SlugEntityType{EntityTypePerson}
	SlugEntityTypeTerm   = SlugEntityType{EntityTypeTerm}
	SlugEntityTypeEvent  = SlugEntityType{EntityTypeEvent}
)

// EntityType returns the entity type t is, or "" for the zero value.
func (t SlugEntityType) EntityType() EntityType {
	return t.entityType
}

// String returns the entity type's name.
func (t SlugEntityType) String() string {
	return string(t.entityType)
}

// RevisionSource is where a revision came from.
type RevisionSource string

const (
	// RevisionSourceUser is a revision a user submitted through the API.
	RevisionSourceUser RevisionSource = "user"
	// RevisionSourceImport is a revision created by the MediaWiki importer.
	RevisionSourceImport RevisionSource = "import"
)

// RevisionStatus is where a revision is in moderation.
type RevisionStatus string

const (
	// RevisionStatusPending is a submitted revision awaiting review.
	RevisionStatusPending RevisionStatus = "pending"
	// RevisionStatusApproved is an approved revision; only approved revisions are public.
	RevisionStatusApproved RevisionStatus = "approved"
	// RevisionStatusRejected is a revision a moderator rejected.
	RevisionStatusRejected RevisionStatus = "rejected"
	// RevisionStatusWithdrawn is a revision its author withdrew.
	RevisionStatusWithdrawn RevisionStatus = "withdrawn"
)

// Facet is the dimension a term classifies pieces (or, for affiliations, persons) along.
type Facet string

const (
	// FacetMood is a piece's mood, such as "Warlike".
	FacetMood Facet = "mood"
	// FacetTopic is what a piece is about, such as "War".
	FacetTopic Facet = "topic"
	// FacetCulture is the culture a piece comes from or depicts, such as "Gaelic".
	FacetCulture Facet = "culture"
	// FacetLanguage is the language a piece is in.
	FacetLanguage Facet = "language"
	// FacetLegendaria is the legend or story cycle a piece belongs to.
	FacetLegendaria Facet = "legendaria"
	// FacetOrigin is where or when a piece originated, such as "16th Century" or "SCA Pieces".
	FacetOrigin Facet = "origin"
	// FacetOther is any other classification.
	FacetOther Facet = "other"
	// FacetAffiliation is a group, such as a kingdom, that persons belong to.
	FacetAffiliation Facet = "affiliation"
)

// ParseFacet returns the Facet named by s, or an error if s is not a known facet.
func ParseFacet(s string) (Facet, error) {
	return parseEnum[Facet](s, "facet")
}

// CollectionKind is what sort of resource a collection is.
type CollectionKind string

const (
	// CollectionKindIndexOrLinkList is an index or list of links.
	CollectionKindIndexOrLinkList CollectionKind = "index_or_link_list"
	// CollectionKindSongbookPDF is a songbook published as a PDF.
	CollectionKindSongbookPDF CollectionKind = "songbook_pdf"
	// CollectionKindSongbookWebsite is a songbook published as a website.
	CollectionKindSongbookWebsite CollectionKind = "songbook_website"
	// CollectionKindWebsite is a website.
	CollectionKindWebsite CollectionKind = "website"
	// CollectionKindWikiCategory is a category of a wiki.
	CollectionKindWikiCategory CollectionKind = "wiki_category"
	// CollectionKindWiki is a wiki.
	CollectionKindWiki CollectionKind = "wiki"
	// CollectionKindYouTubeChannel is a YouTube channel.
	CollectionKindYouTubeChannel CollectionKind = "youtube_channel"
	// CollectionKindYouTubePlaylist is a YouTube playlist.
	CollectionKindYouTubePlaylist CollectionKind = "youtube_playlist"
	// CollectionKindGoogleDocSongbook is a songbook published as a Google Doc.
	CollectionKindGoogleDocSongbook CollectionKind = "google_doc_songbook"
	// CollectionKindBlog is a blog.
	CollectionKindBlog CollectionKind = "blog"
	// CollectionKindArchivedSongbook is a songbook that survives only in an archive.
	CollectionKindArchivedSongbook CollectionKind = "archived_songbook"
	// CollectionKindHostedDocument is a document hosted on a file-sharing service.
	CollectionKindHostedDocument CollectionKind = "hosted_document"
)

// ParseCollectionKind returns the CollectionKind named by s, or an error if s is not a known collection kind.
func ParseCollectionKind(s string) (CollectionKind, error) {
	return parseEnum[CollectionKind](s, "collection kind")
}

// PersonKind is what sort of person a person row is.
type PersonKind string

const (
	// PersonKindCommunityBard is a member of the bardic community.
	PersonKindCommunityBard PersonKind = "community_bard"
	// PersonKindHistorical is a historical author or composer.
	PersonKindHistorical PersonKind = "historical"
	// PersonKindGroup is a group of performers.
	PersonKindGroup PersonKind = "group"
)

// ParsePersonKind returns the PersonKind named by s, or an error if s is not a known person kind.
func ParsePersonKind(s string) (PersonKind, error) {
	return parseEnum[PersonKind](s, "person kind")
}

// PersonStatus is a person's status, which decides how much of their page is shown.
type PersonStatus string

const (
	// PersonStatusActive is the default.
	PersonStatusActive PersonStatus = "active"
	// PersonStatusDeceased is a person who has died.
	PersonStatusDeceased PersonStatus = "deceased"
	// PersonStatusPrefersNoPage is a person who asked not to have a page: their page shows only their identity, and
	// references to them name them without a link.
	PersonStatusPrefersNoPage PersonStatus = "prefers_no_page"
)

// LicensingStatus is whether the licensing of a piece or a person's works is known.
type LicensingStatus string

const (
	// LicensingStatusKnown is known licensing.
	LicensingStatusKnown LicensingStatus = "known"
	// LicensingStatusUnknown is unknown licensing, the default.
	LicensingStatusUnknown LicensingStatus = "unknown"
)

// PersonAliasKind is what sort of name a person alias is.
type PersonAliasKind string

const (
	// PersonAliasKindLegal is a legal name.
	PersonAliasKindLegal PersonAliasKind = "legal"
	// PersonAliasKindSCA is a Society for Creative Anachronism name.
	PersonAliasKindSCA PersonAliasKind = "sca"
)

// PersonRole is something a person does.
type PersonRole string

const (
	// PersonRoleSinger sings.
	PersonRoleSinger PersonRole = "singer"
	// PersonRoleSongwriter writes songs.
	PersonRoleSongwriter PersonRole = "songwriter"
	// PersonRoleMusician plays music.
	PersonRoleMusician PersonRole = "musician"
	// PersonRoleInstrumentalist plays an instrument.
	PersonRoleInstrumentalist PersonRole = "instrumentalist"
	// PersonRoleStoryteller tells stories.
	PersonRoleStoryteller PersonRole = "storyteller"
	// PersonRolePoet writes or recites poetry.
	PersonRolePoet PersonRole = "poet"
	// PersonRoleJester performs comedy.
	PersonRoleJester PersonRole = "jester"
	// PersonRoleDancer dances.
	PersonRoleDancer PersonRole = "dancer"
)

// ParsePersonRole returns the PersonRole named by s, or an error if s is not a known person role.
func ParsePersonRole(s string) (PersonRole, error) {
	return parseEnum[PersonRole](s, "person role")
}

// PieceKind is what sort of piece a piece is.
type PieceKind string

const (
	// PieceKindSong is a song.
	PieceKindSong PieceKind = "song"
	// PieceKindPoem is a poem.
	PieceKindPoem PieceKind = "poem"
	// PieceKindStory is a story.
	PieceKindStory PieceKind = "story"
	// PieceKindDanceMusic is music for dancing.
	PieceKindDanceMusic PieceKind = "dance_music"
	// PieceKindInstrumental is an instrumental piece.
	PieceKindInstrumental PieceKind = "instrumental"
)

// ParsePieceKind returns the PieceKind named by s, or an error if s is not a known piece kind.
func ParsePieceKind(s string) (PieceKind, error) {
	return parseEnum[PieceKind](s, "piece kind")
}

// ConservationStatus is how at risk a piece is of being lost.
type ConservationStatus string

const (
	// ConservationStatusUnassessed is not yet assessed, the default.
	ConservationStatusUnassessed ConservationStatus = "unassessed"
	// ConservationStatusLowRisk is widely known and recorded.
	ConservationStatusLowRisk ConservationStatus = "low_risk"
	// ConservationStatusEndangered is known to few people or poorly recorded.
	ConservationStatusEndangered ConservationStatus = "endangered"
	// ConservationStatusCriticallyEndangered is close to being lost.
	ConservationStatusCriticallyEndangered ConservationStatus = "critically_endangered"
	// ConservationStatusRestricted overrides every other status and hides the prompts to help preserve the piece.
	ConservationStatusRestricted ConservationStatus = "restricted"
)

// ParseConservationStatus returns the ConservationStatus named by s, or an error if s is not a known
// conservation status.
func ParseConservationStatus(s string) (ConservationStatus, error) {
	return parseEnum[ConservationStatus](s, "conservation status")
}

// PieceAliasKind is what sort of alternative title a piece alias is.
type PieceAliasKind string

const (
	// PieceAliasKindAltTitle is an alternative title.
	PieceAliasKindAltTitle PieceAliasKind = "alt_title"
	// PieceAliasKindSubtitle is a subtitle.
	PieceAliasKindSubtitle PieceAliasKind = "subtitle"
	// PieceAliasKindRedirect is a title the legacy wiki redirected to the piece's page.
	PieceAliasKindRedirect PieceAliasKind = "redirect"
)

// CreditRole is what a credit credits.
type CreditRole string

const (
	// CreditRoleLyrics is the lyrics.
	CreditRoleLyrics CreditRole = "lyrics"
	// CreditRoleTune is the tune.
	CreditRoleTune CreditRole = "tune"
	// CreditRoleAuthor is the whole of a text, such as a poem or story.
	CreditRoleAuthor CreditRole = "author"
)

// Attribution is who a credit is attributed to. Only a named credit has a person.
type Attribution string

const (
	// AttributionNamed is a credit to a person.
	AttributionNamed Attribution = "named"
	// AttributionTraditional is a traditional piece.
	AttributionTraditional Attribution = "traditional"
	// AttributionUnknown is an author nobody knows.
	AttributionUnknown Attribution = "unknown"
	// AttributionAnonymous is an author who chose to be anonymous.
	AttributionAnonymous Attribution = "anonymous"
	// AttributionVarious is many authors.
	AttributionVarious Attribution = "various"
)

// Certainty is how certain a credit is.
type Certainty string

const (
	// CertaintyCertain is certain.
	CertaintyCertain Certainty = "certain"
	// CertaintyProbable is probable.
	CertaintyProbable Certainty = "probable"
	// CertaintyPossible is possible.
	CertaintyPossible Certainty = "possible"
	// CertaintyDisputed is disputed.
	CertaintyDisputed Certainty = "disputed"
)

// LinkPlaceType is what sort of place a link points to.
type LinkPlaceType string

const (
	// LinkPlaceTypeRecordingPlatform is a platform hosting recordings, such as a video site.
	LinkPlaceTypeRecordingPlatform LinkPlaceType = "recording_platform"
	// LinkPlaceTypeStreaming is a music streaming service.
	LinkPlaceTypeStreaming LinkPlaceType = "streaming"
	// LinkPlaceTypeAuthorSite is an author's own site.
	LinkPlaceTypeAuthorSite LinkPlaceType = "author_site"
	// LinkPlaceTypeCommunityArchive is an archive kept by the community.
	LinkPlaceTypeCommunityArchive LinkPlaceType = "community_archive"
	// LinkPlaceTypeBook is a book.
	LinkPlaceTypeBook LinkPlaceType = "book"
)

// Liveness is whether a link still works, as last checked.
type Liveness string

const (
	// LivenessUnknown is not yet checked, the default.
	LivenessUnknown Liveness = "unknown"
	// LivenessOK is a working link.
	LivenessOK Liveness = "ok"
	// LivenessDead is a link that no longer works.
	LivenessDead Liveness = "dead"
	// LivenessBlocked is a link that refused the checker, so whether it works is not known.
	LivenessBlocked Liveness = "blocked"
)

// LinkRole is what a link offers about the piece or person it is attached to.
type LinkRole string

const (
	// LinkRoleOfficial is the official page.
	LinkRoleOfficial LinkRole = "official"
	// LinkRoleLyrics is the lyrics.
	LinkRoleLyrics LinkRole = "lyrics"
	// LinkRoleText is the text of a poem or story.
	LinkRoleText LinkRole = "text"
	// LinkRoleSheetMusic is sheet music.
	LinkRoleSheetMusic LinkRole = "sheet_music"
	// LinkRoleDanceInstructions is dance instructions.
	LinkRoleDanceInstructions LinkRole = "dance_instructions"
	// LinkRoleTextSource is a historical source of the text.
	LinkRoleTextSource LinkRole = "text_source"
	// LinkRoleTranslation is a translation.
	LinkRoleTranslation LinkRole = "translation"
	// LinkRolePerformance is a recorded performance.
	LinkRolePerformance LinkRole = "performance"
	// LinkRoleLicensingSource is where the licensing is stated.
	LinkRoleLicensingSource LinkRole = "licensing_source"
	// LinkRoleWebsite is a person's website.
	LinkRoleWebsite LinkRole = "website"
	// LinkRoleExternalProfile is a person's profile on another site.
	LinkRoleExternalProfile LinkRole = "external_profile"
	// LinkRoleOther is anything else.
	LinkRoleOther LinkRole = "other"
)

// NeedsWorkReason is why an entry is listed as needing work.
type NeedsWorkReason string

const (
	// NeedsWorkReasonStub is a piece or person marked as a stub.
	NeedsWorkReasonStub NeedsWorkReason = "stub"
	// NeedsWorkReasonUnassessed is a piece whose conservation status is unassessed.
	NeedsWorkReasonUnassessed NeedsWorkReason = "unassessed"
	// NeedsWorkReasonMissingLicensing is a piece or community bard whose licensing is unknown.
	NeedsWorkReasonMissingLicensing NeedsWorkReason = "missing_licensing"
	// NeedsWorkReasonDeadLinks is an entry with at least one visible dead link.
	NeedsWorkReasonDeadLinks NeedsWorkReason = "dead_links"
)

// NeedsWorkReasons returns every needs-work reason, in declaration order.
func NeedsWorkReasons() []NeedsWorkReason {
	return []NeedsWorkReason{
		NeedsWorkReasonStub, NeedsWorkReasonUnassessed, NeedsWorkReasonMissingLicensing, NeedsWorkReasonDeadLinks,
	}
}

// ParseNeedsWorkReason returns the NeedsWorkReason named by s, or an error if s is not a known reason.
func ParseNeedsWorkReason(s string) (NeedsWorkReason, error) {
	return parseEnum[NeedsWorkReason](s, "needs-work reason")
}

// PieceSort is the order of a piece list.
type PieceSort string

const (
	// PieceSortTitle orders by sort title.
	PieceSortTitle PieceSort = "title"
	// PieceSortRelevance orders by search score; it needs a search query.
	PieceSortRelevance PieceSort = "relevance"
)

// ParsePieceSort returns the PieceSort named by s, or an error if s is not a known piece order.
func ParsePieceSort(s string) (PieceSort, error) {
	return parseEnum[PieceSort](s, "piece sort")
}

// PersonSort is the order of a person list.
type PersonSort string

const (
	// PersonSortName orders by sort name.
	PersonSortName PersonSort = "name"
	// PersonSortRelevance orders by search score; it needs a search query.
	PersonSortRelevance PersonSort = "relevance"
)

// ParsePersonSort returns the PersonSort named by s, or an error if s is not a known person order.
func ParsePersonSort(s string) (PersonSort, error) {
	return parseEnum[PersonSort](s, "person sort")
}
