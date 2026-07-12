package virtualization

import appvirtualization "github.com/opensoha/soha/internal/application/virtualization"

var (
	_ appvirtualization.ConnectionReader     = (*Repository)(nil)
	_ appvirtualization.ConnectionWriter     = (*Repository)(nil)
	_ appvirtualization.DockerLinkRepository = (*Repository)(nil)
	_ appvirtualization.VMRepository         = (*Repository)(nil)
	_ appvirtualization.ImageRepository      = (*Repository)(nil)
	_ appvirtualization.FlavorRepository     = (*Repository)(nil)
	_ appvirtualization.TaskRepository       = (*Repository)(nil)
	_ appvirtualization.TaskQueueRepository  = (*Repository)(nil)
	_ appvirtualization.TaskLogRepository    = (*Repository)(nil)
)
