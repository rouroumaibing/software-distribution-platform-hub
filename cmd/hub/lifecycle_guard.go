package main

import (
	"github.com/google/uuid"

	componentsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/service"
)

// componentLifecycleAdapter bridges ComponentService to the run service's
// ComponentGuard narrow interface (§6.1 治理守卫: deprecated 禁新触发).
type componentLifecycleAdapter struct {
	svc *componentsvc.ComponentService
}

func (a componentLifecycleAdapter) LifecycleOf(componentID uuid.UUID) (string, error) {
	c, err := a.svc.Get(componentID)
	if err != nil {
		return "", err
	}
	if c.Lifecycle == "" {
		return "active", nil
	}
	return c.Lifecycle, nil
}
