package domain

import "fmt"

// countingKeys is a KeySource issuing the keys 0000000000000001, 0000000000000002 and so on.
func countingKeys() KeySource {
	issued := 0

	return func() string {
		issued++

		return fmt.Sprintf("%016x", issued)
	}
}
