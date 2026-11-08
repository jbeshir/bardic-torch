package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"go.yaml.in/yaml/v3"
)

// bodyArgument is the argument holding an operation's request body.
const bodyArgument = "body"

const (
	componentSchemaPrefix = "#/components/schemas/"
	defsPrefix            = "#/$defs/"
)

var errSpec = errors.New("the OpenAPI spec does not describe the tool's operation")

// object is a JSON object, as the spec's YAML decodes.
type object = map[string]any

// spec is the OpenAPI spec, decoded.
type spec struct {
	root object
	// operations are the spec's operations by operationId.
	operations map[string]specOperation
}

// specOperation is an operation of the spec.
type specOperation struct {
	id     string
	method string
	// path is the path template, such as /v1/drafts/{draft_id}.
	path string
	raw  object
}

// parameter is a path or query parameter of an operation.
type parameter struct {
	name string
	in   string
	// array query parameters are sent once per element.
	array    bool
	required bool
	// schema is the parameter's schema, with references rewritten to #/$defs.
	schema object
}

// operation is a REST operation as a tool calls it: where it is, the arguments it takes and how they are checked.
type operation struct {
	id         string
	method     string
	path       string
	parameters []parameter
	// hasBody operations take a body argument, required if bodyRequired.
	hasBody      bool
	bodyRequired bool
	// bodySchema is the request body's schema, with references rewritten to #/$defs.
	bodySchema object
	// defs are the component schemas the parameter and body schemas refer to, directly or not.
	defs object
	// validators check each path and query parameter's argument.
	validators map[string]*jsonschema.Resolved
}

// arguments returns the name of every argument the operation takes.
func (op operation) arguments() []string {
	names := make([]string, 0, len(op.parameters)+1)
	for _, p := range op.parameters {
		names = append(names, p.name)
	}
	if op.hasBody {
		names = append(names, bodyArgument)
	}
	return names
}

// required returns the name of every argument the operation needs.
func (op operation) required() []string {
	var names []string
	for _, p := range op.parameters {
		if p.required {
			names = append(names, p.name)
		}
	}
	if op.bodyRequired {
		names = append(names, bodyArgument)
	}
	return names
}

// properties returns the input schema property of every argument the operation takes.
func (op operation) properties() object {
	properties := object{}
	for _, p := range op.parameters {
		properties[p.name] = p.schema
	}
	if op.hasBody {
		properties[bodyArgument] = op.bodySchema
	}
	return properties
}

// parseSpec decodes the OpenAPI spec in YAML.
func parseSpec(specYAML []byte) (spec, error) {
	var root object
	if err := yaml.Unmarshal(specYAML, &root); err != nil {
		return spec{}, fmt.Errorf("decoding the OpenAPI spec: %w", err)
	}

	paths, _ := root["paths"].(object)
	operations := map[string]specOperation{}
	for path, item := range paths {
		methods, _ := item.(object)
		for method, raw := range methods {
			op, ok := raw.(object)
			if !ok {
				continue
			}
			if id, ok := op["operationId"].(string); ok {
				operations[id] = specOperation{id: id, method: strings.ToUpper(method), path: path, raw: op}
			}
		}
	}

	return spec{root: root, operations: operations}, nil
}

// version is the spec's info.version.
func (s spec) version() string {
	info, _ := s.root["info"].(object)
	version, _ := info["version"].(string)
	return version
}

// operation returns the operation with operationId id, with its parameters' and body's schemas.
func (s spec) operation(id string) (operation, error) {
	specOp, ok := s.operations[id]
	if !ok {
		return operation{}, fmt.Errorf("operation %s: %w", id, errSpec)
	}

	op := operation{id: id, method: specOp.method, path: specOp.path, validators: map[string]*jsonschema.Resolved{}}
	refs := map[string]bool{}
	rawParameters, _ := specOp.raw["parameters"].([]any)
	for _, raw := range rawParameters {
		p, err := s.parameter(raw, refs)
		if err != nil {
			return operation{}, fmt.Errorf("operation %s: %w", id, err)
		}
		op.parameters = append(op.parameters, p)
	}

	if raw, ok := specOp.raw["requestBody"]; ok {
		body, err := s.resolve(raw)
		if err != nil {
			return operation{}, fmt.Errorf("operation %s: request body: %w", id, err)
		}
		content, _ := body["content"].(object)
		media, _ := content["application/json"].(object)
		schema, ok := media["schema"].(object)
		if !ok {
			return operation{}, fmt.Errorf("operation %s: request body without a JSON schema: %w", id, errSpec)
		}
		op.hasBody = true
		op.bodyRequired, _ = body["required"].(bool)
		op.bodySchema = rewriteRefs(schema, refs)
	}

	defs, err := s.defs(refs)
	if err != nil {
		return operation{}, fmt.Errorf("operation %s: %w", id, err)
	}
	op.defs = defs

	for _, p := range op.parameters {
		validator, err := resolveSchema(withDefs(p.schema, defs))
		if err != nil {
			return operation{}, fmt.Errorf("operation %s: parameter %s: %w", id, p.name, err)
		}
		op.validators[p.name] = validator
	}

	return op, nil
}

