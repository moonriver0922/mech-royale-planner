package decision

import (
	"math"
	"reflect"
	"testing"

	"ownbot/internal/wire"
)

func growthScene(t *testing.T, wall bool) (*Controller, wire.Observation) {
	t.Helper()
	c, o := sample(t, wall)
	c.cfg.Cubes.PickupRadius = 3.5
	c.cfg.Mech["step_up"], c.cfg.Mech["max_slope_deg"] = 1.2, 50
	c.cfg.Mech["charge_move_mult"] = .5
	c.cfg.NPC["height"], c.cfg.NPC["radius"] = 9, 2.5
	o.Mechs = []wire.Actor{{ID: 11, Kind: "mech", Pos: wire.Vec3{50, 0, 0}}}
	o.Self.Aim[1] = math.Atan2(-2.5, 50)
	return c, o
}

func compareWithoutLoot(t *testing.T, c *Controller, o wire.Observation) (wire.Action, wire.Action) {
	t.Helper()
	plain := o
	plain.Pickups, plain.Cubes, plain.Crates = nil, nil, nil
	baseline := *c
	want := baseline.Decide(plain)
	got := c.Decide(o)
	if c.target != baseline.target || !reflect.DeepEqual(c.route, baseline.route) || c.destination != baseline.destination {
		t.Fatalf("combat navigation changed: %+v %v, originally %+v %v", c.target, c.route, baseline.target, baseline.route)
	}
	rest := got
	rest.Move, rest.Take = want.Move, want.Take
	if !reflect.DeepEqual(rest, want) || c.laser != baseline.laser {
		t.Fatalf("combat action or laser state changed: %+v, originally %+v", got, want)
	}
	return got, want
}

func TestGrowthDuringPlayerFire(t *testing.T) {
	c, o := growthScene(t, false)
	o.Cubes = []wire.Cube{{ID: 22, Pos: wire.Vec3{-12, 0, 0}}}
	got, old := compareWithoutLoot(t, c, o)
	if !got.Fire[0] || got.Take != nil || got.Move != (wire.Vec2{-1, 0}) || got.Move == old.Move || c.target != (goal{"enemy", 11}) {
		t.Fatalf("expected westward cube collection with original gun shot, got %+v", got)
	}
}

func TestGrowthWeaponRequiresCurrentLegalPickup(t *testing.T) {
	for _, tt := range []struct {
		name, kind string
		pos        wire.Vec3
		take       bool
	}{
		{"lightning edge", "lightning", wire.Vec3{-4, 0, 4}, true},
		{"laser in range", "laser", wire.Vec3{-3, 0, 0}, true},
		{"next step in range", "lightning", wire.Vec3{-4.01, 0, 0}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, o := growthScene(t, false)
			o.Pickups = []wire.Pickup{{ID: 8800, Kind: tt.kind, Pos: tt.pos}}
			got, _ := compareWithoutLoot(t, c, o)
			if !got.Fire[0] || (got.Take != nil) != tt.take || (tt.take && *got.Take != 8800) || (!tt.take && got.Move[0] > -.99) {
				t.Fatalf("unexpected pickup/approach: %+v", got)
			}
		})
	}
}

func TestGrowthNonEligibleLeavesOriginalCombat(t *testing.T) {
	for _, tt := range []struct {
		name   string
		adjust func(*Controller, *wire.Observation)
	}{
		{"far", func(c *Controller, o *wire.Observation) { o.Cubes[0].Pos[0] = -42 }},
		{"roof loot", func(c *Controller, o *wire.Observation) { o.Cubes[0].Pos[2] = 5 }},
		{"roof self", func(c *Controller, o *wire.Observation) { o.Self.Pos[2] = 6 }},
		{"airborne", func(c *Controller, o *wire.Observation) { o.Self.Grounded = false }},
		{"stat cap even before upgrade", func(c *Controller, o *wire.Observation) { o.Self.Cubes = 10; c.cfg.Cubes.Thresholds = []int{11} }},
		{"projected zone", func(c *Controller, o *wire.Observation) {
			o.Zone = wire.Zone{Radius: 90, NextRadius: 4, ShrinkStart: o.T, ShrinkEnd: o.T + .5}
		}},
		{"NPC aim", func(c *Controller, o *wire.Observation) { o.Mechs[0].Kind = "npc" }},
		{"mortar", func(c *Controller, o *wire.Observation) {
			o.Cubes = nil
			o.Pickups = []wire.Pickup{{ID: 8, Kind: "mortar", Pos: wire.Vec3{-3, 0, 0}}}
		}},
		{"occupied slot", func(c *Controller, o *wire.Observation) {
			o.Cubes = nil
			o.Self.Weapons[1] = &wire.Weapon{Kind: "lightning"}
			o.Pickups = []wire.Pickup{{ID: 8, Kind: "laser", Pos: wire.Vec3{-3, 0, 0}}}
		}},
		{"recent failed take", func(c *Controller, o *wire.Observation) {
			o.Cubes = nil
			c.failedTake = 8
			c.retryTakeAt = o.T + 1
			o.Pickups = []wire.Pickup{{ID: 8, Kind: "laser", Pos: wire.Vec3{-3, 0, 0}}}
		}},
		{"crate", func(c *Controller, o *wire.Observation) {
			o.Cubes = nil
			o.Crates = []wire.Crate{{ID: 8, Pos: wire.Vec3{-10, 0, 0}}}
		}},
		{"urgent zone", func(c *Controller, o *wire.Observation) {
			o.Self.InZone = false
			o.Self.Pos[1] = 18
			o.Zone.Radius = 11
			o.Zone.NextRadius = 9
		}},
		{"uncovered recovery", func(c *Controller, o *wire.Observation) { o.Self.HP = 300 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, o := growthScene(t, false)
			o.Cubes = []wire.Cube{{ID: 3, Pos: wire.Vec3{-12, 0, 0}}}
			tt.adjust(c, &o)
			got, old := compareWithoutLoot(t, c, o)
			if !reflect.DeepEqual(got, old) {
				t.Fatalf("detour should be ignored: got %+v want %+v", got, old)
			}
		})
	}
}

