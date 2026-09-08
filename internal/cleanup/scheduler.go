package cleanup

import (
	"context"
	"fmt"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
)

// scheduleName/Cron: once a month, at midnight on the 1st. DBOS's cron
// parser runs with seconds enabled, so this needs 6 fields (sec min hour day
// month weekday), not the usual 5 - mirroring internal/analytics/scheduler.go.
const (
	scheduleName = "monthly-data-cleanup"
	cronSchedule = "0 0 0 1 * *"
)

// Scheduler registers Service.Run as a DBOS-scheduled workflow, mirroring
// internal/analytics/scheduler.go's registration pattern.
type Scheduler struct {
	service *Service
}

// NewScheduler registers the cleanup workflow and its monthly schedule
// against ctx. Must be constructed before dbos.Launch.
func NewScheduler(ctx dbos.Context, service *Service) (*Scheduler, error) {
	s := &Scheduler{service: service}

	dbos.RegisterWorkflow(ctx, s.CleanupWorkflow)

	// ApplySchedules upserts by schedule_name, unlike CreateSchedule (a plain
	// insert) - this must be idempotent across restarts, since the schedule
	// row from a prior run is still there.
	if err := dbos.ApplySchedules(ctx, []dbos.ScheduleSpec{
		{
			ScheduleName: scheduleName,
			Schedule:     cronSchedule,
			Workflow:     s.CleanupWorkflow,
		},
	}); err != nil {
		return nil, fmt.Errorf("apply %s schedule: %w", scheduleName, err)
	}

	return s, nil
}

// CleanupWorkflow is the DBOS entry point the monthly schedule invokes. The
// actual deletes are wrapped in a single step so DBOS can checkpoint and
// safely replay it.
func (s *Scheduler) CleanupWorkflow(ctx dbos.Context, input dbos.ScheduledWorkflowInput) (any, error) {
	return dbos.RunAsStep(ctx, func(stepCtx context.Context) (any, error) {
		return nil, s.service.Run(stepCtx, input.ScheduledTime)
	})
}
