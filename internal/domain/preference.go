package domain

import (
	"slices"
)

// PreferenceValue is what a preference profile states for one dimension: choices from the dimension's
// vocabulary (preferenceVocabularies), and free-text notes for what the choices cannot say.
type PreferenceValue struct {
	Choices []PreferenceChoice `json:"choices"`
	Notes   *string            `json:"notes"`
}

// PreferenceChoice is one code of a preference dimension's vocabulary. The codes come from the Bard Preference
// Checklist; a code may belong to several dimensions (ask_first), and each dimension groups its codes
// (preferenceVocabularies). The licensing dimension's codes are the Licence values and varies_by_work.
type PreferenceChoice string

// The licence choices of the licensing dimension: the Licence values, and a licence that differs between works.
const (
	PreferenceChoicePD                            = PreferenceChoice(LicencePD)
	PreferenceChoiceCCBY                          = PreferenceChoice(LicenceCCBY)
	PreferenceChoiceCCBYSA                        = PreferenceChoice(LicenceCCBYSA)
	PreferenceChoiceCCBYNC                        = PreferenceChoice(LicenceCCBYNC)
	PreferenceChoiceCCBYNCSA                      = PreferenceChoice(LicenceCCBYNCSA)
	PreferenceChoiceCCBYND                        = PreferenceChoice(LicenceCCBYND)
	PreferenceChoiceCCBYNCND                      = PreferenceChoice(LicenceCCBYNCND)
	PreferenceChoiceARR                           = PreferenceChoice(LicenceARR)
	PreferenceChoiceOther                         = PreferenceChoice(LicenceOther)
	PreferenceChoiceVariesByWork PreferenceChoice = "varies_by_work"
)

// The permission choices of the performance_permission dimension.
const (
	PreferenceChoiceNotGiven          PreferenceChoice = "not_given"
	PreferenceChoiceAskFirst          PreferenceChoice = "ask_first"
	PreferenceChoiceAskRepresentative PreferenceChoice = "ask_representative"
	PreferenceChoiceGroupMembers      PreferenceChoice = "group_members"
	PreferenceChoiceGiven             PreferenceChoice = "given"
)

// The informing choices of the performance_permission and adaptations dimensions: whether the creator wants to
// hear of a use.
const (
	PreferenceChoiceInformRequired  PreferenceChoice = "inform_required"
	PreferenceChoiceInformWelcome   PreferenceChoice = "inform_welcome"
	PreferenceChoiceInformNotNeeded PreferenceChoice = "inform_not_needed"
	PreferenceChoiceInformNotWanted PreferenceChoice = "inform_not_wanted"
)

// The permission choices of the adaptations dimension, and the stance choices of the commercial_use dimension
// (with ask_first and non_commercial_only).
const (
	PreferenceChoiceEncouraged            PreferenceChoice = "encouraged"
	PreferenceChoiceAllowed               PreferenceChoice = "allowed"
	PreferenceChoiceAllowedWithConditions PreferenceChoice = "allowed_with_conditions"
	PreferenceChoiceLiveOnly              PreferenceChoice = "live_only"
	PreferenceChoiceNotAllowed            PreferenceChoice = "not_allowed"
)

// The conditions choices of the adaptations dimension.
const (
	PreferenceChoiceFairUseOnly          PreferenceChoice = "fair_use_only"
	PreferenceChoiceSeriousOnly          PreferenceChoice = "serious_only"
	PreferenceChoiceRespectfulOnly       PreferenceChoice = "respectful_only"
	PreferenceChoiceMinorChangesOnly     PreferenceChoice = "minor_changes_only"
	PreferenceChoiceShowBeforePerforming PreferenceChoice = "show_before_performing"
)

