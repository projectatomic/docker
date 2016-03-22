// +build !linux

package middleware

import (
	"net/http"

	"github.com/docker/docker/api/server/httputils"
	"github.com/docker/docker/daemon"
	"golang.org/x/net/context"
)

// AuditMiddleware logs actions and information about containers when they're started.
func AuditMiddleware(handler httputils.APIFunc, d *daemon.Daemon) httputils.APIFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, vars map[string]string) error {
		return handler(ctx, w, r, vars)
	}
}
