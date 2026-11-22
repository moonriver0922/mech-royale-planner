package decision

import (
	"math"
	"testing"

	"ownbot/internal/wire"
)

func pressureScene(t *testing.T, wall bool) (*Controller, wire.Observation) {
	t.Helper()
	c, o := sample(t, wall)
	o.Self.ID = 8
	c.cfg.Weapons["gun"]["damage"] = 45
	c.cfg.Weapons["gun"]["reload"] = 2.4
	c.cfg.Weapons["lightning"]["damage"] = 60
	c.cfg.Weapons["lightning"]["cooldown"] = .25
	c.cfg.Cubes.DamagePerCube = .04
	o.Mechs = []wire.Actor{
		{ID: 4, Kind: "mech", Pos: wire.Vec3{40, 0, 0}, HP: 280},
		{ID: 0, Kind: "mech", Pos: wire.Vec3{35, 20, 0}, HP: 650},
	}
	o.Self.Aim = wire.Vec2{0, math.Atan2(-2.5, 40)}
	return c, o
}

func incoming(id int, amount float64) wire.Event {
	return wire.Event{Type: "damaged", Weapon: "gun", By: &id, Amount: amount}
}

func focus(t *testing.T, c *Controller, o wire.Observation) int {
	t.Helper()
	c.update(o)
	c.observeLaser(o)
	s, ok := c.bestEnemy(o)
	if !ok {
		t.Fatal("missing visible target")
	}
	return s.id
}

func TestPressureChoiceAndFinishingShot(t *testing.T) {
	for _, tt := range []struct {
		name              string
		hp                float64
		ammo, cubes, want int
		wantFire          bool
	}{
		{"attacker zero", 280, 12, 0, 0, false},
		{"one shot finish", 45, 12, 0, 4, true},
		{"cube-scaled finish", 46, 12, 1, 4, true},
		{"empty gun cannot finish", 40, 0, 0, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, o := pressureScene(t, false)
			o.Mechs[0].HP = tt.hp
			o.Self.Cubes, o.Self.Weapons[0].Ammo = tt.cubes, tt.ammo
			o.Events = []wire.Event{incoming(0, 100)}
			a := c.Decide(o)
			if c.target != (goal{"enemy", tt.want}) || a.Fire[0] != tt.wantFire {
				t.Fatalf("target=%+v gun=%v, want %d/%v", c.target, a.Fire[0], tt.want, tt.wantFire)
			}
		})
	}
}

func TestPressureExpiresAndConfirmedDeathClears(t *testing.T) {
	for _, scenario := range []string{"expiry", "kill", "kill then late projectile"} {
		t.Run(scenario, func(t *testing.T) {
			c, o := pressureScene(t, false)
			o.Self.Stats["regen_delay"] = 2
			o.Events = []wire.Event{incoming(0, 100)}
			if id := focus(t, c, o); id != 0 {
				t.Fatalf("damage initially chose %d, want 0", id)
			}
			advance(&o, 38)
			o.Events = nil
			if id := focus(t, c, o); id != 0 {
				t.Fatalf("unexpired damage chose %d, want 0", id)
			}
			if scenario == "expiry" {
				advance(&o, 2)
			} else {
				o.Events = []wire.Event{{Type: "kill", Target: 0}}
			}
			if id := focus(t, c, o); id != 4 {
				t.Fatalf("expired/eliminated pressure chose %d, want 4", id)
			}
			if scenario == "kill then late projectile" {
				advance(&o, 2)
				o.Events = []wire.Event{incoming(0, 100)}
				if id := focus(t, c, o); id != 4 {
					t.Fatalf("posthumous damage restored focus on %d", id)
				}
			}
		})
	}
}

func TestPressureAbsentBlockedAndInvalidEventsKeepBaseline(t *testing.T) {
	for _, name := range []string{"no pressure", "anonymous", "zone", "outside roster", "negative id", "own splash", "npc", "missing", "wall", "out of range", "prior target"} {
		t.Run(name, func(t *testing.T) {
			c, o := pressureScene(t, name == "wall")
			want := 4
			switch name {
			case "anonymous":
				o.Events = []wire.Event{{Type: "damaged", Weapon: "gun", Amount: 100}}
			case "zone":
				e := incoming(0, 600)
				e.Weapon = "zone"
				o.Events = []wire.Event{e}
			case "outside roster":
				o.Events = []wire.Event{incoming(30, 100)}
			case "negative id":
				o.Events = []wire.Event{incoming(-1, 100)}
			case "own splash":
				o.Events = []wire.Event{incoming(o.Self.ID, 100)}
			case "npc":
				o.Events = []wire.Event{incoming(101, 100)}
			case "missing":
				o.Events = []wire.Event{incoming(2, 100)}
			case "wall":
				o.Mechs[0].Pos = wire.Vec3{0, 35, 0}
				o.Events = []wire.Event{incoming(0, 100)}
			case "out of range":
				o.Self.Weapons[0].Stats = map[string]float64{"range": 35}
				o.Mechs[0].Pos = wire.Vec3{30, 0, 0}
				o.Events = []wire.Event{incoming(0, 100)}
			case "prior target":
				c.target = goal{"enemy", 0}
				o.Mechs[1].Pos = wire.Vec3{40, 4, 0}
				want = 0
			}
			if id := focus(t, c, o); id != want {
				t.Fatalf("selected %d, want baseline %d", id, want)
			}
		})
	}
}