// The kinds of adaptation the adaptations dimension allows, each with its refused twin.
const (
	PreferenceChoiceAllowNewWords        PreferenceChoice = "allow_new_words"
	PreferenceChoiceAllowFanVideos       PreferenceChoice = "allow_fan_videos"
	PreferenceChoiceAllowStoryChanges    PreferenceChoice = "allow_story_changes"
	PreferenceChoiceAllowShortening      PreferenceChoice = "allow_shortening"
	PreferenceChoiceAllowExtending       PreferenceChoice = "allow_extending"
	PreferenceChoiceAllowImprovements    PreferenceChoice = "allow_improvements"
	PreferenceChoiceAllowMusicalSetting  PreferenceChoice = "allow_musical_setting"
	PreferenceChoiceAllowOwnVersion      PreferenceChoice = "allow_own_version"
	PreferenceChoiceAllowStageUse        PreferenceChoice = "allow_stage_use"
	PreferenceChoiceRefuseNewWords       PreferenceChoice = "refuse_new_words"
	PreferenceChoiceRefuseFanVideos      PreferenceChoice = "refuse_fan_videos"
	PreferenceChoiceRefuseStoryChanges   PreferenceChoice = "refuse_story_changes"
	PreferenceChoiceRefuseShortening     PreferenceChoice = "refuse_shortening"
	PreferenceChoiceRefuseExtending      PreferenceChoice = "refuse_extending"
	PreferenceChoiceRefuseImprovements   PreferenceChoice = "refuse_improvements"
	PreferenceChoiceRefuseMusicalSetting PreferenceChoice = "refuse_musical_setting"
	PreferenceChoiceRefuseOwnVersion     PreferenceChoice = "refuse_own_version"
	PreferenceChoiceRefuseStageUse       PreferenceChoice = "refuse_stage_use"
)

// The name choices of the attribution dimension: which of the creator's names to credit (legal_name names the
// kind of name, never the name).
const (
	PreferenceChoiceStageName           PreferenceChoice = "stage_name"
	PreferenceChoiceSCAName             PreferenceChoice = "sca_name"
	PreferenceChoiceLegalName           PreferenceChoice = "legal_name"
	PreferenceChoicePersonaAndLegalName PreferenceChoice = "persona_and_legal_name"
	PreferenceChoiceKnownName           PreferenceChoice = "known_name"
	PreferenceChoiceNameWithAffiliation PreferenceChoice = "name_with_affiliation"
)

// The timing and extras choices of the attribution dimension.
const (
	PreferenceChoiceCreditBefore         PreferenceChoice = "credit_before"
	PreferenceChoiceCreditAfter          PreferenceChoice = "credit_after"
	PreferenceChoiceCreditBeforeAndAfter PreferenceChoice = "credit_before_and_after"
	PreferenceChoiceCreditOncePerSet     PreferenceChoice = "credit_once_per_set"
	PreferenceChoiceCreditAnyTime        PreferenceChoice = "credit_any_time"
	PreferenceChoiceCreditWhenAsked      PreferenceChoice = "credit_when_asked"
	PreferenceChoiceMentionWebsite       PreferenceChoice = "mention_website"
	PreferenceChoiceMentionSource        PreferenceChoice = "mention_source"
	PreferenceChoiceMentionShop          PreferenceChoice = "mention_shop"
	PreferenceChoiceMentionPerformance   PreferenceChoice = "mention_performance"
	PreferenceChoiceLinkToCreator        PreferenceChoice = "link_to_creator"
	PreferenceChoiceWrittenCredit        PreferenceChoice = "written_credit"
	PreferenceChoiceQRCode               PreferenceChoice = "qr_code"
)

// The choices of the commercial_use dimension: its stance (with the adaptations permissions), the uses it allows
// and the conditions of those uses (with group_members).
const (
	PreferenceChoiceNonCommercialOnly      PreferenceChoice = "non_commercial_only"
	PreferenceChoiceTipsAndPrizes          PreferenceChoice = "tips_and_prizes"
	PreferenceChoicePaidPerformance        PreferenceChoice = "paid_performance"
	PreferenceChoiceTicketedPerformance    PreferenceChoice = "ticketed_performance"
	PreferenceChoiceThirdPartyCommercial   PreferenceChoice = "third_party_commercial"
	PreferenceChoiceMonetisedUploads       PreferenceChoice = "monetised_uploads"
	PreferenceChoicePaidRecording          PreferenceChoice = "paid_recording"
	PreferenceChoiceStreamingIncome        PreferenceChoice = "streaming_income"
	PreferenceChoiceSellDownloads          PreferenceChoice = "sell_downloads"
	PreferenceChoiceSellPhysicalRecordings PreferenceChoice = "sell_physical_recordings"
	PreferenceChoiceSellPrint              PreferenceChoice = "sell_print"
	PreferenceChoiceResellCreatorCopies    PreferenceChoice = "resell_creator_copies"
	PreferenceChoiceMerchandise            PreferenceChoice = "merchandise"
	PreferenceChoiceAdvertising            PreferenceChoice = "advertising"
	PreferenceChoiceWithPermission         PreferenceChoice = "with_permission"
	PreferenceChoiceWithAttribution        PreferenceChoice = "with_attribution"
	PreferenceChoiceWithNotification       PreferenceChoice = "with_notification"
	PreferenceChoiceCreatorFirst           PreferenceChoice = "creator_first"
	PreferenceChoiceProfitShare            PreferenceChoice = "profit_share"
	PreferenceChoiceIndividualsOnly        PreferenceChoice = "individuals_only"
	PreferenceChoiceStandaloneSingle       PreferenceChoice = "standalone_single"
	PreferenceChoiceCompilationOnly        PreferenceChoice = "compilation_only"
	PreferenceChoiceIncomeCap              PreferenceChoice = "income_cap"
	PreferenceChoiceAmateursOnly           PreferenceChoice = "amateurs_only"
)

