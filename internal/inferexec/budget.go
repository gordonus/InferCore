package inferexec

import (
	"context"
	"errors"
	"net/http"
)

// InferBudgetHTTPStatus returns 504 only when the infer-scoped context hit its deadline.
func InferBudgetHTTPStatus(ctx context.Context, err error) (status int, code string, ok bool) {
	if err == nil {
		return 0, "", false
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		return 0, "", false
	}
	if ctx.Err() == nil {
		return 0, "", false
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, "gateway_timeout", true
	}
	return 0, "", false
}
