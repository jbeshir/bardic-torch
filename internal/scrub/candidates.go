package scrub

import (
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// CandidateDecision is the decision on one possible person name of the crawl (mediawiki.PrivacyDecision) and,
// for a private name, the placeholder replacing it everywhere. Text is a name of the crawl, so decisions are only
// written to the private candidates list, never to the repository or the output.
type CandidateDecision struct {
	mediawiki.PrivacyDecision
	Placeholder string
}

// collectPhrases returns the private names of pages (sorted by page ID) and redirects (mediawiki.PrivateNames),
// numbered from 1 in the order they are found, and the decisions with their placeholders.
func collectPhrases(
	pages []mediawiki.Page, redirects []mediawiki.Redirect,
) ([]mediawiki.PrivateName, []CandidateDecision) {
	names, decisions := mediawiki.PrivateNames(pages, redirects)
	numbers := map[string]int{}
	for k, n := range names {
		numbers[mediawiki.FoldName(n.Text)] = k + 1
	}
	out := make([]CandidateDecision, 0, len(decisions))
	for _, d := range decisions {
		c := CandidateDecision{PrivacyDecision: d}
		if d.Private {
			c.Placeholder = fmt.Sprintf(mediawiki.LegalNameFormat, numbers[mediawiki.FoldName(d.Text)])
		}
		out = append(out, c)
	}

	return names, out
}