// The choices of the recording dimension: whether the words may be written down, and whether performances may be
// recorded.
const (
	PreferenceChoiceNoWriting            PreferenceChoice = "no_writing"
	PreferenceChoiceOralOnly             PreferenceChoice = "oral_only"
	PreferenceChoiceTrustedAudiencesOnly PreferenceChoice = "trusted_audiences_only"
	PreferenceChoicePrivateNotebook      PreferenceChoice = "private_notebook"
	PreferenceChoiceSmallPrintOnly       PreferenceChoice = "small_print_only"
	PreferenceChoiceShareInPerson        PreferenceChoice = "share_in_person"
	PreferenceChoiceLyricsOnline         PreferenceChoice = "lyrics_online"
	PreferenceChoiceUploadsAllowed       PreferenceChoice = "uploads_allowed"
	PreferenceChoiceNoRecording          PreferenceChoice = "no_recording"
	PreferenceChoiceNotifyAfter          PreferenceChoice = "notify_after"
	PreferenceChoiceAskBeforeUpload      PreferenceChoice = "ask_before_upload"
	PreferenceChoiceAskBeforeRecording   PreferenceChoice = "ask_before_recording"
	PreferenceChoiceSelectiveRecording   PreferenceChoice = "selective_recording"
	PreferenceChoiceFormalOnly           PreferenceChoice = "formal_only"
)

// The choices of the performance_etiquette dimension: singing along, competitions, bardic circles and learning.
const (
	PreferenceChoiceSingAlongWelcome              PreferenceChoice = "sing_along_welcome"
	PreferenceChoiceJoinInAnyWay                  PreferenceChoice = "join_in_any_way"
	PreferenceChoiceSingAlongFine                 PreferenceChoice = "sing_along_fine"
	PreferenceChoiceNoSingAlong                   PreferenceChoice = "no_sing_along"
	PreferenceChoiceOtherJammingOnly              PreferenceChoice = "other_jamming_only"
	PreferenceChoiceUnisonOnly                    PreferenceChoice = "unison_only"
	PreferenceChoiceNoPercussion                  PreferenceChoice = "no_percussion"
	PreferenceChoiceChorusOnly                    PreferenceChoice = "chorus_only"
	PreferenceChoiceNoCompetition                 PreferenceChoice = "no_competition"
	PreferenceChoiceCompeteUnlessCreatorCompeting PreferenceChoice = "compete_unless_creator_competing"
	PreferenceChoiceCompeteAfterChecking          PreferenceChoice = "compete_after_checking"
	PreferenceChoiceNotAgainstCreator             PreferenceChoice = "not_against_creator"
	PreferenceChoiceCircleFine                    PreferenceChoice = "circle_fine"
	PreferenceChoiceCircleAskBeforehand           PreferenceChoice = "circle_ask_beforehand"
	PreferenceChoiceCircleQuickAsk                PreferenceChoice = "circle_quick_ask"
	PreferenceChoiceCirclePermissionNeeded        PreferenceChoice = "circle_permission_needed"
	PreferenceChoiceCircleNotAllowed              PreferenceChoice = "circle_not_allowed"
	PreferenceChoicePerformFreely                 PreferenceChoice = "perform_freely"
	PreferenceChoiceMemorise                      PreferenceChoice = "memorise"
	PreferenceChoicePractiseThoroughly            PreferenceChoice = "practise_thoroughly"
)

