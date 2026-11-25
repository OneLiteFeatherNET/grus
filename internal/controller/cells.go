package controller

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

// cellsOf returns the cell names of the grus.olf/cells label of an instance.
func cellsOf(inst model.Instance) []string {
	v := inst.Labels[model.LabelCells]
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

// assignCells gives every cell of every sharded pool to exactly one Ready instance of the pool and stores the
// result as the label grus.olf/cells. The assignment is stable: a cell stays with its instance while that
// instance is Ready. Cells of instances that are not Ready (failed, lost, stopping) and cells without owner
// go to the Ready instance with the fewest cells. pool.cells is published when the assignment changed.
//
// Spec only: splitting a hot cell and merging idle neighbours with the load; see model.Sharding.
// Callers hold c.mu.
func (c *Controller) assignCells(ctx context.Context, insts []model.Instance, specs map[string]model.PoolSpec, unlisted map[string]bool) []error {
	var errs []error
	names := make([]string, 0, len(specs))
	for name, spec := range specs {
		if spec.Sharding != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := c.assignPoolCells(ctx, name, specs[name].Sharding, insts, unlisted); err != nil {
			errs = append(errs, fmt.Errorf("assign cells of pool %s: %w", name, err))
		}
	}
	return errs
}

func (c *Controller) assignPoolCells(ctx context.Context, pool string, sh *model.Sharding, all []model.Instance, unlisted map[string]bool) error {
	var insts []model.Instance
	for _, inst := range all {
		if inst.Pool == pool && !inst.State.Terminal() {
			insts = append(insts, inst)
		}
	}
	slices.SortFunc(insts, func(a, b model.Instance) int { return strings.Compare(a.ID, b.ID) })

	eligible := func(inst model.Instance) bool { return inst.State == model.StateReady }
	declared := make(map[string]bool, len(sh.Cells))
	for _, cell := range sh.Cells {
		declared[cell.Name] = true
	}

	before := make(map[string]string)   // cell -> instance as stored
	assigned := make(map[string]string) // cell -> instance after this cycle
	count := make(map[string]int)       // instance -> number of cells
	for _, inst := range insts {
		for _, cell := range cellsOf(inst) {
			before[cell] = inst.ID
			if !declared[cell] || !eligible(inst) || assigned[cell] != "" {
				continue // removed from the spec, instance not Ready, or already held by another instance
			}
			assigned[cell] = inst.ID
			count[inst.ID]++
		}
	}
	var ready []model.Instance
	for _, inst := range insts {
		if eligible(inst) && !unlisted[hostOf(inst)] {
			ready = append(ready, inst)
		}
	}
	if len(ready) > 0 {
		for _, cell := range sh.Cells {
			if assigned[cell.Name] != "" {
				continue
			}
			best := ready[0]
			for _, inst := range ready[1:] {
				if count[inst.ID] < count[best.ID] {
					best = inst
				}
			}
			assigned[cell.Name] = best.ID
			count[best.ID]++
		}
	}

	// Rebalance: an instance that became Ready later gets cells from one that holds several. Only an empty
	// instance triggers a move, so assignments stay stable otherwise.
	for len(ready) > 1 {
		most, least := ready[0], ready[0]
		for _, inst := range ready[1:] {
			if count[inst.ID] > count[most.ID] {
				most = inst
			}
			if count[inst.ID] < count[least.ID] {
				least = inst
			}
		}
		if count[least.ID] != 0 || count[most.ID] < 2 {
			break
		}
		moved := ""
		for cell, id := range assigned {
			if id == most.ID && cell > moved {
				moved = cell
			}
		}
		assigned[moved] = least.ID
		count[most.ID]--
		count[least.ID]++
	}

	changed := false
	for _, inst := range insts {
		var mine []string
		for cell, id := range assigned {
			if id == inst.ID {
				mine = append(mine, cell)
			}
		}
		sort.Strings(mine)
		want := strings.Join(mine, ",")
		if inst.Labels[model.LabelCells] == want {
			continue
		}
		labels := make(map[string]string, len(inst.Labels)+1)
		for k, v := range inst.Labels {
			labels[k] = v
		}
		if want == "" {
			delete(labels, model.LabelCells)
		} else {
			labels[model.LabelCells] = want
		}
		inst.Labels = labels
		if err := c.st.UpdateInstance(ctx, inst); err != nil {
			return fmt.Errorf("persist instance %s: %w", inst.ID, err)
		}
		changed = true
	}
	if !changed && sameAssignment(before, assigned) {
		return nil
	}
	data := model.PoolCellsData{Pool: pool, Cells: make([]model.CellOwner, 0, len(sh.Cells))}
	for _, cell := range sh.Cells {
		data.Cells = append(data.Cells, model.CellOwner{Cell: cell.Name, Instance: assigned[cell.Name]})
	}
	c.pub.Publish(model.EventPoolCells, data)
	return nil
}

func sameAssignment(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// PoolCells lists the cells of a sharded pool with the instance that serves each.
func (c *Controller) PoolCells(ctx context.Context, pool string) ([]model.CellInfo, error) {
	p, err := c.st.GetPool(ctx, pool)
	if err != nil {
		return nil, mapErr(err)
	}
	if p.Spec.Sharding == nil {
		return nil, fmt.Errorf("%w: pool %s has no sharding", ErrNotFound, pool)
	}
	insts, err := c.st.ListInstances(ctx, store.InstanceFilter{Pool: pool})
	if err != nil {
		return nil, mapErr(err)
	}
	owner := make(map[string]model.Instance)
	for _, inst := range insts {
		if inst.State != model.StateReady {
			continue
		}
		for _, cell := range cellsOf(inst) {
			owner[cell] = inst
		}
	}
	out := make([]model.CellInfo, 0, len(p.Spec.Sharding.Cells))
	for _, cell := range p.Spec.Sharding.Cells {
		info := model.CellInfo{Cell: cell.Name, Bounds: model.CellBounds{MinX: cell.MinX, MinZ: cell.MinZ, MaxX: cell.MaxX, MaxZ: cell.MaxZ}}
		if inst, ok := owner[cell.Name]; ok {
			info.Instance, info.Address = inst.ID, inst.Address
		}
		out = append(out, info)
	}
	return out, nil
}

// CellAt returns the cell that contains the block (x, z) and its instance. ErrNotFound when no cell covers the
// block, ErrInvalidState when the cell has no Ready instance yet.
func (c *Controller) CellAt(ctx context.Context, pool string, x, z int) (model.CellInfo, error) {
	cells, err := c.PoolCells(ctx, pool)
	if err != nil {
		return model.CellInfo{}, err
	}
	cx, cz := model.ChunkOf(x), model.ChunkOf(z)
	for _, info := range cells {
		b := info.Bounds
		if cx < b.MinX || cx > b.MaxX || cz < b.MinZ || cz > b.MaxZ {
			continue
		}
		if info.Instance == "" {
			return model.CellInfo{}, fmt.Errorf("%w: cell %s has no ready instance", ErrInvalidState, info.Cell)
		}
		return info, nil
	}
	return model.CellInfo{}, fmt.Errorf("%w: no cell covers block %d,%d", ErrNotFound, x, z)
}
