// Package mcp serves the API's MCP tools: a facade over the REST operations for API tokens, at POST /v1/mcp
// (docs/PLAN.md §5.1 "MCP"). Every tool call is an ordinary REST request dispatched to the API's own router, so
// authorisation, rate limits, validation and privacy are the REST operation's.
package mcp

import (
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jbeshir/bardic-torch/internal/domain"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// entityTypeArgument is the argument by which a tool with an operation per entity type chooses its operation.
const entityTypeArgument = "entity_type"

// toolDefinition is one row of the tool table.
type toolDefinition struct {
	name        string
	description string
	// operation is the operationId of the REST operation the tool calls, for a tool with one operation.
	operation string
	// byEntityType are the operationIds of a tool with an operation per entity type, chosen by its entity_type
	// argument.
	byEntityType map[domain.EntityType]string
	// destructive tools remove or give up something the caller made.
	destructive bool
}

// toolTable is every tool the server offers: the only place tools are registered. tools/list lists them by name.
// No tool calls a moderator, token, contact or account operation: a long-lived token must not reach moderator
// views, which carry hidden aliases, user IDs and contact details.
var toolTable = []toolDefinition{
	{name: "search_pieces", operation: "listPieces", description: "Search and filter the public catalogue of " +
		"pieces (songs, poems, stories and more) by words, terms, kinds, conservation status or a credited person."},
	{name: "get_piece", operation: "getPiece", description: "Get a public piece by its slug: its credits, terms, " +
		"links, performances, preferences and conservation status."},
	{name: "search_persons", operation: "listPersons",
		description: "Search and filter the public persons (bards, authors, composers and other contributors)."},
	{name: "get_person", operation: "getPerson", description: "Get a public person by their slug, with their " +
		"profile if they have a page."},
	{name: "list_terms", operation: "listTerms",
		description: "List the public terms of one facet, or of every facet, in tree order, with their counts."},
	{name: "get_term", operation: "getTerm", description: "Get a public term by its slug, with its pieces, or " +
		"for an affiliation its persons, a page at a time."},
	{name: "list_collections", operation: "listCollections",
		description: "List the public collections, such as songbooks, albums and websites, a page at a time."},
	{name: "get_collection", operation: "getCollection",
		description: "Get a public collection by its ID, with its citations."},
	{name: "list_events", operation: "listEvents",
		description: "List the public events and venues, a page at a time."},
	{name: "get_event", operation: "getEvent",
		description: "Get a public event or venue by its slug, with the performances recorded at it."},
	{name: "list_needs_work", operation: "listNeedsWork",
		description: "List the public pieces and persons that need work, such as stubs, unassessed pieces and " +
			"dead links."},
	{name: "get_revision", operation: "getRevision",
		description: "Get an approved revision of a public entity by its ID, with what it changed, as the public sees it."},
	{name: "list_person_performances", operation: "listPersonPerformances",
		description: "List a public person's performances, a page at a time."},
	{name: "list_collection_citations", operation: "listCollectionCitations",
		description: "List a public collection's citations, a page at a time."},
	{name: "list_event_performances", operation: "listEventPerformances",
		description: "List the performances at a public event, a page at a time."},
	{name: "get_editable", description: "Get the editable document of an entity, the starting point of a " +
		"draft that changes it: entity_type chooses the entity, which is addressed by slug, or by collection_id " +
		"for a collection.", byEntityType: map[domain.EntityType]string{
		domain.EntityTypePiece:      "getPieceEditable",
		domain.EntityTypePerson:     "getPersonEditable",
		domain.EntityTypeTerm:       "getTermEditable",
		domain.EntityTypeEvent:      "getEventEditable",
		domain.EntityTypeCollection: "getCollectionEditable",
	}},
	{name: "get_pending", description: "Get the caller's own pending revision of an entity: entity_type " +
		"chooses the entity, which is addressed by slug, or by collection_id for a collection.",
		byEntityType: map[domain.EntityType]string{
			domain.EntityTypePiece:      "getPiecePending",
			domain.EntityTypePerson:     "getPersonPending",
			domain.EntityTypeTerm:       "getTermPending",
			domain.EntityTypeEvent:      "getEventPending",
			domain.EntityTypeCollection: "getCollectionPending",
		}},
	{name: "create_draft", operation: "createDraft", description: "Create a draft that changes an entity, or " +
		"creates one, from a document in the editor format. Submit it with submit_draft."},
	{name: "get_draft", operation: "getDraft", description: "Get one of the caller's drafts by its ID."},
	{name: "update_draft", operation: "updateDraft", description: "Replace the document and summary of one of " +
		"the caller's drafts."},
	{name: "delete_draft", operation: "deleteDraft", destructive: true,
		description: "Discard one of the caller's drafts."},
	{name: "submit_draft", operation: "submitRevision", description: "Submit one of the caller's drafts for " +
		"moderation. A submission made with an API token is always pending until a moderator approves it."},
	{name: "withdraw_revision", operation: "withdrawRevision", destructive: true,
		description: "Withdraw one of the caller's pending revisions from moderation."},
	{name: "revert_revision", operation: "revertRevision", description: "Submit a revision that restores an " +
		"entity to an earlier approved revision; it is pending until a moderator approves it."},
	{name: "list_my_revisions", operation: "listMyRevisions",
		description: "List the caller's own revisions: drafts, pending, approved, rejected and withdrawn."},
	{name: "get_my_revision", operation: "getMyRevision",
		description: "Get one of the caller's own revisions by its ID."},
	{name: "report_link", operation: "reportLink", description: "Report a link a public page shows, by its URL, " +
		"to the moderators: for example a dead link or one to the wrong work."},
	{name: "list_my_repertoire", operation: "listMyRepertoire",
		description: "List the pieces in the caller's repertoire, with their notes and visibility."},
	{name: "get_my_repertoire_entry", operation: "getMyRepertoireEntry",
		description: "Get the caller's repertoire entry for a piece, by the piece's slug."},
	{name: "set_repertoire_entry", operation: "putMyRepertoireEntry", description: "Add a public piece to the " +
		"caller's repertoire, or replace the notes and visibility of its entry."},
	{name: "remove_repertoire_entry", operation: "deleteMyRepertoireEntry", destructive: true,
		description: "Remove a piece from the caller's repertoire."},
	{name: "list_public_repertoire", operation: "listPublicRepertoire", description: "List the public entries " +
		"of a user's repertoire, by the user's public ID."},
}

// Tool is a tool the server offers, as tools/list describes it.
type Tool struct {
	Name        string
	Description string
	// Operations are the REST operations the tool calls: one, or one per entity type.
	Operations  []Operation
	Annotations Annotations
}

// Operation is a REST operation a tool calls.
type Operation struct {
	ID     string
	Method string
	// Path is the path template, such as /v1/drafts/{draft_id}.
	Path string
}

// Annotations are the hints tools/list gives about a tool: whether it only reads, whether it removes or gives up
// something, and whether repeating a call changes nothing more.
type Annotations struct {
	ReadOnly    bool
	Destructive bool
	Idempotent  bool
}

// Toolset is every tool, with the input schemas derived from the OpenAPI spec: what each request's MCP server is
// built from. It is immutable and safe for concurrent use.
type Toolset struct {
	// version is the API's version, from the spec.
	version string
	tools   []tool
}

// tool is a tool of the table, ready to call.
type tool struct {
	Tool
	// operations are the tool's operation, keyed by "", or its operations by entity type.
	operations map[string]operation
	// input is the input schema tools/list shows: every argument of every operation.
	input *jsonschema.Schema
	// sdkTool is the tool as the MCP server registers it.
	sdkTool *sdk.Tool
}

// NewToolset derives every tool's input schema from the OpenAPI spec in specYAML. It fails if a tool's operation
// is not in the spec, or its parameters or body cannot be made an input schema.
func NewToolset(specYAML []byte) (*Toolset, error) {
	s, err := parseSpec(specYAML)
	if err != nil {
		return nil, err
	}

	tools := make([]tool, 0, len(toolTable))
	for _, definition := range toolTable {
		t, err := newTool(s, definition)
		if err != nil {
			return nil, fmt.Errorf("tool %s: %w", definition.name, err)
		}
		tools = append(tools, t)
	}

	return &Toolset{version: s.version(), tools: tools}, nil
}

// Tools returns every tool, in the tool table's order.
func (ts *Toolset) Tools() []Tool {
	tools := make([]Tool, len(ts.tools))
	for i, t := range ts.tools {
		tools[i] = t.Tool
		tools[i].Operations = slices.Clone(t.Operations)
	}
	return tools
}

func newTool(s spec, definition toolDefinition) (tool, error) {
	ids := map[string]string{"": definition.operation}
	if definition.byEntityType != nil {
		ids = map[string]string{}
		for entityType, id := range definition.byEntityType {
			ids[string(entityType)] = id
		}
	}

	operations := map[string]operation{}
	for _, key := range slices.Sorted(maps.Keys(ids)) {
		op, err := s.operation(ids[key])
		if err != nil {
			return tool{}, err
		}
		operations[key] = op
	}

	input, err := inputSchema(definition, operations)
	if err != nil {
		return tool{}, err
	}

	t := tool{
		Tool: Tool{
			Name:        definition.name,
			Description: definition.description,
			Annotations: annotations(definition, operations),
		},
		operations: operations,
		input:      input,
	}
	for _, key := range slices.Sorted(maps.Keys(operations)) {
		op := operations[key]
		t.Operations = append(t.Operations, Operation{ID: op.id, Method: op.method, Path: op.path})
	}
	t.sdkTool = t.registration()

	return t, nil
}

// annotations are a tool's hints: read only if every operation is a GET, idempotent if every one is a PUT.
func annotations(definition toolDefinition, operations map[string]operation) Annotations {
	readOnly, idempotent := true, true
	for _, op := range operations {
		readOnly = readOnly && op.method == http.MethodGet
		idempotent = idempotent && op.method == http.MethodPut
	}
	return Annotations{ReadOnly: readOnly, Destructive: definition.destructive, Idempotent: idempotent}
}

// inputSchema is the input schema of a tool: an object with the arguments of its operations, which must agree on
// the arguments they share, and for a tool with an operation per entity type, entity_type.
func inputSchema(definition toolDefinition, operations map[string]operation) (*jsonschema.Schema, error) {
	properties, defs := object{}, object{}
	for _, key := range slices.Sorted(maps.Keys(operations)) {
		op := operations[key]
		for name, schema := range op.properties() {
			if existing, ok := properties[name]; ok && !reflect.DeepEqual(existing, schema) {
				return nil, fmt.Errorf("argument %s differs between operations: %w", name, errSpec)
			}
			properties[name] = schema
		}
		maps.Copy(defs, op.defs)
	}

	required := operations[""].required()
	if definition.byEntityType != nil {
		properties[entityTypeArgument] = object{
			"type":        "string",
			"enum":        slices.Sorted(maps.Keys(operations)),
			"description": "The type of the entity, which chooses the operation and its arguments.",
		}
		required = []string{entityTypeArgument}
	}

	schema := object{"type": "object", "additionalProperties": false, "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	parsed, err := parseSchema(withDefs(schema, defs))
	if err != nil {
		return nil, err
	}
	if _, err := parsed.Resolve(nil); err != nil {
		return nil, fmt.Errorf("resolving the input schema: %w", err)
	}
	return parsed, nil
}

// registration is the tool as the MCP server registers it. It declares no output schema: a call's result is the
// REST response's body, or an error as text.
func (t tool) registration() *sdk.Tool {
	hints := &sdk.ToolAnnotations{ReadOnlyHint: t.Annotations.ReadOnly, IdempotentHint: t.Annotations.Idempotent}
	if !t.Annotations.ReadOnly {
		// MCP assumes a tool that writes is destructive unless it says otherwise.
		hints.DestructiveHint = new(t.Annotations.Destructive)
	}
	return &sdk.Tool{Name: t.Name, Description: t.Description, InputSchema: t.input, Annotations: hints}
}