// The scope choices of the objectionable_use dimension: which of its lists (purposes, contexts, content) the
// creator objects to.
const (
	PreferenceChoiceNoObjectionablePurposes PreferenceChoice = "no_objectionable_purposes"
	PreferenceChoiceNoObjectionableContexts PreferenceChoice = "no_objectionable_contexts"
	PreferenceChoiceNoObjectionableContent  PreferenceChoice = "no_objectionable_content"
)

// The objectionable purposes, contexts and content of the objectionable_use dimension, and its extras.
const (
	PreferenceChoiceProselytising                   PreferenceChoice = "proselytising"
	PreferenceChoiceAnyPolitics                     PreferenceChoice = "any_politics"
	PreferenceChoicePartisanPolitics                PreferenceChoice = "partisan_politics"
	PreferenceChoiceHateGroups                      PreferenceChoice = "hate_groups"
	PreferenceChoicePrejudice                       PreferenceChoice = "prejudice"
	PreferenceChoiceIllegalActivity                 PreferenceChoice = "illegal_activity"
	PreferenceChoiceReligiousOrPoliticalAdvertising PreferenceChoice = "religious_or_political_advertising"
	PreferenceChoiceCorporateAdvertising            PreferenceChoice = "corporate_advertising"
	PreferenceChoiceRestrictedGoodsAdvertising      PreferenceChoice = "restricted_goods_advertising"
	PreferenceChoiceViolence                        PreferenceChoice = "violence"
	PreferenceChoiceGraphicViolence                 PreferenceChoice = "graphic_violence"
	PreferenceChoiceReligiousConflict               PreferenceChoice = "religious_conflict"
	PreferenceChoiceMockingCreator                  PreferenceChoice = "mocking_creator"
	PreferenceChoiceMockingOthers                   PreferenceChoice = "mocking_others"
	PreferenceChoicePoliticalEvents                 PreferenceChoice = "political_events"
	PreferenceChoicePartisanEvents                  PreferenceChoice = "partisan_events"
	PreferenceChoiceReligiousEvents                 PreferenceChoice = "religious_events"
	PreferenceChoiceHateGroupEvents                 PreferenceChoice = "hate_group_events"
	PreferenceChoiceEventsIncludingHateGroups       PreferenceChoice = "events_including_hate_groups"
	PreferenceChoiceViolentEvents                   PreferenceChoice = "violent_events"
	PreferenceChoiceAlcoholEvents                   PreferenceChoice = "alcohol_events"
	PreferenceChoiceAdultEvents                     PreferenceChoice = "adult_events"
	PreferenceChoiceNSFW                            PreferenceChoice = "nsfw"
	PreferenceChoiceNotChildSafe                    PreferenceChoice = "not_child_safe"
	PreferenceChoiceProfanity                       PreferenceChoice = "profanity"
	PreferenceChoiceSexualContent                   PreferenceChoice = "sexual_content"
	PreferenceChoicePornography                     PreferenceChoice = "pornography"
	PreferenceChoiceCorporateRisk                   PreferenceChoice = "corporate_risk"
	PreferenceChoiceLegalDrugs                      PreferenceChoice = "legal_drugs"
	PreferenceChoiceIllegalDrugs                    PreferenceChoice = "illegal_drugs"
	PreferenceChoiceReligiousContent                PreferenceChoice = "religious_content"
	PreferenceChoiceOffensiveContent                PreferenceChoice = "offensive_content"
	PreferenceChoiceAskIfUnsure                     PreferenceChoice = "ask_if_unsure"
	PreferenceChoiceReservesRight                   PreferenceChoice = "reserves_right"
	PreferenceChoicePrivateUseOK                    PreferenceChoice = "private_use_ok"
	PreferenceChoiceReportSightings                 PreferenceChoice = "report_sightings"
	PreferenceChoiceMayIntervene                    PreferenceChoice = "may_intervene"
	PreferenceChoiceLetCreatorDecide                PreferenceChoice = "let_creator_decide"
)

// preferenceCardinality is how many choices of a group a value may hold.
type preferenceCardinality int

const (
	// cardinalityExactlyOne groups need one choice, and allow no more.
	cardinalityExactlyOne preferenceCardinality = iota
	// cardinalityAtMostOne groups allow one choice or none.
	cardinalityAtMostOne
	// cardinalityAtLeastOne groups need one choice, and allow more.
	cardinalityAtLeastOne
	// cardinalityAny groups allow any number of choices.
	cardinalityAny
)

