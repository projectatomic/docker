package logger

// Context provides enough information for a logging driver to do its function.
type Context struct {
	// These fields are shared across all definitions.
	CommonContext
	// Fields below this point are platform-specific.
	ContainerCGroup string
}

// CGroup returns the name of the container's cgroup.
func (ctx *Context) CGroup() (string, error) {
	return ctx.ContainerCGroup, nil
}
