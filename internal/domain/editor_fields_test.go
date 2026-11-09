package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEditorFieldsSent_All(t *testing.T) {
	t.Parallel()

	assert.True(t, AllEditorFieldsSent().All())
	assert.False(t, EditorFieldsSent{}.All())
	assert.False(t, EditorFieldsSent{Preferences: true}.All())
	assert.False(t, EditorFieldsSent{LicenceVersion: true}.All())
}