// preferenceGroup is a named group of a dimension's choices, of which a value holds as many as its cardinality
// allows.
type preferenceGroup struct {
	name        string
	cardinality preferenceCardinality
	choices     []PreferenceChoice
}

// The names of the groups the cross-group rules (preferenceRules) refer to.
const (
	groupPermission   = "permission"
	groupInforming    = "informing"
	groupConditions   = "conditions"
	groupAllowedKinds = "allowed_kinds"
	groupRefusedKinds = "refused_kinds"
	groupStance       = "stance"
	groupAllowedUses  = "allowed_uses"
	groupExtras       = "extras"
	groupPurposes     = "purposes"
	groupContexts     = "contexts"
	groupContent      = "content"
)

// informingChoices are the choices of the informing groups.
var informingChoices = []PreferenceChoice{
	PreferenceChoiceInformRequired, PreferenceChoiceInformWelcome, PreferenceChoiceInformNotNeeded,
	PreferenceChoiceInformNotWanted,
}

// preferenceVocabularies are the groups of choices of each dimension, in vocabulary order: the order choices are
// stored in (CanonicalisePreferences). A dimension holds each code once; the union of the codes, in the order
// they first appear, is preferenceChoiceValues (TestPreferenceVocabularies).
var preferenceVocabularies = map[PreferenceDimension][]preferenceGroup{
	PreferenceDimensionLicensing: {
		{name: "licence", cardinality: cardinalityExactlyOne, choices: []PreferenceChoice{
			PreferenceChoicePD, PreferenceChoiceCCBY, PreferenceChoiceCCBYSA, PreferenceChoiceCCBYNC,
			PreferenceChoiceCCBYNCSA, PreferenceChoiceCCBYND, PreferenceChoiceCCBYNCND, PreferenceChoiceARR,
			PreferenceChoiceOther, PreferenceChoiceVariesByWork,
		}},
	},
	PreferenceDimensionPerformancePermission: {
		{name: groupPermission, cardinality: cardinalityExactlyOne, choices: []PreferenceChoice{
			PreferenceChoiceNotGiven, PreferenceChoiceAskFirst, PreferenceChoiceAskRepresentative,
			PreferenceChoiceGroupMembers, PreferenceChoiceGiven,
		}},
		{name: groupInforming, cardinality: cardinalityAtMostOne, choices: informingChoices},
	},
	PreferenceDimensionAdaptations: {
		{name: groupPermission, cardinality: cardinalityExactlyOne, choices: []PreferenceChoice{
			PreferenceChoiceEncouraged, PreferenceChoiceAllowed, PreferenceChoiceAllowedWithConditions,
			PreferenceChoiceAskFirst, PreferenceChoiceLiveOnly, PreferenceChoiceNotAllowed,
		}},
		{name: groupConditions, cardinality: cardinalityAny, choices: []PreferenceChoice{
			PreferenceChoiceFairUseOnly, PreferenceChoiceSeriousOnly, PreferenceChoiceRespectfulOnly,
			PreferenceChoiceMinorChangesOnly, PreferenceChoiceShowBeforePerforming,
		}},
		{name: groupInforming, cardinality: cardinalityAtMostOne, choices: informingChoices},
		{name: groupAllowedKinds, cardinality: cardinalityAny, choices: []PreferenceChoice{
			PreferenceChoiceAllowNewWords, PreferenceChoiceAllowFanVideos, PreferenceChoiceAllowStoryChanges,
			PreferenceChoiceAllowShortening, PreferenceChoiceAllowExtending, PreferenceChoiceAllowImprovements,
			PreferenceChoiceAllowMusicalSetting, PreferenceChoiceAllowOwnVersion, PreferenceChoiceAllowStageUse,
		}},
		{name: groupRefusedKinds, cardinality: cardinalityAny, choices: []PreferenceChoice{
			PreferenceChoiceRefuseNewWords, PreferenceChoiceRefuseFanVideos, PreferenceChoiceRefuseStoryChanges,
			PreferenceChoiceRefuseShortening, PreferenceChoiceRefuseExtending, PreferenceChoiceRefuseImprovements,
			PreferenceChoiceRefuseMusicalSetting, PreferenceChoiceRefuseOwnVersion, PreferenceChoiceRefuseStageUse,
		}},
	},
	PreferenceDimensionAttribution: {
		{name: "name", cardinality: cardinalityAtLeastOne, choices: []PreferenceChoice{
			PreferenceChoiceStageName, PreferenceChoiceSCAName, PreferenceChoiceLegalName,
			PreferenceChoicePersonaAndLegalName, PreferenceChoiceKnownName, PreferenceChoiceNameWithAffiliation,
		}},
		{name: "timing", cardinality: cardinalityAtMostOne, choices: []PreferenceChoice{
			PreferenceChoiceCreditBefore, PreferenceChoiceCreditAfter, PreferenceChoiceCreditBeforeAndAfter,
			PreferenceChoiceCreditOncePerSet, PreferenceChoiceCreditAnyTime, PreferenceChoiceCreditWhenAsked,
		}},
		{name: groupExtras, cardinality: cardinalityAny, choices: []PreferenceChoice{
			PreferenceChoiceMentionWebsite, PreferenceChoiceMentionSource, PreferenceChoiceMentionShop,
			PreferenceChoiceMentionPerformance, PreferenceChoiceLinkToCreator, PreferenceChoiceWrittenCredit,
			PreferenceChoiceQRCode,
		}},
	},
	PreferenceDimensionCommercialUse: {
		{name: groupStance, cardinality: cardinalityExactlyOne, choices: []PreferenceChoice{
			PreferenceChoiceNonCommercialOnly, PreferenceChoiceAllowed, PreferenceChoiceAllowedWithConditions,
			PreferenceChoiceAskFirst, PreferenceChoiceNotAllowed,
		}},
		{name: groupAllowedUses, cardinality: cardinalityAny, choices: []PreferenceChoice{
			PreferenceChoiceTipsAndPrizes, PreferenceChoicePaidPerformance, PreferenceChoiceTicketedPerformance,
			PreferenceChoiceThirdPartyCommercial, PreferenceChoiceMonetisedUploads, PreferenceChoicePaidRecording,
			PreferenceChoiceStreamingIncome, PreferenceChoiceSellDownloads, PreferenceChoiceSellPhysicalRecordings,
			PreferenceChoiceSellPrint, PreferenceChoiceResellCreatorCopies, PreferenceChoiceMerchandise,
			PreferenceChoiceAdvertising,
		}},
		{name: groupConditions, cardinality: cardinalityAny, choices: []PreferenceChoice{
			PreferenceChoiceWithPermission, PreferenceChoiceWithAttribution, PreferenceChoiceWithNotification,
			PreferenceChoiceCreatorFirst, PreferenceChoiceProfitShare, PreferenceChoiceIndividualsOnly,
			PreferenceChoiceStandaloneSingle, PreferenceChoiceCompilationOnly, PreferenceChoiceGroupMembers,
			PreferenceChoiceIncomeCap, PreferenceChoiceAmateursOnly,
		}},
	},
	PreferenceDimensionRecording: {
		{name: "writing", cardinality: cardinalityAtMostOne, choices: []PreferenceChoice{
			PreferenceChoiceNoWriting, PreferenceChoiceOralOnly, PreferenceChoiceTrustedAudiencesOnly,
			PreferenceChoicePrivateNotebook, PreferenceChoiceSmallPrintOnly, PreferenceChoiceShareInPerson,
			PreferenceChoiceLyricsOnline, PreferenceChoiceUploadsAllowed,
		}},
		{name: "recording_creator", cardinality: cardinalityAtMostOne, choices: []PreferenceChoice{
			PreferenceChoiceNoRecording, PreferenceChoiceNotifyAfter, PreferenceChoiceAskBeforeUpload,
			PreferenceChoiceAskBeforeRecording, PreferenceChoiceSelectiveRecording, PreferenceChoiceFormalOnly,
		}},
	},
	PreferenceDimensionPerformanceEtiquette: {
		{name: "sing_along", cardinality: cardinalityAtMostOne, choices: []PreferenceChoice{
			PreferenceChoiceSingAlongWelcome, PreferenceChoiceJoinInAnyWay, PreferenceChoiceSingAlongFine,
			PreferenceChoiceNoSingAlong, PreferenceChoiceOtherJammingOnly, PreferenceChoiceUnisonOnly,
			PreferenceChoiceNoPercussion, PreferenceChoiceChorusOnly,
		}},
		{name: "competition", cardinality: cardinalityAtMostOne, choices: []PreferenceChoice{
			PreferenceChoiceNoCompetition, PreferenceChoiceCompeteUnlessCreatorCompeting,
			PreferenceChoiceCompeteAfterChecking, PreferenceChoiceNotAgainstCreator,
		}},
		{name: "circles", cardinality: cardinalityAtMostOne, choices: []PreferenceChoice{
			PreferenceChoiceCircleFine, PreferenceChoiceCircleAskBeforehand, PreferenceChoiceCircleQuickAsk,
			PreferenceChoiceCirclePermissionNeeded, PreferenceChoiceCircleNotAllowed,
		}},
		{name: "learning", cardinality: cardinalityAtMostOne, choices: []PreferenceChoice{
			PreferenceChoicePerformFreely, PreferenceChoiceMemorise, PreferenceChoicePractiseThoroughly,
		}},
	},
	PreferenceDimensionObjectionableUse: {
		{name: "scope", cardinality: cardinalityAtLeastOne, choices: []PreferenceChoice{
			PreferenceChoiceNoObjectionablePurposes, PreferenceChoiceNoObjectionableContexts,
			PreferenceChoiceNoObjectionableContent,
		}},
		{name: groupPurposes, cardinality: cardinalityAny, choices: []PreferenceChoice{
			PreferenceChoiceProselytising, PreferenceChoiceAnyPolitics, PreferenceChoicePartisanPolitics,
			PreferenceChoiceHateGroups, PreferenceChoicePrejudice, PreferenceChoiceIllegalActivity,
			PreferenceChoiceReligiousOrPoliticalAdvertising, PreferenceChoiceCorporateAdvertising,
			PreferenceChoiceRestrictedGoodsAdvertising, PreferenceChoiceViolence, PreferenceChoiceGraphicViolence,
			PreferenceChoiceReligiousConflict, PreferenceChoiceMockingCreator, PreferenceChoiceMockingOthers,
		}},
		{name: groupContexts, cardinality: cardinalityAny, choices: []PreferenceChoice{
			PreferenceChoicePoliticalEvents, PreferenceChoicePartisanEvents, PreferenceChoiceReligiousEvents,
			PreferenceChoiceHateGroupEvents, PreferenceChoiceEventsIncludingHateGroups, PreferenceChoiceViolentEvents,
			PreferenceChoiceAlcoholEvents, PreferenceChoiceAdultEvents,
		}},
		{name: groupContent, cardinality: cardinalityAny, choices: []PreferenceChoice{
			PreferenceChoiceNSFW, PreferenceChoiceNotChildSafe, PreferenceChoiceProfanity,
			PreferenceChoiceSexualContent, PreferenceChoicePornography, PreferenceChoiceCorporateRisk,
			PreferenceChoiceLegalDrugs, PreferenceChoiceIllegalDrugs, PreferenceChoiceReligiousContent,
			PreferenceChoiceOffensiveContent,
		}},
		{name: groupExtras, cardinality: cardinalityAny, choices: []PreferenceChoice{
			PreferenceChoiceAskIfUnsure, PreferenceChoiceReservesRight, PreferenceChoicePrivateUseOK,
			PreferenceChoiceReportSightings, PreferenceChoiceMayIntervene, PreferenceChoiceLetCreatorDecide,
		}},
	},
}

