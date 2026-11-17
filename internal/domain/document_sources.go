package domain

// equalPointers reports whether a and b are both nil or point to equal values.
func equalPointers[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
