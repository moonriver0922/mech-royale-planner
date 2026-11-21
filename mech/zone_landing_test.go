package mech

import "testing"

func TestZoneLanding(t *testing.T) {
	w := FlatWorld()
	b := Brain{W: w}
	cell := w.Cell(0, 0)
	gx, gy := w.CellPos(cell)
	actor := Self{Pos: Vec3{gx + 20, gy, 0}, Grounded: true}
	zone := Zone{NextCenter: [2]float64{gx, gy}, NextRadius: 20}
	check := func(name string, change bool) {
		t.Helper()
		x, y := b.zoneLanding(&actor, &zone, gx, gy)
		if ((x != gx || y != gy) != change) ||
			(change && (x <= gx || x > actor.Pos[0] || y != gy || !w.Free(x, y) ||
				hyp(x-zone.NextCenter[0], y-zone.NextCenter[1]) > zone.NextRadius-zone.NextRadius/4)) {
			t.Errorf("%s: landing (%g, %g), expected near-side substitution %v", name, x, y, change)
		}
	}
	check("clear endpoint", false)
	for offset := 0; offset < 6; offset += 2 {
		c := w.Cell(gx+float64(offset), gy)
		w.Blocked[c], w.Comp[c], w.Clearance[c] = true, 0, 0
	}
	check("blocked endpoint", true)
	actor.Grounded = false
	check("in flight", false)
	actor.Grounded, actor.Pos[2] = true, 7
	check("on roof", false)
	actor.Pos[2] = 0
	zone.NextRadius = 4
	check("near side has too little zone margin", false)
	zone.NextRadius = 20
	w.Comp[w.Cell(actor.Pos[0], actor.Pos[1])] = w.MainComp + 1
	check("disconnected start", false)
}
