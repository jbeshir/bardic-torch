package domain

import "slices"

// The conservation suggestion (docs/PLAN.md §2.3): a conservation status derived from how many distinct places a
// piece's content can be found in now. It is read-only: a moderator confirms it by editing the piece's
// conservation status like any sourced field.

// ConservationSuggestion is the status SuggestConservation derives, with the places it counts: the distinct place
// types, in LinkPlaceType's declaration order.
type ConservationSuggestion struct {
	Status     ConservationStatus
	PlaceCount int
	Places     []LinkPlaceType
}

// SuggestionLink is what SuggestConservation needs of a visible link: its place type, roles and liveness.
type SuggestionLink struct {
	PlaceType *LinkPlaceType
	Roles     []LinkRole
	Liveness  Liveness
}

// contentRoles are the link roles that make a link a place the piece's content is found: not a licensing source,
// a website, an external profile or another role.
var contentRoles = []LinkRole{
	LinkRoleOfficial, LinkRoleLyrics, LinkRoleText, LinkRoleSheetMusic, LinkRoleDanceInstructions,
	LinkRoleTextSource, LinkRoleTranslation, LinkRolePerformance,
}

// The place counts at which the suggestion changes: fewer than endangeredPlaces is critically endangered, fewer
// than lowRiskPlaces endangered (exactly three places counts as endangered, docs/PLAN.md §2), and more low risk.
const (
	endangeredPlaces = 1
	lowRiskPlaces    = 4
)

// SuggestConservation derives a conservation status for a piece with status and links, which must be its visible
// links only. Its places are the distinct place types of the links that are not dead (an unknown or blocked link
// counts as live), carry a content role and have a place type: none is critically endangered, one to three
// endangered, four or more low risk. A restricted piece has no suggestion (nil): restricted overrides everything.
func SuggestConservation(status ConservationStatus, links []SuggestionLink) *ConservationSuggestion {
	if status == ConservationStatusRestricted {
		return nil
	}
	found := map[LinkPlaceType]bool{}
	for _, l := range links {
		if l.PlaceType != nil && l.Liveness != LivenessDead &&
			slices.ContainsFunc(l.Roles, func(r LinkRole) bool { return slices.Contains(contentRoles, r) }) {
			found[*l.PlaceType] = true
		}
	}
	places := []LinkPlaceType{}
	for _, placeType := range linkPlaceTypeValues {
		if found[placeType] {
			places = append(places, placeType)
		}
	}
	suggestion := ConservationSuggestion{Status: ConservationStatusLowRisk, PlaceCount: len(places), Places: places}
	switch {
	case len(places) < endangeredPlaces:
		suggestion.Status = ConservationStatusCriticallyEndangered
	case len(places) < lowRiskPlaces:
		suggestion.Status = ConservationStatusEndangered
	}

	return &suggestion
}

// PieceSuggestionLinks are the suggestion links of a public piece's links, which are its visible links.
func PieceSuggestionLinks(links []PieceLink) []SuggestionLink {
	out := make([]SuggestionLink, 0, len(links))
	for _, l := range links {
		out = append(out, SuggestionLink{PlaceType: l.PlaceType, Roles: l.Roles, Liveness: l.Liveness})
	}

	return out
}

// DocumentSuggestionLinks are the suggestion links of a piece document's links that are not hidden now, given
// states, their link states by the document's URL: a link without a state (not stored yet) is not hidden and of
// unknown liveness.
func DocumentSuggestionLinks(d PieceDocument, states []LinkState) []SuggestionLink {
	byURL := map[string]LinkState{}
	for _, s := range states {
		byURL[s.URL] = s
	}
	out := make([]SuggestionLink, 0, len(d.Links))
	for _, l := range d.Links {
		state := byURL[l.URL]
		if state.IsHidden {
			continue
		}
		liveness := LivenessUnknown
		if state.Liveness != nil {
			liveness = *state.Liveness
		}
		out = append(out, SuggestionLink{PlaceType: l.PlaceType, Roles: l.Roles, Liveness: liveness})
	}

	return out
}
