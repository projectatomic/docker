package middleware

import "github.com/docker/docker/daemon"

type AuditMiddleware struct {
	d *daemon.Daemon
}

func NewAuditMiddleware(d *daemon.Daemon) AuditMiddleware {
	return AuditMiddleware{d: d}
}
