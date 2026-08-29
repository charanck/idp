package analytics

import (
	"context"
	"fmt"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
)

// snapshotScheduleName/Cron: one snapshot on the hour, every hour. DBOS's
// cron parser runs with seconds enabled, so this needs 6 fields
// (sec min hour day month weekday), not the usual 5.
const (
	snapshotScheduleName = "analytics-snapshot"
	snapshotCron         = "0 0 * * * *"
)

// Scheduler registers Service.RecordSnapshot as a DBOS-scheduled workflow,
// mirroring internal/notification/tasks.go's registration pattern.
type Scheduler struct {
	service *Service
}

// NewScheduler registers the snapshot workflow and its hourly schedule
// against ctx. Must be constructed before dbos.Launch.
func NewScheduler(ctx dbos.Context, service *Service) (*Scheduler, error) {
	s := &Scheduler{service: service}

	dbos.RegisterWorkflow(ctx, s.SnapshotWorkflow)

	if err := dbos.CreateSchedule(ctx, dbos.ScheduleSpec{
		ScheduleName: snapshotScheduleName,
		Schedule:     snapshotCron,
		Workflow:     s.SnapshotWorkflow,
	}); err != nil {
		return nil, fmt.Errorf("create %s schedule: %w", snapshotScheduleName, err)
	}

	return s, nil
}

// SnapshotWorkflow is the DBOS entry point the hourly schedule invokes. The
// actual snapshot computation/write is wrapped in a single step so DBOS can
// checkpoint and safely replay it.
func (s *Scheduler) SnapshotWorkflow(ctx dbos.Context, input dbos.ScheduledWorkflowInput) (any, error) {
	return dbos.RunAsStep(ctx, func(stepCtx context.Context) (any, error) {
		return nil, s.service.RecordSnapshot(stepCtx, input.ScheduledTime)
	})
}
