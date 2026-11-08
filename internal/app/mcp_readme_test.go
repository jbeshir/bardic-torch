package app

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/transport/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readmeToolRow is a tool's row in the README's MCP tool list: its name, description and annotations.
func readmeToolRow(tool mcp.Tool) string {
	var hints []string
	if tool.Annotations.ReadOnly {
		hints = append(hints, "read-only")
	}
	if tool.Annotations.Destructive {
		hints = append(hints, "destructive")
	}
	if tool.Annotations.Idempotent {
		hints = append(hints, "idempotent")
	}
	annotations := strings.Join(hints, ", ")
	if annotations == "" {
		annotations = "—"
	}
	return "| `" + tool.Name + "` | " + tool.Description + " | " + annotations + " |"
}

// TestMCP_ToolTableMatchesReadme checks that the README's MCP section lists every tool, in the tool table's order,
// with its description and annotations exactly as tools/list gives them.
func TestMCP_ToolTableMatchesReadme(t *testing.T) {
	t.Parallel()

	readme, err := fs.ReadFile(repositoryRoot(), "README.md")
	require.NoError(t, err)
	_, section, found := strings.Cut(string(readme), "\n## MCP\n")
	require.True(t, found, "the README has no MCP section")
	section, _, _ = strings.Cut(section, "\n## ")

	var listed []string
	for line := range strings.Lines(section) {
		if strings.HasPrefix(line, "| `") {
			listed = append(listed, strings.TrimRight(line, "\n"))
		}
	}

	var want []string
	for _, tool := range testMCPTools(t).Tools() {
		want = append(want, readmeToolRow(tool))
	}
	assert.Equal(t, want, listed, "the README's tool list must be:\n%s", strings.Join(want, "\n"))
}
