package sched_db

type ScheduledTaskStatus string

const (
	ScheduledTaskStatusPending ScheduledTaskStatus = "pending"
	ScheduledTaskStatusRunning ScheduledTaskStatus = "running"
	ScheduledTaskStatusStopped ScheduledTaskStatus = "stopped"
)
