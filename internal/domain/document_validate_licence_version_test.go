package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const pathLicenceVersion = "/licence_version"

// licensedPiece is a valid piece published under licence, at version if not nil.
func licensedPiece(licence *Licence, version *LicenceVersion) PieceDocument {
	return pieceWith(func(d *PieceDocument) {
		d.LicensingStatus, d.Licence, d.LicenceVersion = LicensingStatusKnown, licence, version
	})
}

func TestValidatePiece_LicenceVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  PieceDocument
		want ProblemCode
	}{
		{name: "creative commons with a version", doc: licensedPiece(ptr(LicenceCCBY), ptr(LicenceVersion40))},
		{name: "every creative commons licence takes a version", doc: licensedPiece(ptr(LicenceCCBYNCND),
			ptr(LicenceVersion25))},
		{name: "creative commons without a version", doc: licensedPiece(ptr(LicenceCCBYSA), nil)},
		{name: "no licence and no version", doc: validPiece()},
		{name: "public domain with a version", doc: licensedPiece(ptr(LicencePD), ptr(LicenceVersion40)),
			want: ProblemOutOfRange},
		{name: "all rights reserved with a version", doc: licensedPiece(ptr(LicenceARR), ptr(LicenceVersion10)),
			want: ProblemOutOfRange},
		{name: "another licence with a version", doc: licensedPiece(ptr(LicenceOther), ptr(LicenceVersion30)),
			want: ProblemOutOfRange},
		{name: "a version with no licence", doc: licensedPiece(nil, ptr(LicenceVersion40)), want: ProblemOutOfRange},
		{name: "an unknown version", doc: licensedPiece(ptr(LicenceCCBY), ptr(LicenceVersion("5.0"))),
			want: ProblemOutOfRange},
	}
	for _, role := range []Role{RoleContributor, RoleModerator} {
		for _, tt := range tests {
			t.Run(string(role)+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				// The document is unchanged, so no source is needed.
				problems := validatePiece(&tt.doc, tt.doc, role, "")
				if tt.want == "" {
					assert.Empty(t, problems)
					return
				}
				assert.Equal(t, []DocumentProblem{{Path: pathLicenceVersion, Code: tt.want}}, problems)
			})
		}

		t.Run(string(role)+"/a changed version needs a licensing source", func(t *testing.T) {
			t.Parallel()
			base := licensedPiece(ptr(LicenceCCBY), ptr(LicenceVersion30))
			changed := licensedPiece(ptr(LicenceCCBY), ptr(LicenceVersion40))
			assert.Equal(t, []DocumentProblem{{Path: pathLicenceVersion, Code: ProblemSourceRequired}},
				validatePiece(&base, changed, role, ""))
			assert.Empty(t, validatePiece(&base, withLicensingLink(changed, ptr(validSource)), role, ""))
		})
		t.Run(string(role)+"/a cleared version needs no source", func(t *testing.T) {
			t.Parallel()
			base := licensedPiece(ptr(LicenceCCBY), ptr(LicenceVersion30))
			assert.Empty(t, validatePiece(&base, licensedPiece(ptr(LicenceCCBY), nil), role, ""))
		})
	}
}