func TestGrowthDoesNotDisplaceCoveredRecoveryOrWalkThroughWall(t *testing.T) {
	for _, covered := range []bool{false, true} {
		c, o := growthScene(t, true)
		if covered {
			o.Self.HP = 300
		} else {
			o.Mechs[0].Pos = wire.Vec3{0, 50, 0}
			o.Self.Aim[0] = math.Pi / 2
		}
		o.Cubes = []wire.Cube{{ID: 3, Pos: wire.Vec3{16, 0, 0}}}
		got, old := compareWithoutLoot(t, c, o)
		if !reflect.DeepEqual(got, old) || (covered && (!c.recovering || got.Move != (wire.Vec2{}))) {
			t.Fatalf("unexpected action: %+v", got)
		}
	}
}

func TestGrowthETAUsesVelocityAndStableID(t *testing.T) {
	for _, tt := range []struct {
		name                      string
		west, east, initial, want float64
	}{
		{"inertia outweighs shorter distance", 10, 9, -12, -1},
		{"stable lower ID", 10, 10, 0, 1},
		{"closer item within same physics tick", 10, 10.05, 0, -1},
	} {
		for _, reverse := range []bool{false, true} {
			t.Run(tt.name, func(t *testing.T) {
				c, o := growthScene(t, false)
				o.Self.Vel[0] = tt.initial
				o.Cubes = []wire.Cube{{ID: 9, Pos: wire.Vec3{-tt.west, 0, 0}}, {ID: 1, Pos: wire.Vec3{tt.east, 0, 0}}}
				if reverse {
					o.Cubes[0], o.Cubes[1] = o.Cubes[1], o.Cubes[0]
				}
				got, _ := compareWithoutLoot(t, c, o)
				if got.Move[0] != tt.want {
					t.Fatalf("wrong collection direction: %+v", got)
				}
			})
		}
	}
}

func TestGrowthThreeSecondsReflectsCurrentAndStartingLaser(t *testing.T) {
	for _, l := range []string{"none", "starting", "charging"} {
		t.Run(l, func(t *testing.T) {
			c, o := growthScene(t, false)
			if l != "none" {
				c.cfg.Weapons["laser"] = map[string]float64{"range": 80, "charge": .75, "min_charge": .15, "hold_s": 2, "cooldown": 1.65, "charge_turn_mult": 1}
				o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"}
				if l == "charging" {
					o.Self.Weapons[1].Charge = .15
				}
			}
			o.Cubes = []wire.Cube{{ID: 1, Pos: wire.Vec3{-38, 0, 0}}}
			got, old := compareWithoutLoot(t, c, o)
			if (l == "none" && got.Move[0] != -1) || (l != "none" && (!got.Fire[1] || got.Move != old.Move)) {
				t.Fatalf("wrong three-second decision for %s: %+v", l, got)
			}
		})
	}
}