func TestPressurePerKillTimeIncludesMagazineReload(t *testing.T) {
	for _, tt := range []struct{ ammo, want int }{{12, 0}, {2, 4}} {
		c, o := pressureScene(t, false)
		o.Mechs[0].Pos, o.Mechs[1].Pos = wire.Vec3{40, -4, 0}, wire.Vec3{40, 4, 0}
		o.Mechs[0].HP, o.Mechs[1].HP = 90, 135
		o.Self.Weapons[0].Ammo = tt.ammo
		o.Events = []wire.Event{incoming(4, 50), incoming(0, 100)}
		if id := focus(t, c, o); id != tt.want {
			t.Errorf("%d rounds selected %d, want %d", tt.ammo, id, tt.want)
		}
	}
}

func TestPressureUsesAgeAndPreservesCurrentOnTies(t *testing.T) {
	c, o := pressureScene(t, false)
	o.Mechs[0].Pos, o.Mechs[1].Pos = wire.Vec3{40, -4, 0}, wire.Vec3{40, 4, 0}
	o.Mechs[0].HP, o.Mechs[1].HP = 225, 225
	c.target = goal{"enemy", 4}
	o.Events = []wire.Event{incoming(0, 120), incoming(4, 120)}
	if id := focus(t, c, o); id != 4 {
		t.Fatalf("tied candidates chose %d, want current 4", id)
	}
	advance(&o, 80)
	o.Events = []wire.Event{incoming(0, 40)}
	if id := focus(t, c, o); id != 0 {
		t.Fatalf("new dominant pressure chose %d, want 0", id)
	}
}

func TestPressureLaserTransferRequiresSafeRelease(t *testing.T) {
	for _, tt := range []struct {
		name   string
		offset float64
		want   int
	}{{"unsafe", 20, 4}, {"safe", .4, 0}} {
		t.Run(tt.name, func(t *testing.T) {
			c, o := pressureScene(t, false)
			c.cfg.Weapons["laser"] = map[string]float64{"damage": 315, "range": 150, "charge": .75, "min_charge": .15, "hold_s": 2, "cooldown": 1.65, "charge_turn_mult": 1}
			o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"}
			o.Mechs[0].HP = 700
			old := o.Mechs[0]
			o.Mechs = []wire.Actor{old}
			if a := c.Decide(o); !a.Fire[1] {
				t.Fatal("laser failed to start on actor 4")
			}
			advance(&o, 2)
			o.Self.Weapons[1].Charge = .75
			o.Mechs = append(o.Mechs, wire.Actor{ID: 0, Kind: "mech", Pos: wire.Vec3{35, tt.offset, 0}, HP: 900})
			o.Events = []wire.Event{incoming(0, 100)}
			a := c.Decide(o)
			if c.target != (goal{"enemy", tt.want}) || a.Fire[1] {
				t.Fatalf("target=%v laserHeld=%v, want safe release on %d", c.target, a.Fire[1], tt.want)
			}
		})
	}
}

func TestPressureTiedKillTimesPreferCurrentOverFasterInitialSlew(t *testing.T) {
	c, o := pressureScene(t, false)
	o.Self.Weapons[0] = nil
	o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"}
	c.cfg.Weapons["laser"] = map[string]float64{"damage": 315, "range": 150, "charge": .75, "min_charge": .15, "hold_s": 2, "cooldown": 1.65, "charge_turn_mult": 1}
	o.Mechs[0].Pos = wire.Vec3{10, 39, 0}
	o.Mechs[1].Pos = wire.Vec3{40, 0, 0}
	o.Mechs[0].HP, o.Mechs[1].HP = 500, 500
	c.target = goal{"enemy", 4}
	o.Events = []wire.Event{incoming(0, 100), incoming(4, 100)}
	// Both targets take two full charges. Turning to either fits inside the
	// first charge, even though their S02 times to minimum charge differ.
	if id := focus(t, c, o); id != 4 {
		t.Fatalf("equal removal scores chose %d, want current 4", id)
	}
}

func TestPressureFinishingVolleyIncludesOnlyDirectArc(t *testing.T) {
	for _, tt := range []struct {
		name  string
		crate bool
		want  int
	}{{"gun plus arc", false, 4}, {"crate takes arc", true, 0}} {
		t.Run(tt.name, func(t *testing.T) {
			c, o := pressureScene(t, false)
			o.Mechs[0].HP = 105
			o.Self.Weapons[1] = &wire.Weapon{Kind: "lightning"}
			if tt.crate {
				c.cfg.Crates = wire.Numbers{"height": 5}
				o.Crates = []wire.Crate{{ID: 1001, Pos: wire.Vec3{10, 0, 0}, HP: 800}}
			}
			o.Events = []wire.Event{incoming(0, 100)}
			if id := focus(t, c, o); id != tt.want {
				t.Fatalf("selected %d, want %d", id, tt.want)
			}
		})
	}
}

