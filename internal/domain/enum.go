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
		RevisionSourceUser, RevisionSourceImport, RevisionSourceMCP,
	}
	revisionStatusValues = []RevisionStatus{
		RevisionStatusDraft, RevisionStatusPending, RevisionStatusApproved, RevisionStatusRejected,
		RevisionStatusWithdrawn,
	}
	revisionActionValues = []RevisionAction{
		RevisionActionSaveDraft, RevisionActionDiscardDraft, RevisionActionSubmit, RevisionActionApprove,
		RevisionActionReject, RevisionActionWithdraw, RevisionActionRebase, RevisionActionRevert,
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
	preferenceDimensionValues = []PreferenceDimension{
		PreferenceDimensionLicensing, PreferenceDimensionPerformancePermission, PreferenceDimensionAdaptations,
		PreferenceDimensionAttribution, PreferenceDimensionCommercialUse, PreferenceDimensionRecording,
		PreferenceDimensionPerformanceEtiquette, PreferenceDimensionObjectionableUse,
	}
	preferenceChoiceValues = []PreferenceChoice{
		PreferenceChoicePD, PreferenceChoiceCCBY, PreferenceChoiceCCBYSA, PreferenceChoiceCCBYNC,
		PreferenceChoiceCCBYNCSA, PreferenceChoiceCCBYND, PreferenceChoiceCCBYNCND, PreferenceChoiceARR,
		PreferenceChoiceOther, PreferenceChoiceVariesByWork,
		PreferenceChoiceNotGiven, PreferenceChoiceAskFirst, PreferenceChoiceAskRepresentative,
		PreferenceChoiceGroupMembers, PreferenceChoiceGiven,
		PreferenceChoiceInformRequired, PreferenceChoiceInformWelcome, PreferenceChoiceInformNotNeeded,
		PreferenceChoiceInformNotWanted,
		PreferenceChoiceEncouraged, PreferenceChoiceAllowed, PreferenceChoiceAllowedWithConditions,
		PreferenceChoiceLiveOnly, PreferenceChoiceNotAllowed,
		PreferenceChoiceFairUseOnly, PreferenceChoiceSeriousOnly, PreferenceChoiceRespectfulOnly,
		PreferenceChoiceMinorChangesOnly, PreferenceChoiceShowBeforePerforming,
		PreferenceChoiceAllowNewWords, PreferenceChoiceAllowFanVideos, PreferenceChoiceAllowStoryChanges,
		PreferenceChoiceAllowShortening, PreferenceChoiceAllowExtending, PreferenceChoiceAllowImprovements,
		PreferenceChoiceAllowMusicalSetting, PreferenceChoiceAllowOwnVersion, PreferenceChoiceAllowStageUse,
		PreferenceChoiceRefuseNewWords, PreferenceChoiceRefuseFanVideos, PreferenceChoiceRefuseStoryChanges,
		PreferenceChoiceRefuseShortening, PreferenceChoiceRefuseExtending, PreferenceChoiceRefuseImprovements,
		PreferenceChoiceRefuseMusicalSetting, PreferenceChoiceRefuseOwnVersion, PreferenceChoiceRefuseStageUse,
		PreferenceChoiceStageName, PreferenceChoiceSCAName, PreferenceChoiceLegalName,
		PreferenceChoicePersonaAndLegalName, PreferenceChoiceKnownName, PreferenceChoiceNameWithAffiliation,
		PreferenceChoiceCreditBefore, PreferenceChoiceCreditAfter, PreferenceChoiceCreditBeforeAndAfter,
		PreferenceChoiceCreditOncePerSet, PreferenceChoiceCreditAnyTime, PreferenceChoiceCreditWhenAsked,
		PreferenceChoiceMentionWebsite, PreferenceChoiceMentionSource, PreferenceChoiceMentionShop,
		PreferenceChoiceMentionPerformance, PreferenceChoiceLinkToCreator, PreferenceChoiceWrittenCredit,
		PreferenceChoiceQRCode,
		PreferenceChoiceNonCommercialOnly,
		PreferenceChoiceTipsAndPrizes, PreferenceChoicePaidPerformance, PreferenceChoiceTicketedPerformance,
		PreferenceChoiceThirdPartyCommercial, PreferenceChoiceMonetisedUploads, PreferenceChoicePaidRecording,
		PreferenceChoiceStreamingIncome, PreferenceChoiceSellDownloads, PreferenceChoiceSellPhysicalRecordings,
		PreferenceChoiceSellPrint, PreferenceChoiceResellCreatorCopies, PreferenceChoiceMerchandise,
		PreferenceChoiceAdvertising,
		PreferenceChoiceWithPermission, PreferenceChoiceWithAttribution, PreferenceChoiceWithNotification,
		PreferenceChoiceCreatorFirst, PreferenceChoiceProfitShare, PreferenceChoiceIndividualsOnly,
		PreferenceChoiceStandaloneSingle, PreferenceChoiceCompilationOnly, PreferenceChoiceIncomeCap,
		PreferenceChoiceAmateursOnly,
		PreferenceChoiceNoWriting, PreferenceChoiceOralOnly, PreferenceChoiceTrustedAudiencesOnly,
		PreferenceChoicePrivateNotebook, PreferenceChoiceSmallPrintOnly, PreferenceChoiceShareInPerson,
		PreferenceChoiceLyricsOnline, PreferenceChoiceUploadsAllowed,
		PreferenceChoiceNoRecording, PreferenceChoiceNotifyAfter, PreferenceChoiceAskBeforeUpload,
		PreferenceChoiceAskBeforeRecording, PreferenceChoiceSelectiveRecording, PreferenceChoiceFormalOnly,
		PreferenceChoiceSingAlongWelcome, PreferenceChoiceJoinInAnyWay, PreferenceChoiceSingAlongFine,
		PreferenceChoiceNoSingAlong, PreferenceChoiceOtherJammingOnly, PreferenceChoiceUnisonOnly,
		PreferenceChoiceNoPercussion, PreferenceChoiceChorusOnly,
		PreferenceChoiceNoCompetition, PreferenceChoiceCompeteUnlessCreatorCompeting,
		PreferenceChoiceCompeteAfterChecking, PreferenceChoiceNotAgainstCreator,
		PreferenceChoiceCircleFine, PreferenceChoiceCircleAskBeforehand, PreferenceChoiceCircleQuickAsk,
		PreferenceChoiceCirclePermissionNeeded, PreferenceChoiceCircleNotAllowed,
		PreferenceChoicePerformFreely, PreferenceChoiceMemorise, PreferenceChoicePractiseThoroughly,
		PreferenceChoiceNoObjectionablePurposes, PreferenceChoiceNoObjectionableContexts,
		PreferenceChoiceNoObjectionableContent,
		PreferenceChoiceProselytising, PreferenceChoiceAnyPolitics, PreferenceChoicePartisanPolitics,
		PreferenceChoiceHateGroups, PreferenceChoicePrejudice, PreferenceChoiceIllegalActivity,
		PreferenceChoiceReligiousOrPoliticalAdvertising, PreferenceChoiceCorporateAdvertising,
		PreferenceChoiceRestrictedGoodsAdvertising, PreferenceChoiceViolence, PreferenceChoiceGraphicViolence,
		PreferenceChoiceReligiousConflict, PreferenceChoiceMockingCreator, PreferenceChoiceMockingOthers,
		PreferenceChoicePoliticalEvents, PreferenceChoicePartisanEvents, PreferenceChoiceReligiousEvents,
		PreferenceChoiceHateGroupEvents, PreferenceChoiceEventsIncludingHateGroups, PreferenceChoiceViolentEvents,
		PreferenceChoiceAlcoholEvents, PreferenceChoiceAdultEvents,
		PreferenceChoiceNSFW, PreferenceChoiceNotChildSafe, PreferenceChoiceProfanity,
		PreferenceChoiceSexualContent, PreferenceChoicePornography, PreferenceChoiceCorporateRisk,
		PreferenceChoiceLegalDrugs, PreferenceChoiceIllegalDrugs, PreferenceChoiceReligiousContent,
		PreferenceChoiceOffensiveContent,
		PreferenceChoiceAskIfUnsure, PreferenceChoiceReservesRight, PreferenceChoicePrivateUseOK,
		PreferenceChoiceReportSightings, PreferenceChoiceMayIntervene, PreferenceChoiceLetCreatorDecide,
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
	licenceVersionValues = []LicenceVersion{
		LicenceVersion10, LicenceVersion20, LicenceVersion25, LicenceVersion30, LicenceVersion40,
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
	linkReportReasonValues = []LinkReportReason{
		LinkReportReasonBroken, LinkReportReasonWrongLink, LinkReportReasonPrivacy, LinkReportReasonCopyright,
		LinkReportReasonLyrics, LinkReportReasonOther,
	}
	linkReportStatusValues = []LinkReportStatus{
		LinkReportStatusOpen, LinkReportStatusResolved, LinkReportStatusDismissed,
	}
	repertoireVisibilityValues = []RepertoireVisibility{
		RepertoireVisibilityPrivate, RepertoireVisibilityPublic,
	}
	contactRequestKindValues = []ContactRequestKind{
		ContactRequestKindTakedown, ContactRequestKindOptOut, ContactRequestKindCorrection,
		ContactRequestKindGeneral,
	}
	contactRequestStatusValues = []ContactRequestStatus{
		ContactRequestStatusOpen, ContactRequestStatusResolved, ContactRequestStatusDismissed,
	}
	pieceSortValues = []PieceSort{
		PieceSortTitle, PieceSortRelevance,
	}
	personSortValues = []PersonSort{
		PersonSortName, PersonSortRelevance,
	}
	flagCodeValues = []FlagCode{
		FlagHiddenAliasInSummary, FlagHiddenAliasInText, FlagHiddenAliasInReviewNote, FlagHiddenAliasCollision,
		FlagHiddenLinkCollision, FlagHiddenLinkAttached, FlagHiddenDataConflict, FlagPossibleDuplicate,
		FlagReferencesPending,
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

// Symmetric reports whether a relation of type p between two persons holds both ways.
func (p PersonRelationType) Symmetric() bool { return p == PersonRelationTypeSameAs }

// Valid reports whether p is a known PreferenceDimension.
func (p PreferenceDimension) Valid() bool { return slices.Contains(preferenceDimensionValues, p) }

// Valid reports whether p is a known PreferenceChoice, of any dimension.
func (p PreferenceChoice) Valid() bool { return slices.Contains(preferenceChoiceValues, p) }

// Valid reports whether p is a known PieceKind.
func (p PieceKind) Valid() bool { return slices.Contains(pieceKindValues, p) }

// Valid reports whether c is a known ConservationStatus.
func (c ConservationStatus) Valid() bool { return slices.Contains(conservationStatusValues, c) }

// Valid reports whether l is a known Licence.
func (l Licence) Valid() bool { return slices.Contains(licenceValues, l) }

// Valid reports whether v is a known LicenceVersion.
func (v LicenceVersion) Valid() bool { return slices.Contains(licenceVersionValues, v) }

// Valid reports whether p is a known PieceAliasKind.
func (p PieceAliasKind) Valid() bool { return slices.Contains(pieceAliasKindValues, p) }

// Valid reports whether p is a known PiecePart.
func (p PiecePart) Valid() bool { return slices.Contains(piecePartValues, p) }

// Valid reports whether p is a known PieceRelationType.
func (p PieceRelationType) Valid() bool { return slices.Contains(pieceRelationTypeValues, p) }

// Symmetric reports whether a relation of type p between two pieces holds both ways.
func (p PieceRelationType) Symmetric() bool { return p == PieceRelationTypeSameTextOtherForm }

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

// Valid reports whether r is a known RevisionAction.
func (r RevisionAction) Valid() bool { return slices.Contains(revisionActionValues, r) }

// Valid reports whether l is a known LinkReportReason.
func (l LinkReportReason) Valid() bool { return slices.Contains(linkReportReasonValues, l) }

// Valid reports whether l is a known LinkReportStatus.
func (l LinkReportStatus) Valid() bool { return slices.Contains(linkReportStatusValues, l) }

// Valid reports whether v is a known RepertoireVisibility.
func (v RepertoireVisibility) Valid() bool { return slices.Contains(repertoireVisibilityValues, v) }

// Valid reports whether c is a known ContactRequestKind.
func (c ContactRequestKind) Valid() bool { return slices.Contains(contactRequestKindValues, c) }

// Valid reports whether c is a known ContactRequestStatus.
func (c ContactRequestStatus) Valid() bool { return slices.Contains(contactRequestStatusValues, c) }

// ProblemCodeValues returns every problem code, in declaration order: the problem vocabulary of the API's
// DocumentError (TestSpec_InlineEnumsMatchCode).
func ProblemCodeValues() []ProblemCode {
	return []ProblemCode{
		ProblemRequired, ProblemTooLong, ProblemInvalidCharacters, ProblemNotNFC, ProblemBlank, ProblemDuplicate,
		ProblemUnknownReference, ProblemSelfReference, ProblemCycle, ProblemTooDeep, ProblemWrongFacet,
		ProblemFacetImmutable, ProblemSourceRequired, ProblemReasonRequired, ProblemInvalidURL,
		ProblemLicenceNeedsKnown, ProblemDateNeedsStatus, ProblemNamedNeedsPerson, ProblemPersonNeedsNamed,
		ProblemCreditToNoPagePerson, ProblemRelationToNoPagePerson, ProblemSecondPrimaryTerm, ProblemPartNotAllowed,
		ProblemOutOfRange, ProblemTooMany, ProblemAliasVisibility, ProblemWithheldPlaceholder,
		ProblemWithheldFieldChanged, ProblemChoiceConflict, ProblemDimensionNotAllowed,
	}
}

// RoleValues returns the roles, in the order of the users.role ENUM.
func RoleValues() []Role { return []Role{RoleContributor, RoleModerator} }
