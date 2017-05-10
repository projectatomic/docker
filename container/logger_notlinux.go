// +build !linux

package container

// configurePlatformLogger takes a logger.CommonContext and adds any
// OS-specific information that is exclusive to a logger.Context.
func configurePlatformLogger(ctx logger.CommonContext, container *Container) logger.Context {
	return logger.Context(ctx)
}
