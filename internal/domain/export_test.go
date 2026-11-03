package domain

import "reflect"

// ValidateEnums exposes validateEnums to the external tests.
var ValidateEnums = validateEnums

// MergeBase exposes the merge tests' base document to the external fuzz tests.
var MergeBase = mergeBase

// MutatePiece exposes the merge tests' document mutator to the external fuzz tests.
var MutatePiece = mutatePiece

// EqualValues reports whether a and b are equal as the merge compares them: an empty list equals no list.
func EqualValues(a, b any) bool {
	return equalValues(reflect.ValueOf(a), reflect.ValueOf(b))
}
