package domaintest

import (
	"fmt"
	"sync/atomic"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// Keys is a KeySource issuing the keys 0000000000000001, 0000000000000002 and so on, safe for concurrent use.
func Keys() domain.KeySource {
	var issued atomic.Int64

	return func() string { return fmt.Sprintf("%016x", issued.Add(1)) }
}
