package command

import "context"

// Command is the generic interface for all commands.
// Req is the request type and Res is the result type.
type Command[Req, Res any] interface {
	Execute(ctx context.Context, req Req) (Res, error)
}