func TestPressureKillTimeIncludesObservedWeaponTimings(t *testing.T) {
	c, o := pressureScene(t, false)
	o.Mechs[0].Pos = wire.Vec3{40, 0, 0}
	o.Self.Aim = wire.Vec2{0, 0}
	o.Self.Stats["muzzle_height"] = 4.5
	gun := o.Self.Weapons[0]
	gun.Cooldown = .4
	gun.Stats = map[string]float64{"speed": 200}
	shot, ok := c.solveTarget(o, 4, o.Mechs[0].Pos, wire.Vec3{}, 9, 2.5, 90, true)
	if !ok {
		t.Fatal("fixture has no gun solution")
	}
	check := func(want float64) {
		t.Helper()
		if got := c.killTime(o, shot); math.Abs(got-want) > 1e-6 {
			t.Fatalf("estimated kill %v, want %v", got, want)
		}
	}
	check(.84) // .4 + .2 flight, then .24
	gun.Ammo, gun.Reload = 0, 1.1
	check(1.54)
	gun.Reload = 0
	check(2.84)
	o.Self.Weapons[1] = &wire.Weapon{Kind: "laser", Charge: .5}
	c.cfg.Weapons["laser"] = map[string]float64{"damage": 100, "range": 150, "charge": .75, "min_charge": .15, "hold_s": 2, "cooldown": 1.65, "charge_turn_mult": 1}
	c.observeLaser(o)
	check(.25) // existing half-second of charge
	o.Self.Weapons[1].Charge = 0
	o.Self.Weapons[1].Cooldown = .5
	c.observeLaser(o)
	check(1.25)
	o.Self.Weapons[1] = &wire.Weapon{Kind: "lightning"}
	o.Self.Energy = 3
	gun.Ammo, gun.Cooldown = 12, 0
	check(.2) // 60 instant, then a single 45-damage bullet arrives
}

func TestPressureInRangeArcSurvivesOutOfRangeGunLead(t *testing.T) {
	c, o := pressureScene(t, false)
	o.Self.Stats["muzzle_height"] = 4.5
	o.Self.Aim = wire.Vec2{0, 0}
	o.Self.Weapons[0].Stats = map[string]float64{"range": 43}
	o.Self.Weapons[1] = &wire.Weapon{Kind: "lightning", Cooldown: .65}
	shot, ok := c.solveTarget(o, 4, wire.Vec3{40, 0, 0}, wire.Vec3{40, 0, 0}, 9, 2.5, 60, true)
	if !ok {
		t.Fatal("fixture has no projectile solution")
	}
	if got := c.killTime(o, shot); math.Abs(got-.65) > 1e-6 {
		t.Fatalf("arc on target 40m away gives kill in %v, want .65", got)
	}
}

func TestPressureRecentSmallHitOutweighsOldLargeHit(t *testing.T) {
	c, o := pressureScene(t, false)
	o.Mechs[0].Pos, o.Mechs[1].Pos = wire.Vec3{40, -4, 0}, wire.Vec3{40, 4, 0}
	o.Mechs[0].HP, o.Mechs[1].HP = 225, 225
	o.Events = []wire.Event{incoming(4, 150)}
	if id := focus(t, c, o); id != 4 {
		t.Fatalf("initial actor %d", id)
	}
	advance(&o, 80)
	o.Events = []wire.Event{incoming(0, 45)}
	if id := focus(t, c, o); id != 0 {
		t.Fatalf("150 damage four seconds ago beat fresh 45, selected %d", id)
	}
}

func TestPressureLaserTransferThroughObservedActorNeedsPierce(t *testing.T) {
	for _, tt := range []struct {
		pierce float64
		want   int
	}{{0, 4}, {1, 0}} {
		c, o := pressureScene(t, false)
		c.cfg.Weapons["laser"] = map[string]float64{"damage": 315, "range": 150, "charge": .75, "min_charge": .15, "hold_s": 2, "cooldown": 1.65, "charge_turn_mult": 1, "pierce": tt.pierce}
		o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"}
		o.Mechs = o.Mechs[:1]
		o.Mechs[0].HP = 700
		c.Decide(o)
		advance(&o, 2)
		o.Self.Weapons[1].Charge = .75
		o.Mechs = append(o.Mechs, wire.Actor{ID: 0, Kind: "mech", Pos: wire.Vec3{55, 0, 0}, HP: 900})
		o.Events = []wire.Event{incoming(0, 100)}
		if id := focus(t, c, o); id != tt.want {
			t.Errorf("pierce %.0f selected %d, want %d", tt.pierce, id, tt.want)
		}
	}
}
