package mcp

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jbeshir/bardic-torch/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolSchemas_MatchSpec checks every tool's input schema against the spec, read by another parser: its
// arguments are its operations' path and query parameters and, for an operation with a request body, body (and
// entity_type for a tool with an operation per entity type); the required ones are the operation's required
// parameters and body; it is closed; and every reference in it resolves within its $defs.
func TestToolSchemas_MatchSpec(t *testing.T) {
	t.Parallel()

	doc, err := openapi3.NewLoader().LoadFromData(openapi.Spec)
	require.NoError(t, err)

	for _, tl := range testToolset(t).tools {
		t.Run(tl.Name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(tl.input)
			require.NoError(t, err)
			var input map[string]any
			require.NoError(t, json.Unmarshal(encoded, &input))
			assert.Equal(t, "object", input["type"])
			assert.Equal(t, false, input["additionalProperties"], "the input schema is closed")
			properties, ok := input["properties"].(map[string]any)
			require.True(t, ok)

			wantArguments, wantRequired := specArguments(t, doc, tl.Tool)
			assert.Equal(t, wantArguments, slices.Sorted(maps.Keys(properties)))

			var required []string
			if raw, ok := input["required"].([]any); ok {
				for _, name := range raw {
					required = append(required, name.(string))
				}
			}
			assert.ElementsMatch(t, wantRequired, required)

			for _, ref := range references(input) {
				require.True(t, strings.HasPrefix(ref, defsPrefix), "reference %s is outside $defs", ref)
				assert.NotNil(t, resolvePointer(input, strings.TrimPrefix(ref, "#/")), "reference %s", ref)
			}
		})
	}
}

// specArguments returns, sorted, the arguments the spec says tool takes, and those it needs.
func specArguments(t *testing.T, doc *openapi3.T, tool Tool) (arguments, required []string) {
	t.Helper()

	for _, op := range tool.Operations {
		specOp := doc.Paths.Find(op.Path).GetOperation(op.Method)
		require.NotNil(t, specOp, "%s %s", op.Method, op.Path)
		assert.Equal(t, op.ID, specOp.OperationID)

		for _, p := range specOp.Parameters {
			arguments = append(arguments, p.Value.Name)
			if p.Value.Required {
				required = append(required, p.Value.Name)
			}
		}
		if specOp.RequestBody != nil {
			arguments = append(arguments, bodyArgument)
			if specOp.RequestBody.Value.Required {
				required = append(required, bodyArgument)
			}
		}
	}
	if len(tool.Operations) > 1 {
		// The operation's own arguments are checked once entity_type has chosen it.
		arguments = append(arguments, entityTypeArgument)
		required = []string{entityTypeArgument}
	}

	slices.Sort(arguments)
	return slices.Compact(arguments), required
}

// references returns every $ref in the JSON value.
func references(value any) []string {
	var refs []string
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if ref, ok := child.(string); ok && key == "$ref" {
				refs = append(refs, ref)
				continue
			}
			refs = append(refs, references(child)...)
		}
	case []any:
		for _, child := range v {
			refs = append(refs, references(child)...)
		}
	}
	return refs
}

// resolvePointer returns the value at the JSON pointer within root, or nil.
func resolvePointer(root any, pointer string) any {
	current := root
	for _, segment := range strings.Split(pointer, "/") {
		parent, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = parent[segment]
	}
	return current
}

// allowedOperations are the only operations tools may call: the catalogue's public reads, and what a
// contributor's API token may do with its own drafts, revisions, link reports and repertoire. Moderation, API
// tokens, the account, the contact form, health checks and the MCP server itself are never tools.
var allowedOperations = []string{
	"listPieces", "getPiece", "listPersons", "getPerson", "listTerms", "getTerm", "listCollections",
	"getCollection", "listEvents", "getEvent", "listNeedsWork", "getRevision", "listPersonPerformances",
	"listCollectionCitations", "listEventPerformances",
	"getPieceEditable", "getPersonEditable", "getTermEditable", "getEventEditable", "getCollectionEditable",
	"getPiecePending", "getPersonPending", "getTermPending", "getEventPending", "getCollectionPending",
	"createDraft", "getDraft", "updateDraft", "deleteDraft", "submitRevision", "withdrawRevision",
	"revertRevision", "listMyRevisions", "getMyRevision", "reportLink",
	"listMyRepertoire", "getMyRepertoireEntry", "putMyRepertoireEntry", "deleteMyRepertoireEntry",
	"listPublicRepertoire",
}

// TestMCP_ToolTableCoversOnlyAllowedOperations pins the operations the tools call: each allowed operation is
// called by exactly one tool, and no tool calls any other. Tool names are unique.
func TestMCP_ToolTableCoversOnlyAllowedOperations(t *testing.T) {
	t.Parallel()

	var names, called []string
	for _, tl := range testToolset(t).Tools() {
		names = append(names, tl.Name)
		for _, op := range tl.Operations {
			called = append(called, op.ID)
			assert.NotContains(t, []string{mcpPath, "/v1/me", "/v1/contact", "/healthz", "/readyz"}, op.Path)
			for _, forbidden := range []string{"/v1/moderation/", "/v1/tokens", "/blocked", "/hidden", "/approve",
				"/reject", "/rebase"} {
				assert.NotContains(t, op.Path, forbidden, "tool %s calls %s", tl.Name, op.ID)
			}
		}
	}

	assert.ElementsMatch(t, allowedOperations, called)
	assert.Len(t, slices.Compact(slices.Sorted(slices.Values(names))), len(names), "tool names are unique")
	assert.Len(t, names, len(toolTable))
}

// TestTools_Annotations checks each tool's hints: read only exactly for GET operations, idempotent for PUT ones,
// and destructive for the tools that remove or give up what the caller made.
func TestTools_Annotations(t *testing.T) {
	t.Parallel()

	destructive := []string{toolDeleteDraft, toolWithdrawRevision, "remove_repertoire_entry"}
	idempotent := []string{toolUpdateDraft, "set_repertoire_entry"}
	writes := []string{toolCreateDraft, toolUpdateDraft, toolDeleteDraft, "submit_draft", toolWithdrawRevision,
		"revert_revision", "report_link", "set_repertoire_entry", "remove_repertoire_entry"}
	for _, tl := range testToolset(t).Tools() {
		assert.Equal(t, Annotations{
			ReadOnly:    !slices.Contains(writes, tl.Name),
			Destructive: slices.Contains(destructive, tl.Name),
			Idempotent:  slices.Contains(idempotent, tl.Name),
		}, tl.Annotations, tl.Name)
		assert.NotEmpty(t, tl.Description, tl.Name)
	}
}

// TestNewToolset_RejectsAnUnknownOperation checks that a tool whose operation the spec does not have fails the
// toolset, and so the server's startup.
func TestNewToolset_RejectsAnUnknownOperation(t *testing.T) {
	t.Parallel()

	s, err := parseSpec([]byte("openapi: 3.1.0\ninfo: {title: t, version: \"1\"}\npaths: {}\n"))
	require.NoError(t, err)
	_, err = newTool(s, toolDefinition{name: toolGetPiece, operation: "getPiece"})
	require.ErrorIs(t, err, errSpec)

	_, err = NewToolset([]byte("not: [valid"))
	require.Error(t, err)
}
