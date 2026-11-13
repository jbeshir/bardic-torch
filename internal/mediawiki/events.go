package mediawiki

// EventRecord is an event performances are at: one per folded name. It is a page of the Venue category (Green
// Dragon), with its page ID, or a name performance items give after "at", written as first seen. Pages are the
// pages whose performance items name it, in first-seen order.
type EventRecord struct {
	Name   string
	PageID int64
	Pages  []string
}

// events collects the events performances are at, seeded with the venue pages.
type events struct {
	records []EventRecord
	byFold  map[string]int
}

func newEvents(venues []Page) *events {
	e := &events{byFold: map[string]int{}}
	for _, v := range venues {
		e.byFold[FoldName(v.Title)] = len(e.records)
		e.records = append(e.records, EventRecord{Name: v.Title, PageID: v.PageID})
	}

	return e
}

// at returns the name of the event named text, as a page names it, creating the event if it is new.
func (e *events) at(text, page string) string {
	fold := FoldName(text)
	if fold == "" {
		return ""
	}
	k, ok := e.byFold[fold]
	if !ok {
		k = len(e.records)
		e.byFold[fold] = k
		e.records = append(e.records, EventRecord{Name: text})
	}
	e.records[k].Pages = appendNew(e.records[k].Pages, page)

	return e.records[k].Name
}
