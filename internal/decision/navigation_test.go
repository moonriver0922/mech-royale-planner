package decision

import (
	"math"
	"slices"
	"testing"

	"ownbot/internal/wire"
)

func TestFollowRefreshesReachedTerminal(t *testing.T) {
	c, o := sample(t, false)
	// Seed 45027, tick 998, translated to the origin on flat, open ground.
	old := wire.Vec3{.03780, .18329, 0}
	dest := wire.Vec3{.42652, 2.93904, 0}
	g := goal{kind: "cover"}
	c.target, c.destination, c.route, c.replanAt = g, old, []wire.Vec3{old}, o.T+1
	for i := range 2 {
		m := c.follow(o, g, dest)
		if math.Abs(m[0]-.14362) > .0001 || math.Abs(m[1]-.98963) > .0001 {
			t.Fatalf("observation %d: stale terminal suppressed or redirected movement: %v", i, m)
		}
		if c.destination != dest || len(c.route) != 1 || c.route[0] != dest {
			t.Fatalf("requested destination %v, retained destination %v, route %v", dest, c.destination, c.route)
		}
		o.Self.Pos[0] += .4 * m[0]
		o.Self.Pos[1] += .4 * m[1]
		o.T += c.period
	}
}

func TestFollowStopsAtActualGoal(t *testing.T) {
	c, o := sample(t, false)
	g := goal{kind: "cover"}
	c.target, c.destination, c.route = g, o.Self.Pos, []wire.Vec3{o.Self.Pos}
	if m := c.follow(o, g, o.Self.Pos); m != (wire.Vec2{}) {
		t.Fatalf("goal reached, move %v", m)
	}
}

func TestFollowDetoursFromReachedTerminal(t *testing.T) {
	c, o := sample(t, true)
	o.Self.Pos = wire.Vec3{2.4, 19, 0}
	dest := wire.Vec3{6.5, 22.7, 0}
	if c.walk(o.Self, o.Self.Pos, dest) {
		t.Fatal("fixture must clip the inflated corner")
	}
	g := goal{kind: "cover"}
	c.target, c.destination, c.route = g, o.Self.Pos, []wire.Vec3{o.Self.Pos}
	m := c.follow(o, g, dest)
	if m == (wire.Vec2{}) || len(c.route) < 2 || c.route[len(c.route)-1] != dest || c.destination != dest {
		t.Fatalf("expected a detour to %v, retained %v, move %v", dest, c.route, m)
	}
	first := c.route[0]
	if !c.walk(o.Self, o.Self.Pos, first) || math.Abs(math.Atan2(m[1], m[0])-math.Atan2(first[1]-o.Self.Pos[1], first[0]-o.Self.Pos[0])) > 1e-6 {
		t.Fatalf("move %v does not follow a valid first leg to %v", m, first)
	}
}

func TestFollowThrottlesUnreachableTerminal(t *testing.T) {
	c, o := sample(t, true)
	g := goal{kind: "cover"}
	dest := wire.Vec3{6, 0, 0} // Inside the wall.
	c.target, c.destination, c.route = g, o.Self.Pos, []wire.Vec3{o.Self.Pos}
	c.replanAt = o.T + .3
	for i := range 11 {
		o.T = 2 + float64(i)*.1
		before := c.replanAt
		m := c.follow(o, g, dest)
		if m != (wire.Vec2{}) {
			t.Fatalf("unreachable goal at %.1f, move %v", o.T, m)
		}
		if o.T < before && c.replanAt != before || o.T >= before && math.Abs(c.replanAt-(o.T+.6)) > 1e-9 {
			t.Fatalf("unexpected retry schedule at %.1f: before %v, after %v", o.T, before, c.replanAt)
		}
	}
}

func TestFollowPreservesActiveDetour(t *testing.T) {
	c, o := sample(t, true)
	o.Self.Pos = wire.Vec3{2.4, 19, 0}
	g := goal{kind: "cover"}
	old, dest := wire.Vec3{6, 23, 0}, wire.Vec3{6.5, 22.7, 0}
	path := []wire.Vec3{{2.4, 29, 0}, old}
	c.target, c.destination, c.route = g, old, slices.Clone(path)
	m := c.follow(o, g, dest)
	if math.Abs(m[0]) > 1e-9 || m[1] != 1 || c.destination != old || !slices.Equal(c.route, path) {
		t.Fatalf("lost intermediate waypoint: move %v, route %v, destination %v", m, c.route, c.destination)
	}
}