func TestGrowthPreservesNextPhysicsTickGunAndLaserRays(t *testing.T) {
	for _, weapon := range []string{"gun", "laser"} {
		for _, y := range []float64{20.18, 20.4} {
			t.Run(weapon, func(t *testing.T) {
				c, o := growthScene(t, true)
				o.Self.Pos = wire.Vec3{-10, y, 0}
				o.Self.Vel[1] = -1.5
				o.Mechs[0].Pos = wire.Vec3{40, y, 0}
				if weapon == "laser" {
					c.cfg.Weapons["laser"] = map[string]float64{"range": 80, "charge": .75, "min_charge": .15, "hold_s": 2, "cooldown": 1.65, "charge_turn_mult": 1}
					o.Self.Weapons[0].Reload = 2
					o.Self.Weapons[1] = &wire.Weapon{Kind: "laser", Charge: .75}
				}
				o.Cubes = []wire.Cube{{ID: 1, Pos: wire.Vec3{-10, 6, 0}}}
				got, old := compareWithoutLoot(t, c, o)
				if (weapon == "gun" && !got.Fire[0]) || (weapon == "laser" && (got.Fire[1] || !c.laser.pending)) {
					t.Fatalf("fixture has no imminent shot: %+v", got)
				}
				if (y == 20.18 && got.Move != old.Move) || (y == 20.4 && got.Move[1] != -1) {
					t.Fatalf("wrong ray clearance at y=%v: %+v", y, got)
				}
			})
		}
	}
}

func TestGrowthAvoidsCrossingVisibleLightningRange(t *testing.T) {
	for _, arc := range []bool{false, true} {
		c, o := growthScene(t, false)
		// The target remains player 11. A second visible carrier bounds a westward detour.
		o.Mechs = append(o.Mechs, wire.Actor{ID: 15, Kind: "mech", Pos: wire.Vec3{-48, 0, 0}})
		if arc {
			o.Mechs[1].Weapons = []string{"gun", "lightning"}
		}
		o.Cubes = []wire.Cube{{ID: 2, Pos: wire.Vec3{-17, 0, 0}}}
		got, old := compareWithoutLoot(t, c, o)
		if c.target.id != 11 || (arc && got.Move != old.Move) || (!arc && got.Move[0] != -1) {
			t.Fatalf("wrong range decision arc=%v: %+v", arc, got)
		}
	}
}

func TestGrowthProtectsCubeAtAutomaticPickupBoundary(t *testing.T) {
	c, o := growthScene(t, false)
	o.Cubes = []wire.Cube{{ID: 4, Pos: wire.Vec3{0, -3.5, 0}}}
	got, old := compareWithoutLoot(t, c, o)
	if old.Move[1] < .99 || got.Move[1] != -1 || got.Take != nil {
		t.Fatalf("original northward motion leaves the boundary cube on the next tick: %+v", got)
	}
}

func TestGrowthDeclinesUnmodeledPrismSupport(t *testing.T) {
	c, o := growthScene(t, true)
	c.world.Prisms[0].Top = 1
	o.Cubes = []wire.Cube{{ID: 5, Pos: wire.Vec3{16, 0, 0}}}
	got, old := compareWithoutLoot(t, c, o)
	if !reflect.DeepEqual(got, old) {
		t.Fatalf("terrain-only projection crossed raised support: %+v", got)
	}
}

func TestGrowthChecksDelayedShotAfterFirstCollectionTick(t *testing.T) {
	for _, weapon := range []string{"gun", "queued laser"} {
		for _, tt := range []struct {
			name          string
			y, due        float64
			decisionTicks int
			blocked       bool
		}{
			{"second tick obstructed", 20.4, .08, 2, true},
			{"second tick clear", 20.8, .08, 2, false},
			{"third tick obstructed", 20.8, .13, 3, true},
		} {
			t.Run(weapon+"/"+tt.name, func(t *testing.T) {
				c, o := growthScene(t, true)
				c.cfg.Match["decide_every"] = float64(tt.decisionTicks)
				c.period = float64(tt.decisionTicks) / 20
				o.Self.Pos = wire.Vec3{-10, tt.y, 0}
				o.Self.Vel[1] = -1.5
				o.Mechs[0].Pos = wire.Vec3{40, tt.y, 0}
				o.Self.Weapons[0].Cooldown = tt.due
				if weapon == "queued laser" {
					c.cfg.Weapons["laser"] = map[string]float64{"range": 80, "charge": .75, "min_charge": .15, "hold_s": 2, "cooldown": 1.65, "charge_turn_mult": 1}
					o.Self.Weapons[0].Cooldown = c.period + .01
					o.Self.Weapons[1] = &wire.Weapon{Kind: "laser", Charge: .15 - tt.due}
					c.laser = laserState{present: true, slot: 1, pending: true, lastTick: o.Tick - tt.decisionTicks}
				}
				// The cube is reached on the first physics tick. The Move and
				// Fire command still apply for the rest of the decision.
				o.Cubes = []wire.Cube{{ID: 1, Pos: wire.Vec3{-10, tt.y - 3.6, 0}}}
				got, old := compareWithoutLoot(t, c, o)
				if (weapon == "gun" && !got.Fire[0]) || (weapon == "queued laser" && (got.Fire != [2]bool{} || !c.laser.pending)) {
					t.Fatalf("fixture does not have the intended shot: %+v", got)
				}
				if (tt.blocked && got.Move != old.Move) || (!tt.blocked && got.Move[1] != -1) {
					t.Fatalf("wrong movement at delayed %s tick: %+v", weapon, got)
				}
			})
		}
	}
}

