package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultDocument(t *testing.T) {
	t.Parallel()
	assert.Equal(t, DefaultPieceDocument(), DefaultDocument[PieceDocument]())
	assert.Equal(t, DefaultPersonDocument(), DefaultDocument[PersonDocument]())
	assert.Equal(t, EventDocument{}, DefaultDocument[EventDocument]())
}

// TestApprovalFlags checks the flags a moderation view shows are those approval gates on: the submission's own
// flags kept, the others found again in the document, each once.
func TestApprovalFlags(t *testing.T) {
	t.Parallel()
	flags := ApprovalFlags(ApprovalState[EventDocument]{
		Revision: EventDocument{Name: "Midsummer Fair"}, ApprovalInputs: ApprovalInputs{
			HiddenAliases: []string{"Wren Placeholder"}, Summary: aliasSummary, PossibleDuplicate: true,
			StoredFlags: []Flag{{Code: FlagHiddenDataConflict, Path: "/name"}, {Code: FlagPossibleDuplicate}},
		},
	})

	assert.Equal(t, []Flag{
		{Code: FlagHiddenDataConflict, Path: "/name"}, {Code: FlagHiddenAliasInSummary, Path: pathSummaryFlag},
		{Code: FlagPossibleDuplicate},
	}, flags)
}
