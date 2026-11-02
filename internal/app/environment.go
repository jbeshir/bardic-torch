package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// MustGetEnvAsString returns the value of the named environment variable, panicking if it is not set.
// An empty value counts as set.
func MustGetEnvAsString(ctx context.Context, name string) string {
	s, exists := os.LookupEnv(name)
	if !exists {
		logger := domain.LoggerFromContext(ctx)
		logger.ErrorContext(ctx, "environment variable missing", "variable_name", name)
		panic(fmt.Sprintf("missing environment variable [%s]", name))
	}

	return s
}

// MustGetEnvAsInt returns the named environment variable parsed as an integer, panicking if it is
// missing or not an integer.
func MustGetEnvAsInt(ctx context.Context, name string) int {
	s := MustGetEnvAsString(ctx, name)

	v, err := strconv.Atoi(s)
	if err != nil {
		logger := domain.LoggerFromContext(ctx)
		logger.ErrorContext(ctx, "unable to parse environment variable as integer",
			"variable_name", name,
			"variable_value", s,
		)
		panic(fmt.Sprintf("unable to parse environment variable as integer [%s]: %s", name, s))
	}

	return v
}

// MustGetEnvAsStrings returns the named environment variable split on commas, with each entry trimmed
// of surrounding whitespace and empty entries dropped, panicking if it is not set.
func MustGetEnvAsStrings(ctx context.Context, name string) []string {
	var values []string
	for _, v := range strings.Split(MustGetEnvAsString(ctx, name), ",") {
		if v = strings.TrimSpace(v); v != "" {
			values = append(values, v)
		}
	}

	return values
}
