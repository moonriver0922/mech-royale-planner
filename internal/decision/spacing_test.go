package decision

import (
	"math"
	"testing"

	"ownbot/internal/wire"
)

func TestPlayerSpacingDirections(t *testing.T) {
	c, o := sample(t, false)
	c.cfg.Vision = wire.Numbers{"range": 200}
	o.Self.Weapons[0].Stats = map[string]float64{"range": 80}
	desired := playerRangeFraction * 80
	for _, test := range []struct {
		name               string
		rangeError, radial float64
	}{
		{"close", -20, -math.Sqrt(3) / 2},
		{"orbit", 0, 0},
		{"far", 20, math.Sqrt(3) / 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, phase := range []float64{2, 3} {
				o.T = phase
				s := shot{id: 15, pos: wire.Vec3{desired + test.rangeError, 0, 0}, distance: 999}
				m := c.engage(o, s)
				if math.Abs(m[0]-test.radial) > 1e-9 || math.Abs(math.Hypot(m[0], m[1])-1) > 1e-9 || (m[1] > 0) != (phase < 3) {
					t.Fatalf("phase %g move %v, expected radial %g and alternating tangent", phase, m, test.radial)
				}
			}
		})
	}
}

func TestPlayerSpacingVisionCap(t *testing.T) {
	c, o := sample(t, false)
	c.cfg.Vision = wire.Numbers{"range": 30}
	o.Self.Stats["radius"], o.Self.Stats["walk_speed"] = 4, 16
	want := 30 - 4 - 16*c.period
	m := c.engage(o, shot{id: 1, pos: wire.Vec3{want, 0, 0}})
	if math.Abs(m[0]) > 1e-9 || m[1] < .99 {
		t.Fatalf("expected orbit at visibility cap %g, got %v", want, m)
	}
}

func TestNPCSpacingPreservesOriginalAngles(t *testing.T) {
	c, o := sample(t, false)
	c.cfg.Vision = wire.Numbers{"range": 200}
	for _, tc := range []struct{ dist, angle float64 }{{10, 1.35}, {45, 1}, {91, .55}} {
		m := c.engage(o, shot{id: 1000, pos: wire.Vec3{tc.dist, 0, 0}, distance: tc.dist})
		if math.Abs(math.Atan2(m[1], m[0])-math.Pi/2*tc.angle) > 1e-9 {
			t.Errorf("distance %g: move %v", tc.dist, m)
		}
	}
}

func TestPlayerSpacingRetainsTargetAimAndFire(t *testing.T) {
	c, o := sample(t, false)
	c.cfg.Vision = wire.Numbers{"range": 200}
	o.Mechs = []wire.Actor{{ID: 2, Kind: "mech", Pos: wire.Vec3{25, 0, 0}, Vel: wire.Vec3{0, 8, 0}}}
	s, ok := c.bestEnemy(o)
	if !ok {
		t.Fatal("fixture has no target")
	}
	o.Self.Aim = s.aim
	a := c.Decide(o)
	if c.target != (goal{"enemy", 2}) || a.Aim != s.aim || !a.Fire[0] || a.Move[0] >= 0 {
		t.Fatalf("expected lead aim and gunfire while withdrawing, got target %v action %+v", c.target, a)
	}
}

func TestPlayerSpacingChecksWallAndShrinkingZone(t *testing.T) {
	for _, wall := range []bool{true, false} {
		c, o := sample(t, wall)
		c.cfg.Vision = wire.Numbers{"range": 200}
		if !wall {
			o.Self.Pos[0] = 10
			o.Zone = wire.Zone{Radius: 12, NextRadius: 8, ShrinkStart: o.T, ShrinkEnd: o.T + 1}
		}
		s := shot{id: 1, pos: wire.Vec3{-10, 0, 0}}
		m := c.engage(o, s)
		l := 3 * c.stat(o.Self, "walk_speed") * c.period
		q := c.onGround(o.Self.Pos[0]+l*m[0], o.Self.Pos[1]+l*m[1])
		if m == (wire.Vec2{}) || math.Abs(math.Hypot(m[0], m[1])-1) > 1e-9 || !c.walk(o.Self, o.Self.Pos, q) || margin(o.Zone, q, o.T, o.T+3*c.period) < 0 {
			t.Fatalf("wall=%v, unsafe or empty move %v to %v", wall, m, q)
		}
	}
}