// preferenceRule is a rule between the groups of a dimension: a value of the dimension that holds a choice of
// group needs one of requires, if set, and may hold none of excludedBy. A value breaking it holds choices that
// contradict each other (ProblemChoiceConflict).
type preferenceRule struct {
	dimension  PreferenceDimension
	group      string
	requires   []PreferenceChoice
	excludedBy []PreferenceChoice
}

// preferenceRules are the rules between groups:
//   - a performance permission that is not given leaves nothing to be informed of;
//   - an adaptation that is not allowed has no allowed kinds, and adaptation conditions need a permission that
//     has conditions (allowed with conditions, or ask first);
//   - commercial use that is not allowed has no allowed uses, and conditions need a stance that has them;
//   - each list of objectionable uses needs the scope that names it.
var preferenceRules = []preferenceRule{
	{dimension: PreferenceDimensionPerformancePermission, group: groupInforming,
		excludedBy: []PreferenceChoice{PreferenceChoiceNotGiven}},
	{dimension: PreferenceDimensionAdaptations, group: groupAllowedKinds,
		excludedBy: []PreferenceChoice{PreferenceChoiceNotAllowed}},
	{dimension: PreferenceDimensionAdaptations, group: groupConditions,
		requires: []PreferenceChoice{PreferenceChoiceAllowedWithConditions, PreferenceChoiceAskFirst}},
	{dimension: PreferenceDimensionCommercialUse, group: groupAllowedUses,
		excludedBy: []PreferenceChoice{PreferenceChoiceNotAllowed}},
	{dimension: PreferenceDimensionCommercialUse, group: groupConditions,
		requires: []PreferenceChoice{PreferenceChoiceAllowedWithConditions, PreferenceChoiceAskFirst}},
	{dimension: PreferenceDimensionObjectionableUse, group: groupPurposes,
		requires: []PreferenceChoice{PreferenceChoiceNoObjectionablePurposes}},
	{dimension: PreferenceDimensionObjectionableUse, group: groupContexts,
		requires: []PreferenceChoice{PreferenceChoiceNoObjectionableContexts}},
	{dimension: PreferenceDimensionObjectionableUse, group: groupContent,
		requires: []PreferenceChoice{PreferenceChoiceNoObjectionableContent}},
}

