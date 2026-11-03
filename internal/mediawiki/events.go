package mediawiki

import (
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// EventRecord is an event or venue performances are at: one per folded name. A venue is a page of the Venue
// category (Green Dragon), with its page ID; an event is a name performance items give after "at", written as first
// seen. Pages are the pages whose performance items name it, in first-seen order.
type EventRecord struct {
	Name   string
	Kind   domain.EventKind
	PageID int64
	Pages  []string
}

// SimilarEventNames are two events whose folded names are one edit apart ("Pennsic 45", "Pennsic 47"): listed for
// review, never merged.
type SimilarEventNames struct {
	A string
	B string
}

// events collects the events performances are at, seeded with the venue pages.
type events struct {
	records []EventRecord
	byFold  map[string]int
}

func newEvents(venues []Page) *events {
	e := &events{byFold: map[string]int{}}
	for _, v := range venues {
		e.byFold[domain.FoldName(v.Title)] = len(e.records)
		e.records = append(e.records, EventRecord{Name: v.Title, Kind: domain.EventKindVenue, PageID: v.PageID})
	}

	return e
}

// at returns the name of the event named text, as a page names it, creating the event if it is new.
func (e *events) at(text, page string) string {
	fold := domain.FoldName(text)
	if fold == "" {
		return ""
	}
	k, ok := e.byFold[fold]
	if !ok {
		k = len(e.records)
		e.byFold[fold] = k
		e.records = append(e.records, EventRecord{Name: text, Kind: domain.EventKindEvent})
	}
	e.records[k].Pages = appendNew(e.records[k].Pages, page)

	return e.records[k].Name
}

// similar lists the pairs of events whose folded names are one edit apart, in event order.
func (e *events) similar() []SimilarEventNames {
	var out []SimilarEventNames
	for a := range e.records {
		for b := a + 1; b < len(e.records); b++ {
			if withinOneEdit(domain.FoldName(e.records[a].Name), domain.FoldName(e.records[b].Name)) {
				out = append(out, SimilarEventNames{A: e.records[a].Name, B: e.records[b].Name})
			}
		}
	}

	return out
}
