// Panel catalog install: every recipe appended its PanelProvider to
// the boot's panel list during the recipe step; the root asserts
// them once and installs the registry a single time.
package server

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/calionauta/gogogo/config"
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// installPanels asserts the collected panel providers already accumulated
// in panels (as any) into the panel interface and installs the catalog
// plus the action registry once. Fail-fast: an entry that does not
// implement the interface, or a catalog the genui package rejects, is
// a boot failure with the offender named.
func installPanels(panels []any) error {
	providers := make([]comp.PanelProvider, 0, len(panels))
	for _, p := range panels {
		pp, ok := p.(comp.PanelProvider)
		if !ok {
			return fmt.Errorf("panel provider %T does not implement PanelProvider", p)
		}
		providers = append(providers, pp)
	}
	var actionProviders []comp.ActionProvider
	for _, pp := range providers {
		if ap, ok := pp.(comp.ActionProvider); ok {
			actionProviders = append(actionProviders, ap)
		}
	}
	if err := comp.InstallPanelRegistry(providers); err != nil {
		return err
	}
	return comp.InstallActionRegistry(actionProviders)
}

// healSearch runs the delayed search heal now that boot has its DB.
func healSearch(bootCtx context.Context, todoRef *register.TodoRef) error {
	if todoRef != nil && todoRef.HealSearch != nil {
		return todoRef.HealSearch(bootCtx)
	}
	return nil
}

// newChiQueue boots the chi-owned work queue (workers run here so they
// never compete with the other stack's workers).
func newChiQueue(cfg *config.Config) (*queue.Queue, *queue.SSEHub, error) {
	chiQ, err := queue.NewAt(filepath.Join(cfg.DataDir, "chi"))
	if err != nil {
		return nil, nil, err
	}
	return chiQ, chiQ.Hub(), nil
}
