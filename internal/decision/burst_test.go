package decision

import (
	"encoding/json"
	"math"
	"testing"

	"ownbot/internal/wire"
)

func burstScene(t *testing.T, wall bool) (*Controller, wire.Observation) {
	t.Helper()
	c, o := sample(t, wall)
	c.cfg.Mech["boost_speed"] = 20
	c.cfg.Mech["boost_drain"] = 17
	c.cfg.Mech["step_up"] = 1.2
	c.cfg.Mech["max_slope_deg"] = 50
	c.cfg.Mech["charge_move_mult"] = .5
	c.cfg.Cubes.SizePerCube = .05
	c.cfg.Weapons["laser"] = map[string]float64{"range": 80, "charge": .75, "min_charge": .15, "hold_s": 2, "cooldown": 1.65, "charge_turn_mult": 1}
	o.Mechs = []wire.Actor{{ID: 4, Kind: "mech", HP: 600, Pos: wire.Vec3{35, 0, 0}, Aim: wire.Vec2{math.Pi - math.Atan2(.4, 35), 0}, Weapons: []string{"gun", "laser"}, Charging: true}}
	o.Self.Aim = wire.Vec2{0, math.Atan2(-2.5, 35)}
	o.Self.Weapons[1] = &wire.Weapon{Kind: "lightning"}
	o.Offer = &wire.Offer{Choices: []wire.Choice{{ID: "plating"}}}
	return c, o
}

// With the same observation and controller history, the warning can change
// only the outgoing Boost bit. Compare encoded actions, including optional fields.
func checkBurst(t *testing.T, c *Controller, o wire.Observation, want bool) {
	t.Helper()
	baseline := *c
	quiet := o
	quiet.Mechs = append([]wire.Actor(nil), o.Mechs...)
	for i := range quiet.Mechs {
		quiet.Mechs[i].Charging = false
	}
	before := baseline.Decide(quiet)
	if before.Boost {
		t.Fatal("fixture already boosted in the absence of a warning")
	}
	before.Boost = want
	expected, _ := json.Marshal(before)
	actual, _ := json.Marshal(c.Decide(o))
	if string(actual) != string(expected) {
		t.Fatalf("actual %s, expected %s", actual, expected)
	}
}

