package container

import (
	"github.com/docker/docker/daemon/logger"
)

// configurePlatformLogger takes a logger.CommonContext and adds any
// OS-specific information that is exclusive to a logger.Context.
func configurePlatformLogger(ctx logger.CommonContext, container *Container) logger.Context {
	return logger.Context{
		CommonContext:   ctx,
		ContainerCGroup: *container.Spec.Linux.CgroupsPath,
	}
}
