// +build !linux

package middleware

import (
	"net/http"

	"golang.org/x/net/context"
)

// WrapHandler returns a new handler function wrapping the previous one in the request chain.
func (a AuditMiddleware) WrapHandler(handler func(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error) func(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
		return handler(ctx, w, r, vars)
	}
}