func TestBurstOnlyChangesBoostWhenFiniteRayExposureDrops(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Controller, *wire.Observation)
		want bool
	}{
		{"lateral walk", func(c *Controller, o *wire.Observation) {}, true},
		{"charge absent", func(c *Controller, o *wire.Observation) { o.Mechs[0].Charging = false }, false},
		{"aim misses", func(c *Controller, o *wire.Observation) { o.Mechs[0].Aim[0] = .1 }, false},
		{"range ends before body", func(c *Controller, o *wire.Observation) { c.cfg.Weapons["laser"]["range"] = 25 }, false},
		{"beam above body", func(c *Controller, o *wire.Observation) { o.Mechs[0].Cubes = 8 }, false},
		{"equal exposure", func(c *Controller, o *wire.Observation) { o.Mechs[0].Aim[0] = math.Pi }, false},
		{"initial velocity away from command", func(c *Controller, o *wire.Observation) { o.Self.Vel[1] = -8 }, false},
		{"slower acceleration", func(c *Controller, o *wire.Observation) { o.Self.Stats["ground_accel"] = 25 }, false},
		{"airborne", func(c *Controller, o *wire.Observation) { o.Self.Grounded = false }, false},
		{"energy reserved for pulse", func(c *Controller, o *wire.Observation) { o.Self.Energy = 8 }, false},
		{"energy covers pulse and burst", func(c *Controller, o *wire.Observation) { o.Self.Energy = 8.1 }, true},
		{"ready laser starts charging", func(c *Controller, o *wire.Observation) { o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"} }, false},
		{"holds existing laser charge", func(c *Controller, o *wire.Observation) { o.Self.Weapons[1] = &wire.Weapon{Kind: "laser", Charge: .25} }, false},
		{"offline laser permits full speed", func(c *Controller, o *wire.Observation) { o.Self.Weapons[1] = &wire.Weapon{Kind: "laser", Cooldown: 1} }, true},
		{"active target is npc", func(c *Controller, o *wire.Observation) {
			o.Mechs = append(o.Mechs, wire.Actor{Kind: "npc", ID: 1, HP: 10, Pos: wire.Vec3{30, 0, 0}})
			c.cfg.NPC["height"], c.cfg.NPC["radius"] = 3.5, 1.8
		}, false},
		{"no warning from npc", func(c *Controller, o *wire.Observation) { o.Mechs[0].Kind = "npc" }, false},
		{"new second ray", func(c *Controller, o *wire.Observation) {
			o.Self.Vel[1] = 12
			o.Mechs[0].Aim[0] = math.Pi - math.Atan2(1.9, 35)
			o.Mechs = append(o.Mechs, wire.Actor{ID: 5, Kind: "mech", HP: 900, Pos: wire.Vec3{60, 8, 0}, Aim: wire.Vec2{math.Pi, 0}, Charging: true})
		}, false},
		{"original ray alone improves", func(c *Controller, o *wire.Observation) {
			o.Self.Vel[1] = 12
			o.Mechs[0].Aim[0] = math.Pi - math.Atan2(1.9, 35)
		}, true},
		{"boost projection exits zone", func(c *Controller, o *wire.Observation) {
			o.Self.Vel[1] = 12
			o.Mechs[0].Aim[0] = math.Pi - math.Atan2(1.9, 35)
			o.Zone.Center, o.Zone.NextCenter = wire.Vec2{0, -85.8}, wire.Vec2{0, -85.8}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, o := burstScene(t, false)
			tc.edit(c, &o)
			checkBurst(t, c, o, tc.want)
		})
	}
}

func TestBurstRejectsObstructedShooterWhileOtherTargetIsClear(t *testing.T) {
	for _, wall := range []bool{false, true} {
		c, o := burstScene(t, wall)
		o.Self.ID = 1
		o.Self.Aim[0] = math.Pi
		o.Mechs = append(o.Mechs, wire.Actor{ID: 1, Kind: "mech", HP: 1, Pos: wire.Vec3{-35, 0, 0}})
		checkBurst(t, c, o, !wall)
	}
}

func TestBurstLeavesRecoveryAndUrgentZoneCommandsUntouched(t *testing.T) {
	c, o := burstScene(t, false)
	o.Self.HP = 380
	o.Mechs = append(o.Mechs, wire.Actor{ID: 7, Kind: "mech", HP: 900, Pos: wire.Vec3{0, -15, 0}})
	quiet := *c
	got := quiet.Decide(o)
	if got.Move[1] < .99 || quiet.target.kind == "enemy" {
		t.Fatalf("not a lateral recovery escape: %+v", got)
	}
	checkBurst(t, c, o, false)

	c, o = burstScene(t, false)
	o.Zone = wire.Zone{Center: wire.Vec2{0, 9}, NextCenter: wire.Vec2{0, 9}, Radius: 10, NextRadius: 5, ShrinkStart: o.T, ShrinkEnd: o.T + .8}
	quiet = *c
	got = quiet.Decide(o)
	if quiet.target.kind != "zone" || got.Move[1] <= 0 {
		t.Fatalf("not an urgent zone command: %+v, goal %+v", got, quiet.target)
	}
	checkBurst(t, c, o, false)
}

func TestBurstRequiresCenterSupportAtRoofEdge(t *testing.T) {
	for _, tc := range []struct {
		startY float64
		want   bool
	}{{0, true}, {19, false}, {20.2, false}} {
		c, o := burstScene(t, true)
		o.Self.ID = 1
		o.Self.Pos, o.Self.Aim[0] = wire.Vec3{7.5, tc.startY, 20}, math.Pi
		o.Mechs[0].Pos, o.Mechs[0].Aim[0] = wire.Vec3{-27.5, tc.startY + .4, 20}, 0
		checkBurst(t, c, o, tc.want)
	}
}

func TestBurstRespectsOwnQueuedMinCharge(t *testing.T) {
	for _, tc := range []struct {
		name                string
		charge, angleOffset float64
		want                bool
	}{
		{"early release still moving slowly", .05, .05, true},
		{"fully charged release restores speed", .75, .4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, o := burstScene(t, false)
			c.laser = laserState{present: true, slot: 1, pending: true, lastTick: o.Tick - 2}
			o.Self.Weapons[1] = &wire.Weapon{Kind: "laser", Charge: tc.charge}
			o.Mechs[0].Aim[0] = math.Pi - math.Atan2(tc.angleOffset, 35)
			checkBurst(t, c, o, tc.want)
		})
	}
}

func TestBurstLeavesResourceMoveAndTakeUntouched(t *testing.T) {
	c, o := burstScene(t, false)
	o.Self.Weapons[1] = nil
	o.Self.Weapons[0].Reload = 1
	o.Pickups = []wire.Pickup{{ID: 31, Kind: "lightning", Pos: wire.Vec3{0, 3, 0}}}
	cCheck := *c
	a := cCheck.Decide(o)
	if cCheck.target.kind != "weapon" || a.Take == nil || a.Move[1] < .99 {
		t.Fatalf("expected moving resource action with take, got %+v", a)
	}
	checkBurst(t, c, o, false)
}
