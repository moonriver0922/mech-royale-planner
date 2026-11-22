package decision

import (
	"math"
	"testing"

	"ownbot/internal/wire"
)

func TestUncoveredRetreatCompletesHeldLaser(t *testing.T) {
	c, o := laserScene(t, false)
	o.Self.HP = 400
	o.Self.Weapons[0].Ammo, o.Self.Weapons[0].Reload = 0, 1.1
	for _, frame := range []struct {
		charge float64
		held   bool
	}{
		{.3, true}, {.4, true}, {.5, true}, {.6, true}, {.7, true},
		{.75, false}, {.75, false}, {0, false},
	} {
		o.Self.Weapons[1].Charge = frame.charge
		a := c.Decide(o)
		if a.Move[0] != -1 || math.Abs(a.Move[1]) > 1e-12 ||
			a.Aim != o.Self.Aim || a.Fire != [2]bool{false, frame.held} {
			t.Fatalf("charge %.2f: want westward escape, current aim, laser held=%t; got %+v", frame.charge, frame.held, a)
		}
		advance(&o, 2)
	}
}

func TestUncoveredRetreatGunFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*Controller, *wire.Observation)
	}{
		{"empty charge", func(_ *Controller, o *wire.Observation) { o.Self.Weapons[1].Charge = 0 }},
		{"gun only", func(_ *Controller, o *wire.Observation) { o.Self.Weapons[1] = nil }},
		{"cooldown", func(_ *Controller, o *wire.Observation) { o.Self.Weapons[1].Cooldown = .4 }},
		{"short", func(_ *Controller, o *wire.Observation) {
			o.Events = []wire.Event{{Type: "effect", Effect: "short"}}
		}},
		{"missed decision", func(c *Controller, o *wire.Observation) {
			o.Self.HP = o.Self.MaxHP
			c.Decide(*o)
			advance(o, 4)
			o.Self.HP = 400
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, o := laserScene(t, false)
			o.Self.HP = 400
			o.Self.Weapons[1].Charge = .3
			tc.set(c, &o)
			a := c.Decide(o)
			if a.Fire != [2]bool{true, false} || a.Aim != o.Self.Aim ||
				a.Move[0] != -1 || math.Abs(a.Move[1]) > 1e-12 {
				t.Fatalf("want aligned gun fire while moving west; got %+v", a)
			}
		})
	}
}

func TestRetreatToCoverReleasesHeldLaser(t *testing.T) {
	c, o := laserScene(t, true)
	o.Self.HP = 400
	o.Self.Pos[1], o.Mechs[0].Pos[1] = 22, 22
	o.Self.Weapons[1].Charge = .3
	a := c.Decide(o)
	if a.Move == (wire.Vec2{}) || c.target.kind != "cover" || a.Fire != [2]bool{} {
		t.Fatalf("want movement toward cover with both triggers released; got %+v goal=%+v", a, c.target)
	}
}
