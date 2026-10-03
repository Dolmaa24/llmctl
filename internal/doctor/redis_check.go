package doctor

import (
	"context"
	"time"
)

type RedisCheck struct{}

func (RedisCheck) Name() string {
	return "redis_check"
}

func (RedisCheck) Run(_ context.Context) CheckResult {
	return CheckResult{
		Name:      "redis_check",
		Status:    StatusWarn,
		Message:   "placeholder: redis diagnostics are scheduled for Phase 2",
		CheckedAt: time.Now(),
	}
}
