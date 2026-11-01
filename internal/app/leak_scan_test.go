package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// leakMarker is in every string the leak fixture stores where no public read may show it, so one
// case-insensitive search of a raw response body finds any of them, including strings a row does not name.
const leakMarker = "leakmark"

// leakedMarker returns the first marked text left in a raw JSON response body once the revealed strings are
// removed, or "" if there is none. A revealed string is removed only where it is a whole JSON string value,
// quotes included and case-sensitive. A longer string that merely starts with, ends with or contains a revealed
// string therefore still counts as a leak.
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

// TestLeakedMarker is the leak matrix's self-test: a planted leak next to a revealed string must be found.
func TestLeakedMarker(t *testing.T) {
	t.Parallel()

	const name = "Leakmark Fennel"
	reveals := []string{name, "leakmark-fennel"}

	for _, tt := range []struct {
		name string
		body string
		leak bool
	}{
		{name: "revealed_value", body: `{"name":"Leakmark Fennel","slug":"leakmark-fennel"}`},
		{name: "revealed_in_list", body: `{"data":[{"name":"Leakmark Fennel"},{"name":"Plain"}]}`},
		{name: "no_marker", body: `{"name":"Plain"}`},
		{name: "planted_notes_extend_revealed_name", leak: true,
			body: `{"name":"Leakmark Fennel","notes":"Leakmark Fennel notes"}`},
		{name: "planted_alias_prefixed_by_revealed_name", leak: true,
			body: `{"name":"Leakmark Fennel","aliases":["Leakmark Fennel Legal Alias"]}`},
		{name: "planted_url_extends_revealed_slug", leak: true,
			body: `{"slug":"leakmark-fennel","url":"https://example.org/leakmark-fennel-link"}`},
		{name: "planted_unrelated_marker", leak: true, body: `{"name":"Leakmark Fennel","x":"leakmark/pronoun"}`},
		{name: "revealed_name_with_other_case", leak: true, body: `{"name":"LEAKMARK FENNEL"}`},
		{name: "revealed_name_as_key", leak: true, body: `{"Leakmark Fennel":1,"y":"leakmark"}`},
		{name: "marker_inside_longer_value", leak: true, body: `{"name":"The Leakmark Fennel"}`},
		{name: "marker_in_error_message", leak: true,
			body: `{"code":"invalid_request","message":"unknown term: leakmark-fennel"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			leaked := leakedMarker(tt.body, reveals)
			if tt.leak {
				assert.NotEmpty(t, leaked)
			} else {
				assert.Empty(t, leaked)
			}
		})
	}
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

func TestUnusedReveals(t *testing.T) {
	t.Parallel()

	body := `{"name":"Leakmark Fennel","notes":"Leakmark Fennel notes"}`
	assert.Empty(t, unusedReveals(body, []string{"Leakmark Fennel"}))
	assert.Equal(t, []string{"Leakmark", "leakmark fennel"}, unusedReveals(body, []string{"Leakmark", "Leakmark Fennel", "leakmark fennel"}))
}
