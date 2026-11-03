package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinkRowUpdate(t *testing.T) {
	t.Parallel()

	next := DocLink{URL: linkA, Title: ptr("Editor's title"), Description: ptr("Editor's description"),
		Platform: ptr("Video"), Locator: ptr("0:42"), Roles: []LinkRole{LinkRoleLyrics}}
	row := DocLink{URL: linkA, Title: ptr("Row title"), CollectionID: ptr(refCollection), Locator: ptr("1:00")}
	tests := []struct {
		name string
		row  *DocLink
		prev *DocLink
		want DocLink
	}{
		{name: "NoRow", want: DocLink{URL: linkA, Title: next.Title, Description: next.Description,
			Platform: next.Platform, Locator: next.Locator}},
		{name: "InCurrentDocument_OnlyChangedFields",
			row:  &row,
			prev: &DocLink{URL: linkA, Title: ptr("Old title"), Description: next.Description, Locator: ptr("1:00")},
			want: DocLink{URL: linkA, Title: next.Title, CollectionID: ptr(refCollection), Platform: next.Platform,
				Locator: next.Locator}},
		{name: "InCurrentDocument_Respelled",
			row: &row,
			prev: &DocLink{URL: "https://EXAMPLE.org/a", Title: next.Title, Description: next.Description,
				Platform: next.Platform, Locator: next.Locator},
			want: DocLink{URL: linkA, Title: row.Title, CollectionID: row.CollectionID, Locator: row.Locator}},
		{name: "InCurrentDocument_ClearedField",
			row: &row,
			prev: &DocLink{URL: linkA, Title: next.Title, Description: next.Description, Platform: next.Platform,
				Locator: next.Locator, CollectionID: ptr(refCollection)},
			want: DocLink{URL: linkA, Title: row.Title, Locator: row.Locator}},
		{name: "NewlyAttached_OnlyNullRowFields", row: &row,
			want: DocLink{URL: linkA, Title: row.Title, Description: next.Description, Platform: next.Platform,
				CollectionID: row.CollectionID, Locator: row.Locator}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, LinkRowUpdate(tt.row, tt.prev, next))
		})
	}
}