// preferenceTwin is a pair of groups of a dimension whose choices pair up by position (allow_new_words and
// refuse_new_words): a value may not hold both choices of a pair.
type preferenceTwin struct {
	dimension PreferenceDimension
	groups    [2]string
}

// preferenceTwins are the twins of the dimensions.
var preferenceTwins = []preferenceTwin{
	{dimension: PreferenceDimensionAdaptations, groups: [2]string{groupAllowedKinds, groupRefusedKinds}},
}

// preferenceChoicesNeedingNotes are the choices that say too little without notes: group_members, which needs the
// group named.
var preferenceChoicesNeedingNotes = []PreferenceChoice{PreferenceChoiceGroupMembers}

// preferenceGroupOf returns the group of dimension named name; every name preferenceRules and preferenceTwins use
// is a group of their dimension (TestPreferenceVocabularies).
func preferenceGroupOf(dimension PreferenceDimension, name string) preferenceGroup {
	i := slices.IndexFunc(preferenceVocabularies[dimension], func(g preferenceGroup) bool { return g.name == name })
	if i < 0 {
		return preferenceGroup{name: name}
	}

	return preferenceVocabularies[dimension][i]
}

// preferenceChoiceOrder is the position of choice in dimension's vocabulary, or false if the dimension does not
// have it.
func preferenceChoiceOrder(dimension PreferenceDimension, choice PreferenceChoice) (int, bool) {
	n := 0
	for _, g := range preferenceVocabularies[dimension] {
		if i := slices.Index(g.choices, choice); i >= 0 {
			return n + i, true
		}
		n += len(g.choices)
	}

	return 0, false
}