// parameter returns the path or query parameter raw describes, noting the component schemas it refers to in refs.
func (s spec) parameter(raw any, refs map[string]bool) (parameter, error) {
	p, err := s.resolve(raw)
	if err != nil {
		return parameter{}, err
	}

	name, _ := p["name"].(string)
	in, _ := p["in"].(string)
	schema, ok := p["schema"].(object)
	if name == "" || (in != "path" && in != "query") || !ok {
		return parameter{}, fmt.Errorf("parameter %q in %q: only path and query parameters with schemas: %w",
			name, in, errSpec)
	}
	required, _ := p["required"].(bool)
	schemaType, _ := schema["type"].(string)

	return parameter{
		name: name, in: in, array: schemaType == "array", required: required, schema: rewriteRefs(schema, refs),
	}, nil
}

// resolve returns the object raw is, or the one it refers to with $ref, within the spec.
func (s spec) resolve(raw any) (object, error) {
	obj, ok := raw.(object)
	if !ok {
		return nil, fmt.Errorf("not an object: %w", errSpec)
	}
	ref, ok := obj["$ref"].(string)
	if !ok {
		return obj, nil
	}

	var current any = s.root
	for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		parent, _ := current.(object)
		current = parent[segment]
	}
	resolved, ok := current.(object)
	if !ok || !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("reference %s: %w", ref, errSpec)
	}
	return resolved, nil
}

// defs returns the component schemas named in refs and every component schema they refer to, by name, with their
// references rewritten to #/$defs.
func (s spec) defs(refs map[string]bool) (object, error) {
	components, _ := s.root["components"].(object)
	schemas, _ := components["schemas"].(object)

	defs := object{}
	for pending := slices.Collect(maps.Keys(refs)); len(pending) > 0; {
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, done := defs[name]; done {
			continue
		}
		schema, ok := schemas[name].(object)
		if !ok {
			return nil, fmt.Errorf("schema %s: %w", name, errSpec)
		}

		nested := map[string]bool{}
		defs[name] = rewriteRefs(schema, nested)
		for ref := range nested {
			pending = append(pending, ref)
		}
	}

	return defs, nil
}

// rewriteRefs returns a copy of the schema value with every reference to a component schema rewritten to
// #/$defs, noting the names referred to in refs. It drops OpenAPI's example keyword, which JSON Schema does not
// have (JSON Schema's examples are kept).
func rewriteRefs[T any](value T, refs map[string]bool) T {
	rewritten, _ := rewriteValue(value, refs).(T)
	return rewritten
}

func rewriteValue(value any, refs map[string]bool) any {
	switch v := value.(type) {
	case object:
		rewritten := make(object, len(v))
		for key, child := range v {
			if key == "example" {
				continue
			}
			if ref, ok := child.(string); ok && key == "$ref" && strings.HasPrefix(ref, componentSchemaPrefix) {
				// A reference may point into a component schema, as #/components/schemas/X/properties/y does.
				pointer := strings.TrimPrefix(ref, componentSchemaPrefix)
				name, _, _ := strings.Cut(pointer, "/")
				refs[name] = true
				rewritten[key] = defsPrefix + pointer
				continue
			}
			rewritten[key] = rewriteValue(child, refs)
		}
		return rewritten
	case []any:
		rewritten := make([]any, len(v))
		for i, child := range v {
			rewritten[i] = rewriteValue(child, refs)
		}
		return rewritten
	default:
		return v
	}
}

// withDefs returns schema with defs as its $defs, if it has any.
func withDefs(schema, defs object) object {
	if len(defs) == 0 {
		return schema
	}
	root := maps.Clone(schema)
	root["$defs"] = defs
	return root
}

// resolveSchema parses and resolves the JSON schema schema.
func resolveSchema(schema object) (*jsonschema.Resolved, error) {
	parsed, err := parseSchema(schema)
	if err != nil {
		return nil, err
	}
	resolved, err := parsed.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolving the schema: %w", err)
	}
	return resolved, nil
}

// parseSchema converts schema to a jsonschema.Schema.
func parseSchema(schema object) (*jsonschema.Schema, error) {
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encoding the schema: %w", err)
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		return nil, fmt.Errorf("decoding the schema: %w", err)
	}
	return &parsed, nil
}
