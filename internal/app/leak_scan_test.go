package app

import (
	"encoding/json"
	"strings"
)

// leakMarker is in every string the leak fixture stores where no public read may show it, so one
// case-insensitive search of a raw response body finds any of them, including strings a row does not name.
const leakMarker = "leakmark"

// leakedMarker returns the first marked text left in a raw JSON response body once the revealed strings are
// removed, or "" if there is none. A revealed string is removed only where it is a whole JSON string, value or
// key, quotes included and case-sensitive. A longer string that merely starts with, ends with or contains a
// revealed string therefore still counts as a leak.
func leakedMarker(body string, reveals []string) string {
	for _, revealed := range reveals {
		quoted, err := json.Marshal(revealed)
		if err != nil {
			panic(err)
		}
		body = strings.ReplaceAll(body, string(quoted), `""`)
	}
	lower := strings.ToLower(body)
	i := strings.Index(lower, leakMarker)
	if i < 0 {
		return ""
	}
	return body[max(0, i-20):min(len(body), i+40)]
}

// unusedReveals returns the revealed strings that the raw JSON response body does not show as a whole string
// value, so that a row cannot list a reveal that has stopped doing anything.
func unusedReveals(body string, reveals []string) []string {
	var unused []string
	for _, revealed := range reveals {
		quoted, err := json.Marshal(revealed)
		if err != nil {
			panic(err)
		}
		if !strings.Contains(body, string(quoted)) {
			unused = append(unused, revealed)
		}
	}
	return unused
}