// CanonicalisePreferences returns prefs in canonical order, which is the order they are stored and compared in,
// so that reordering a profile is never a change: the preferences in dimension order and each value's choices in
// its dimension's vocabulary order. Unknown dimensions and choices a dimension does not have keep their relative
// order after the known ones (validation reports them, against the order received). prefs is not modified.
func CanonicalisePreferences(prefs []DocPreference) []DocPreference {
	if prefs == nil {
		return nil
	}
	out := make([]DocPreference, 0, len(prefs))
	for _, p := range prefs {
		if p.Value != nil {
			value := PreferenceValue{Choices: slices.Clone(p.Value.Choices), Notes: p.Value.Notes}
			slices.SortStableFunc(value.Choices, func(a, b PreferenceChoice) int {
				return orderOf(preferenceChoiceOrder(p.Dimension, a)) - orderOf(preferenceChoiceOrder(p.Dimension, b))
			})
			p.Value = &value
		}
		out = append(out, p)
	}
	slices.SortStableFunc(out, func(a, b DocPreference) int {
		return orderOf(dimensionOrder(a.Dimension)) - orderOf(dimensionOrder(b.Dimension))
	})

	return out
}

// dimensionOrder is the position of d among the dimensions, or false if it is not one.
func dimensionOrder(d PreferenceDimension) (int, bool) {
	i := slices.Index(preferenceDimensionValues, d)

	return i, i >= 0
}

// orderOf is a position for sorting: i if known, otherwise after every known position.
func orderOf(i int, known bool) int {
	if !known {
		return len(preferenceChoiceValues)
	}

	return i
}
