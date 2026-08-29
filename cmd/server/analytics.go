package main

import (
	"fmt"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"controlplane/internal/analytics"
	"controlplane/internal/cache"
	"controlplane/internal/dashboard"
	analyticsrepo "controlplane/internal/repository/analytics"
)

// analyticsStack owns construction of the analytics subsystem: the hourly
// snapshot service plus the DBOS-scheduled workflow that drives it (see
// internal/analytics/scheduler.go), and the Redis counter the S2S auth
// middleware feeds request volume into. Wiring is isolated here the same way
// newNotificationStack isolates the notification subsystem's wiring.
type analyticsStack struct {
	Service *analytics.Service
	Counter *analytics.RedisCounter
}

// newAnalyticsStack wires the analytics subsystem, including registering its
// DBOS scheduled workflow against the caller-supplied dbosCtx.
func newAnalyticsStack(dbosCtx dbos.Context, gdb *gorm.DB, rdb *redis.Client, gauges *dashboard.Service, appCache cache.Cache) (*analyticsStack, error) {
	counter := analytics.NewRedisCounter(rdb)
	service := analytics.NewService(analyticsrepo.NewRepository(gdb), gauges, counter, appCache)

	if _, err := analytics.NewScheduler(dbosCtx, service); err != nil {
		return nil, fmt.Errorf("register analytics snapshot workflow: %w", err)
	}

	return &analyticsStack{Service: service, Counter: counter}, nil
}
