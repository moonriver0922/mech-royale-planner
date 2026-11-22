package decision

import (
	"math"
	"reflect"
	"testing"

	"ownbot/internal/wire"
)

func boostScene(t *testing.T) (*Controller, wire.Observation) {
	t.Helper()
	c, o := growthScene(t, false)
	c.cfg.Mech["boost_drain"], c.cfg.Mech["boost_speed"] = 30, 30
	c.cfg.Weapons["laser"] = map[string]float64{"range": 80}
	o.Self.Vel[1] = 12
	o.Mechs[0].Charging = true
	o.Mechs[0].Aim = wire.Vec2{math.Pi, 0}
	o.Offer = &wire.Offer{Choices: []wire.Choice{{ID: "plating"}}}
	return c, o
}

// On the fourth physics tick walking still intersects the ray at y=2.40;
// accelerating from the observed 12 m/s puts Boost at y=3.90, outside it.
func TestDefensiveBoostChangesOnlyBoostDuringPlayerEngagement(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Controller, *wire.Observation)
	}{
		{"ready gun", func(c *Controller, o *wire.Observation) {}},
		{"later engagement while turning", func(c *Controller, o *wire.Observation) { o.Self.Aim[0] = 2 }},
		{"uncollectible cube does not disable escape", func(c *Controller, o *wire.Observation) { o.Cubes = []wire.Cube{{ID: 45, Pos: wire.Vec3{80, 0, 0}}} }},
		{"exact energy budget", func(c *Controller, o *wire.Observation) { o.Self.Energy = 9 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, o := boostScene(t)
			tc.change(c, &o)
			parent, quiet := *c, o
			quiet.Mechs = append([]wire.Actor(nil), o.Mechs...)
			quiet.Mechs[0].Charging = false
			want := parent.Decide(quiet)
			want.Boost = true
			got := c.Decide(o)
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(c, &parent) {
				t.Fatalf("got %+v, want %+v with original controller state", got, want)
			}
		})
	}
}

func TestDefensiveBoostRetainsGrowthActions(t *testing.T) {
	for _, tc := range []struct {
		name         string
		pos          wire.Vec3
		weapon, take bool
	}{
		{"cube already in reach keeps original move", wire.Vec3{0, 1, 0}, false, false},
		{"cube along current heading", wire.Vec3{0, 10, 0}, false, false},
		{"weapon along current heading", wire.Vec3{0, 11, 0}, true, false},
		{"current Take", wire.Vec3{0, 1, 0}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, o := boostScene(t)
			if tc.weapon {
				o.Pickups = []wire.Pickup{{ID: 25, Kind: "laser", Pos: tc.pos}}
			} else {
				o.Cubes = []wire.Cube{{ID: 25, Pos: tc.pos}}
			}
			got := c.Decide(o)
			if got.Boost || math.Abs(got.Move[1]-1) > 1e-12 || (got.Take != nil) != tc.take || tc.take && *got.Take != 25 {
				t.Fatalf("lost selected growth action: %+v", got)
			}
		})
	}
}

func TestDefensiveBoostRequiresClearerWarningAndNormalPlayerEngagement(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Controller, *wire.Observation)
	}{
		{"not charging", func(c *Controller, o *wire.Observation) { o.Mechs[0].Charging = false }},
		{"ray faces away", func(c *Controller, o *wire.Observation) { o.Mechs[0].Aim[0] = 0 }},
		{"finite ray too short", func(c *Controller, o *wire.Observation) { c.cfg.Weapons["laser"]["range"] = 30 }},
		{"low energy", func(c *Controller, o *wire.Observation) { o.Self.Energy = 8.99 }},
		{"airborne", func(c *Controller, o *wire.Observation) { o.Self.Grounded = false }},
		{"elevated support", func(c *Controller, o *wire.Observation) { o.Self.Pos[2] = 1 }},
		{"secondary equipped", func(c *Controller, o *wire.Observation) { o.Self.Weapons[1] = &wire.Weapon{Kind: "lightning"} }},
		{"different primary", func(c *Controller, o *wire.Observation) {
			o.Self.Weapons[0].Kind = "lightning"
			c.cfg.Weapons["lightning"]["range"] = 65
		}},
		{"NPC target in another player's warning", func(c *Controller, o *wire.Observation) {
			o.Mechs = append(o.Mechs, wire.Actor{ID: 1, Kind: "npc", Pos: o.Mechs[0].Pos})
		}},
		{"newly entered ray vetoes escape", func(c *Controller, o *wire.Observation) {
			o.Mechs = append(o.Mechs, wire.Actor{ID: 19, Kind: "mech", Pos: wire.Vec3{50, 8.8, 0}, Charging: true, Aim: wire.Vec2{math.Pi, 0}})
		}},
		{"zone edge reached during Boost", func(c *Controller, o *wire.Observation) {
			o.Zone = wire.Zone{Radius: 5}
		}},
		{"terrain boundary reached during Boost", func(c *Controller, o *wire.Observation) {
			o.Self.Pos[1], o.Mechs[0].Pos[1] = 92, 92
			o.Zone.Center, o.Zone.NextCenter = wire.Vec2{0, 92}, wire.Vec2{0, 92}
		}},
		{"uncovered recovery", func(c *Controller, o *wire.Observation) {
			o.Self.HP = 400
			o.Mechs[0].Pos = wire.Vec3{0, -50, 0}
			o.Mechs[0].Charging = false
			o.Mechs = append(o.Mechs, wire.Actor{ID: 15, Kind: "mech", Pos: wire.Vec3{60, 0, 0}, Charging: true, Aim: wire.Vec2{math.Pi, 0}})
		}},
		{"in-zone urgent route", func(c *Controller, o *wire.Observation) {
			o.Zone = wire.Zone{Radius: 60, NextCenter: wire.Vec2{0, 40}, NextRadius: 20, ShrinkStart: o.T, ShrinkEnd: o.T + .5}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, o := boostScene(t)
			tc.change(c, &o)
			got := c.Decide(o)
			if got.Boost {
				t.Fatalf("unexpected Boost: %+v", got)
			}
			if tc.name == "NPC target in another player's warning" && c.target != (goal{"enemy", 1}) {
				t.Fatalf("fixture selected %+v", c.target)
			}
			if tc.name == "in-zone urgent route" && c.target.kind != "zone" {
				t.Fatalf("fixture selected %+v", c.target)
			}
			if tc.name == "uncovered recovery" && (c.target.kind == "enemy" || got.Move[1] < .99) {
				t.Fatalf("fixture did not escape: %+v, %+v", c.target, got)
			}
		})
	}
}

func TestDefensiveBoostProtectsScheduledGunDischarges(t *testing.T) {
	for _, tc := range []struct {
		name               string
		cooldown, interval float64
		wantBoost          bool
	}{
		{"second tick still clear", .08, .24, true},
		{"third tick leaves gun tolerance", .13, .24, false},
		{"second discharge leaves gun tolerance", 0, .13, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, o := boostScene(t)
			c.cfg.Match["decide_every"], c.period = 3, .15
			o.Self.Weapons[0].Cooldown = tc.cooldown
			c.cfg.Weapons["gun"]["cooldown"] = tc.interval
			got := c.Decide(o)
			// Third-tick gun tolerance is 2.25 m. The held walk travels
			// 1.80 m, while the held Boost crosses that width at 2.70 m.
			if !got.Fire[0] || got.Boost != tc.wantBoost {
				t.Fatalf("wrong movement for held shot: %+v", got)
			}
		})
	}
}
