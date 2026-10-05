package doctor

import (
	"context"
	"fmt"
	"time"
)

type AuthCheck struct {
	Provider string
}

func (c AuthCheck) Name() string {
	if c.Provider == "" {
		return "auth_check"
	}
	return fmt.Sprintf("auth:%s", c.Provider)
}

func (c AuthCheck) Run(_ context.Context) CheckResult {
	name := c.Name()
	return CheckResult{
		Name:      name,
		Status:    StatusWarn,
		Message:   "placeholder: provider Validate() integration pending adapter module",
		CheckedAt: time.Now(),
	}
}
