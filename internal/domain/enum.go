package domain

import (
	"fmt"
	"slices"
)

// Enum is one of the catalogue vocabularies of catalogue.go: a string type whose values are a fixed list.
type Enum interface {
	~string
	// Valid reports whether the value is one this release knows.
	Valid() bool
}

// parseEnum returns the value of E named by s, or an error naming what E is if s is not a known value.
func parseEnum[E Enum](s, what string) (E, error) {
	if e := E(s); e.Valid() {
		return e, nil
	}

	return "", fmt.Errorf("unknown %s [%s]", what, s)
}

// The values of each vocabulary, in declaration order.
var (
	entityTypeValues = []EntityType{
		EntityTypePiece, EntityTypePerson, EntityTypeTerm, EntityTypeCollection, EntityTypeEvent,
	}
	revisionSourceValues = []RevisionSource{
		RevisionSourceUser, RevisionSourceImport,
	}
	revisionStatusValues = []RevisionStatus{
		RevisionStatusPending, RevisionStatusApproved, RevisionStatusRejected,
		RevisionStatusWithdrawn,
	}
	facetValues = []Facet{
		FacetMood, FacetTopic, FacetCulture, FacetLanguage, FacetLegendaria, FacetForm, FacetOrigin, FacetOther,
		FacetAffiliation,
	}
	collectionKindValues = []CollectionKind{
		CollectionKindIndexOrLinkList, CollectionKindSongbookPDF, CollectionKindSongbookWebsite,
		CollectionKindWebsite, CollectionKindWikiCategory, CollectionKindWiki, CollectionKindYouTubeChannel,
		CollectionKindYouTubePlaylist, CollectionKindGoogleDocSongbook, CollectionKindBlog,
		CollectionKindArchivedSongbook, CollectionKindHostedDocument, CollectionKindOther,
	}
	collectionEnumerableValues = []CollectionEnumerable{
		CollectionEnumerableUnknown, CollectionEnumerableYes, CollectionEnumerablePDF, CollectionEnumerableBrowser,
		CollectionEnumerableAPI, CollectionEnumerableNo,
	}
	eventKindValues = []EventKind{
		EventKindVenue, EventKindEvent,
	}
	personKindValues = []PersonKind{
		PersonKindCommunityBard, PersonKindHistorical, PersonKindGroup,
	}
	personStatusValues = []PersonStatus{
		PersonStatusActive, PersonStatusDeceased, PersonStatusPrefersNoPage,
	}
	licensingStatusValues = []LicensingStatus{
		LicensingStatusKnown, LicensingStatusUnknown,
	}
	personAliasKindValues = []PersonAliasKind{
		PersonAliasKindLegal, PersonAliasKindSCA, PersonAliasKindStage, PersonAliasKindNickname,
		PersonAliasKindFormer, PersonAliasKindRedirect,
	}
	personRoleValues = []PersonRole{
		PersonRoleSinger, PersonRoleSongwriter, PersonRoleMusician, PersonRoleInstrumentalist,
		PersonRoleStoryteller, PersonRolePoet, PersonRoleJester, PersonRoleDancer,
	}
	personRelationTypeValues = []PersonRelationType{
		PersonRelationTypeHeirOf, PersonRelationTypeMemberOf, PersonRelationTypeSameAs,
	}
	pieceKindValues = []PieceKind{
		PieceKindSong, PieceKindPoem, PieceKindStory, PieceKindDanceMusic, PieceKindInstrumental, PieceKindComedy,
	}
	conservationStatusValues = []ConservationStatus{
		ConservationStatusUnassessed, ConservationStatusLowRisk, ConservationStatusEndangered,
		ConservationStatusCriticallyEndangered, ConservationStatusRestricted,
	}
	licenceValues = []Licence{
		LicencePD, LicenceCCBY, LicenceCCBYSA, LicenceCCBYNC, LicenceCCBYNCSA, LicenceCCBYND, LicenceCCBYNCND,
		LicenceARR, LicenceOther,
	}
	pieceAliasKindValues = []PieceAliasKind{
		PieceAliasKindAltTitle, PieceAliasKindNickname, PieceAliasKindSubtitle, PieceAliasKindRedirect,
	}
	piecePartValues = []PiecePart{
		PiecePartWords, PiecePartTune, PiecePartVerses, PiecePartChorus,
	}
	pieceRelationTypeValues = []PieceRelationType{
		PieceRelationTypeVariantOf, PieceRelationTypeContrafactumOf, PieceRelationTypeSettingOf,
		PieceRelationTypeSameTextOtherForm,
	}
	creditRoleValues = []CreditRole{
		CreditRoleLyrics, CreditRoleTune, CreditRoleAuthor, CreditRoleChoreography, CreditRoleMusic,
		CreditRoleArrangement, CreditRoleTranslation,
	}
	attributionValues = []Attribution{
		AttributionNamed, AttributionTraditional, AttributionUnknown, AttributionAnonymous, AttributionVarious,
	}
	certaintyValues = []Certainty{
		CertaintyCertain, CertaintyProbable, CertaintyPossible, CertaintyDisputed,
	}
	linkPlaceTypeValues = []LinkPlaceType{
		LinkPlaceTypeRecordingPlatform, LinkPlaceTypeStreaming, LinkPlaceTypeAuthorSite,
		LinkPlaceTypeCommunityArchive, LinkPlaceTypePhysicalArchive, LinkPlaceTypeBook, LinkPlaceTypeVendor,
		LinkPlaceTypeAuthorContact, LinkPlaceTypeOralTradition, LinkPlaceTypeOther,
	}
	livenessValues = []Liveness{
		LivenessUnknown, LivenessOK, LivenessDead, LivenessBlocked,
	}
	linkRoleValues = []LinkRole{
		LinkRoleOfficial, LinkRoleLyrics, LinkRoleText, LinkRoleSheetMusic, LinkRoleDanceInstructions,
		LinkRoleTextSource, LinkRoleTranslation, LinkRolePerformance, LinkRoleLicensingSource, LinkRoleWebsite,
		LinkRoleExternalProfile, LinkRoleOther,
	}
	pieceSortValues = []PieceSort{
		PieceSortTitle, PieceSortRelevance,
	}
	personSortValues = []PersonSort{
		PersonSortName, PersonSortRelevance,
	}
)

