package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidContactEmail(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"wren@example.org":          true,
		"wren.placeholder+x@a.b":    true,
		"wren@localhost":            true,
		"wrén@exämple.org":          true,
		"":                          false,
		"wren":                      false,
		"@example.org":              false,
		"wren@":                     false,
		"wren@exa@mple.org":         false,
		"wren placeholder@a.org":    false,
		" wren@example.org":         false,
		"wren@example.org\n":        false,
		"wren@exam\u0000ple.org":    false,
		"wren@example.org\u00a0":    false,
		"wren\u0007@example.org":    false,
		"wren@example.org\u2028x.y": false,
	}
	for email, want := range cases {
		assert.Equal(t, want, ValidContactEmail(email), "%q", email)
	}
}
