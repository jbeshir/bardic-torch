package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPersonIsStub(t *testing.T) {
	t.Parallel()

	assert.False(t, PersonIsStub(false, PersonStatusActive))
	assert.False(t, PersonIsStub(false, PersonStatusDeceased))
	assert.True(t, PersonIsStub(true, PersonStatusActive))
	assert.True(t, PersonIsStub(false, PersonStatusPrefersNoPage))
}

func TestCreditNames(t *testing.T) {
	t.Parallel()

	person := func(id int64, name string) CreditedPerson { return CreditedPerson{ID: id, Name: name} }
	cases := map[string]struct {
		persons []CreditedPerson
		want    []string
	}{
		"none": {},
		"in_credit_order": {persons: []CreditedPerson{person(2, "Wren of the Vale"), person(1, "Ash Tallow")},
			want: []string{"Wren of the Vale", "Ash Tallow"}},
		// A person with several credits is named once, at the first.
		"distinct_persons": {
			persons: []CreditedPerson{person(1, "Ash Tallow"), person(2, "Wren of the Vale"), person(1, "Ash Tallow")},
			want:    []string{"Ash Tallow", "Wren of the Vale"},
		},
		// Two persons who share a name are two credits.
		"same_name": {persons: []CreditedPerson{person(1, "Rook"), person(2, "Rook")}, want: []string{"Rook", "Rook"}},
		"at_most_five": {
			persons: []CreditedPerson{person(1, "A"), person(1, "A"), person(2, "B"), person(3, "C"), person(4, "D"),
				person(5, "E"), person(6, "F")},
			want: []string{"A", "B", "C", "D", "E"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, CreditNames(tc.persons))
		})
	}
}