// Valid reports whether e is a known EntityType.
func (e EntityType) Valid() bool { return slices.Contains(entityTypeValues, e) }

// Valid reports whether r is a known RevisionSource.
func (r RevisionSource) Valid() bool { return slices.Contains(revisionSourceValues, r) }

// Valid reports whether r is a known RevisionStatus.
func (r RevisionStatus) Valid() bool { return slices.Contains(revisionStatusValues, r) }

// Valid reports whether f is a known Facet.
func (f Facet) Valid() bool { return slices.Contains(facetValues, f) }

// Valid reports whether c is a known CollectionKind.
func (c CollectionKind) Valid() bool { return slices.Contains(collectionKindValues, c) }

// Valid reports whether c is a known CollectionEnumerable.
func (c CollectionEnumerable) Valid() bool { return slices.Contains(collectionEnumerableValues, c) }

// Valid reports whether e is a known EventKind.
func (e EventKind) Valid() bool { return slices.Contains(eventKindValues, e) }

// Valid reports whether p is a known PersonKind.
func (p PersonKind) Valid() bool { return slices.Contains(personKindValues, p) }

// Valid reports whether p is a known PersonStatus.
func (p PersonStatus) Valid() bool { return slices.Contains(personStatusValues, p) }

// Valid reports whether l is a known LicensingStatus.
func (l LicensingStatus) Valid() bool { return slices.Contains(licensingStatusValues, l) }

// Valid reports whether p is a known PersonAliasKind.
func (p PersonAliasKind) Valid() bool { return slices.Contains(personAliasKindValues, p) }

// Valid reports whether p is a known PersonRole.
func (p PersonRole) Valid() bool { return slices.Contains(personRoleValues, p) }

// Valid reports whether p is a known PersonRelationType.
func (p PersonRelationType) Valid() bool { return slices.Contains(personRelationTypeValues, p) }

// Valid reports whether p is a known PieceKind.
func (p PieceKind) Valid() bool { return slices.Contains(pieceKindValues, p) }

// Valid reports whether c is a known ConservationStatus.
func (c ConservationStatus) Valid() bool { return slices.Contains(conservationStatusValues, c) }

// Valid reports whether l is a known Licence.
func (l Licence) Valid() bool { return slices.Contains(licenceValues, l) }

// Valid reports whether p is a known PieceAliasKind.
func (p PieceAliasKind) Valid() bool { return slices.Contains(pieceAliasKindValues, p) }

// Valid reports whether p is a known PiecePart.
func (p PiecePart) Valid() bool { return slices.Contains(piecePartValues, p) }

// Valid reports whether p is a known PieceRelationType.
func (p PieceRelationType) Valid() bool { return slices.Contains(pieceRelationTypeValues, p) }

// Valid reports whether c is a known CreditRole.
func (c CreditRole) Valid() bool { return slices.Contains(creditRoleValues, c) }

// Valid reports whether a is a known Attribution.
func (a Attribution) Valid() bool { return slices.Contains(attributionValues, a) }

// Valid reports whether c is a known Certainty.
func (c Certainty) Valid() bool { return slices.Contains(certaintyValues, c) }

// Valid reports whether l is a known LinkPlaceType.
func (l LinkPlaceType) Valid() bool { return slices.Contains(linkPlaceTypeValues, l) }

// Valid reports whether l is a known Liveness.
func (l Liveness) Valid() bool { return slices.Contains(livenessValues, l) }

// Valid reports whether l is a known LinkRole.
func (l LinkRole) Valid() bool { return slices.Contains(linkRoleValues, l) }

// Valid reports whether n is a known NeedsWorkReason.
func (n NeedsWorkReason) Valid() bool { return slices.Contains(NeedsWorkReasons(), n) }

// Valid reports whether p is a known PieceSort.
func (p PieceSort) Valid() bool { return slices.Contains(pieceSortValues, p) }

// Valid reports whether p is a known PersonSort.
func (p PersonSort) Valid() bool { return slices.Contains(personSortValues, p) }
