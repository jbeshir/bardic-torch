package mysql

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// TestRevisionStatesMirrorDomain checks that each state and plan of documents of a kind known at run time has a
// field for every field of the domain type it mirrors, so that a field added to the domain type and not mirrored
// fails here rather than reaching the planner as its zero value.
func TestRevisionStatesMirrorDomain(t *testing.T) {
	// renamed are the domain fields the mirrors name differently.
	renamed := map[string]string{"Visibility": "vis"}
	// unmirrored are the domain fields no mirror carries, with the reason.
	unmirrored := map[string]string{
		"ApprovalPlan.PendingReferences": "the approval locks and approves the pending references itself",
	}

	tests := []struct {
		name   string
		domain reflect.Type
		mirror reflect.Type
	}{
		{"SubmitState", reflect.TypeFor[domain.SubmitState[domain.PieceDocument]](), reflect.TypeFor[submitState]()},
		{"ApprovalState", reflect.TypeFor[domain.ApprovalState[domain.PieceDocument]](),
			reflect.TypeFor[approvalState]()},
		{"ApprovalPlan", reflect.TypeFor[domain.ApprovalPlan[domain.PieceDocument]](), reflect.TypeFor[approvalPlan]()},
		{"RebaseState", reflect.TypeFor[domain.RebaseState[domain.PieceDocument]](), reflect.TypeFor[rebaseState]()},
		{"RevertState", reflect.TypeFor[domain.RevertState[domain.PieceDocument]](), reflect.TypeFor[revertState]()},
		{"RevertPlan", reflect.TypeFor[domain.RevertPlan[domain.PieceDocument]](), reflect.TypeFor[revertPlan]()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := range tt.domain.NumField() {
				name := tt.domain.Field(i).Name
				if _, skip := unmirrored[tt.name+"."+name]; skip {
					continue
				}
				mirrored, ok := renamed[name]
				if !ok {
					mirrored = strings.ToLower(name[:1]) + name[1:]
					if name == "ID" {
						mirrored = "id"
					}
				}
				_, found := tt.mirror.FieldByName(mirrored)
				assert.True(t, found, "%s has field %s, which %s lacks as %s", tt.domain, name, tt.mirror, mirrored)
			}
		})
	}
}

func TestEncodeFlags_NilIsEmptyArray(t *testing.T) {
	encoded, err := encodeFlags(nil)
	require.NoError(t, err)
	assert.JSONEq(t, "[]", string(encoded))

	encoded, err = encodeFlags([]domain.Flag{{Code: domain.FlagPossibleDuplicate}})
	require.NoError(t, err)
	assert.JSONEq(t, `[{"code":"possible_duplicate","path":""}]`, string(encoded))
}

func TestDecodeJSONColumn_EmptyIsZero(t *testing.T) {
	flags, err := decodeJSONColumn[[]domain.Flag]("flags", nil)
	require.NoError(t, err)
	assert.NotNil(t, flags)
	assert.Empty(t, flags)

	flags, err = decodeJSONColumn[[]domain.Flag]("flags", []byte("null"))
	require.NoError(t, err)
	assert.NotNil(t, flags)

	keys, err := decodeJSONColumn[domain.ElementKeySet]("received keys", []byte{})
	require.NoError(t, err)
	assert.NotNil(t, keys)
	assert.Empty(t, keys)

	_, err = decodeJSONColumn[[]domain.Flag]("flags", []byte("{"))
	require.ErrorContains(t, err, "decoding flags")
}
