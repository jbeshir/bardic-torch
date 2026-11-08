package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
)

// argumentError is a tool call whose arguments the tool refuses, answered as a 400 invalid_request. Its message
// names the argument, never its value.
type argumentError struct {
	message string
}

func (e *argumentError) Error() string {
	return e.message
}

// unexpectedArgumentMessage refuses an argument the tool does not take.
const unexpectedArgumentMessage = "the arguments hold one the tool does not take"

// arguments are a tool call's arguments: each argument's JSON, and its decoded value.
type arguments struct {
	raw    map[string]json.RawMessage
	values map[string]any
}

// decodeArguments decodes a tool call's arguments, which must be a JSON object; absent or null arguments are an
// empty one.
func decodeArguments(raw json.RawMessage) (arguments, error) {
	args := arguments{raw: map[string]json.RawMessage{}, values: map[string]any{}}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return args, nil
	}

	if trimmed[0] != '{' || json.Unmarshal(trimmed, &args.raw) != nil || json.Unmarshal(trimmed, &args.values) != nil {
		return arguments{}, &argumentError{message: "the arguments must be a JSON object"}
	}
	return args, nil
}

// operationFor returns the operation a call of t with args calls: t's only operation, or the one its entity_type
// argument chooses.
func (t tool) operationFor(args arguments) (operation, error) {
	if op, ok := t.operations[""]; ok {
		return op, nil
	}

	value, present := args.values[entityTypeArgument]
	if !present {
		return operation{}, &argumentError{message: "missing argument: " + entityTypeArgument}
	}
	entityType, _ := value.(string)
	op, ok := t.operations[entityType]
	if !ok {
		return operation{}, &argumentError{message: "invalid argument: " + entityTypeArgument}
	}
	return op, nil
}

// validate checks args against op's arguments, and entity_type when the tool chooses op by it: no other
// argument, every required one, each path and query argument valid against its parameter's schema, and the body
// a JSON object. The REST operation validates the body's contents, naming the field at fault.
func (op operation) validate(args arguments, byEntityType bool) error {
	allowed := op.arguments()
	if byEntityType {
		allowed = append(allowed, entityTypeArgument)
	}
	for name := range args.values {
		if !slices.Contains(allowed, name) {
			// The name is the caller's, so it is not echoed either.
			return &argumentError{message: unexpectedArgumentMessage}
		}
	}

	for _, name := range op.required() {
		if _, ok := args.values[name]; !ok {
			return &argumentError{message: "missing argument: " + name}
		}
	}

	for _, p := range op.parameters {
		value, ok := args.values[p.name]
		if !ok {
			continue
		}
		if err := op.validators[p.name].Validate(value); err != nil {
			return &argumentError{message: "invalid argument: " + p.name}
		}
	}

	if body, ok := args.values[bodyArgument]; ok {
		if _, isObject := body.(map[string]any); !isObject {
			return &argumentError{message: "invalid argument: " + bodyArgument + ": must be a JSON object"}
		}
	}

	return nil
}

// asArgumentError reports whether err is an argumentError, and returns it.
func asArgumentError(err error) (*argumentError, bool) {
	var argErr *argumentError
	ok := errors.As(err, &argErr)
	return argErr, ok
}