func TestGrowthTracksTurretTurnUntilGunCooldownClears(t *testing.T) {
	c, o := growthScene(t, false)
	c.cfg.Mech["turn_rate"] = .24
	o.Mechs[0].Pos[1] = -.5
	o.Mechs[0].Vel[1] = 13
	o.Self.Weapons[0].Cooldown = .08
	o.Cubes = []wire.Cube{{ID: 1, Pos: wire.Vec3{-12, 0, 0}}}
	got, _ := compareWithoutLoot(t, c, o)
	// The turret gains .012 rad per tick. On tick two the lead is about
	// .068 rad. The actual .024 rad bearing still clears the gun's 2.25 m
	// tolerance, while a frozen first-tick bearing of .012 rad misses.
	if !got.Fire[0] || got.Move[0] != -1 || c.target != (goal{"enemy", 11}) {
		t.Fatalf("the held turret turns into a valid second-tick shot: %+v", got)
	}
}

func TestGrowthKeepsLightningRangeAfterFirstTickCollection(t *testing.T) {
	for _, tc := range []struct {
		carrierX float64
		safe     bool
	}{{-45.25, false}, {-46, true}} {
		c, o := growthScene(t, false)
		o.Self.Aim[0] = -.45 // No trigger while rotating to player 11.
		o.Mechs = append(o.Mechs, wire.Actor{ID: 15, Kind: "mech", Pos: wire.Vec3{tc.carrierX, 0, 0}, Weapons: []string{"gun", "lightning"}})
		o.Cubes = []wire.Cube{{ID: 2, Pos: wire.Vec3{-3.6, 0, 0}}}
		got, old := compareWithoutLoot(t, c, o)
		// From rest the westward detour travels 0.15 m and then 0.30 m.
		// The pickup is in reach after 0.15 m. The nearer carrier enters
		// 45 m only after 0.45 m, including the 2.5 m height difference.
		if got.Fire != [2]bool{} || c.target.id != 11 || (!tc.safe && got.Move != old.Move) || (tc.safe && got.Move[0] != -1) {
			t.Errorf("carrier at x=%v: got %+v, original %+v", tc.carrierX, got, old)
		}
	}
}

func TestGrowthKeepsZoneAfterFirstTickCollection(t *testing.T) {
	for _, tc := range []struct {
		radius float64
		safe   bool
	}{{50, false}, {51, true}} {
		c, o := growthScene(t, false)
		o.Self.Aim[0] = -.45
		o.Self.Vel[1] = 12
		o.Cubes = []wire.Cube{{ID: 2, Pos: wire.Vec3{-3.5, -.15, 0}}}
		o.Zone = wire.Zone{Center: wire.Vec2{0, -49.35}, Radius: tc.radius}
		got, old := compareWithoutLoot(t, c, o)
		// Lateral inertia carries the body north across y=0.65 on tick two.
		// The cube and the first possible collection endpoint are both inside.
		if got.Fire != [2]bool{} || c.target.id != 11 || (!tc.safe && got.Move != old.Move) || (tc.safe && got.Move[0] > -.99) {
			t.Errorf("zone radius %v: got %+v, original %+v", tc.radius, got, old)
		}
	}
}

func TestGrowthKeepsTerrainClearAfterFirstTickCollection(t *testing.T) {
	for _, wall := range []bool{true, false} {
		c, o := growthScene(t, wall)
		o.Self.Pos[0] = 1.8
		o.Self.Vel[0] = 12
		o.Self.Aim[0] = math.Pi/2 - .45
		o.Mechs[0].Pos = wire.Vec3{1.8, 50, 0}
		o.Cubes = []wire.Cube{{ID: 2, Pos: wire.Vec3{1.65, -3.5, 0}}}
		got, old := compareWithoutLoot(t, c, o)
		// The direct path to the cube clears the prism at x=5. Inertia
		// brings the 2.5 m body radius against it only on the second tick.
		if got.Fire != [2]bool{} || c.target.id != 11 || (wall && got.Move != old.Move) || (!wall && got.Move[1] > -.99) {
			t.Errorf("wall=%v: got %+v, original %+v", wall, got, old)
		}
	}
}
