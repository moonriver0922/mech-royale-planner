package decision

import (
	"math"
	"reflect"
	"testing"

	"ownbot/internal/wire"
)

// The wall extends from x=5 to 10 and y=-20 to 20. Both actors start
// north of it. A short walk south places the wall between them.
func recoveryCorner(t *testing.T) (*Controller, wire.Observation) {
	t.Helper()
	c, o := sample(t, true)
	o.Self.Pos = wire.Vec3{0, 23, 0}
	o.Self.HP = 300
	o.Self.Aim = wire.Vec2{0, math.Atan2(-2.5, 30)}
	o.Mechs = []wire.Actor{{ID: 9, Kind: "mech", Pos: wire.Vec3{30, 23, 0}, Weapons: []string{"gun"}}}
	c.cfg.Mech["boost_speed"] = 20
	return c, o
}

func TestRecoveryTravelFiresAndStopsOnlyInShelter(t *testing.T) {
	for _, secondary := range []string{"lightning", "laser"} {
		t.Run(secondary, func(t *testing.T) {
			c, o := recoveryCorner(t)
			c.cfg.Weapons["laser"] = map[string]float64{"range": 80, "charge": .75, "min_charge": .15}
			o.Self.Weapons[1] = &wire.Weapon{Kind: secondary}
			a := c.Decide(o)
			if a.Move[1] >= 0 || !c.coverValid || !a.Fire[0] || a.Fire[1] != (secondary == "lightning") {
				t.Fatalf("travel should head behind wall with legal fire: %+v cover=%v", a, c.cover)
			}
			o.Self.Pos = c.cover
			o.Events = []wire.Event{{Type: "hit", Target: 9, Amount: 45, Weapon: "gun"}}
			advance(&o, 2)
			if a = c.Decide(o); a.Move != (wire.Vec2{}) || a.Fire != [2]bool{} || !c.recovering {
				t.Fatalf("must hold this shelter immediately after last dealt hit: %+v", a)
			}
		})
	}
}

func TestRecoveryRejectsPartialCoverAndReturnsToCombat(t *testing.T) {
	c, o := recoveryCorner(t)
	o.Mechs = append(o.Mechs, wire.Actor{ID: 10, Kind: "mech", Pos: wire.Vec3{-30, 23, 0}, Weapons: []string{"gun"}})
	a := c.Decide(o)
	if c.coverValid || c.target != (goal{"enemy", 9}) || !a.Fire[0] {
		t.Fatalf("opposite clear threats require normal engagement: %+v goal=%+v cover=%v", a, c.target, c.cover)
	}
}

func TestRecoveryUncoveredUsesNormalResourceWhenCombatUnready(t *testing.T) {
	c, o := sample(t, false)
	o.Self.HP = 100
	o.Self.Weapons[0].Reload = 2
	o.Mechs = []wire.Actor{{ID: 8, Kind: "mech", Pos: wire.Vec3{40, 0, 0}, Weapons: []string{"gun"}}}
	o.Pickups = []wire.Pickup{{ID: 8009, Kind: "lightning", Pos: o.Self.Pos}}
	a := c.Decide(o)
	if a.Take == nil || *a.Take != 8009 {
		t.Fatalf("expected normal in-range pickup, got %+v", a)
	}
}

func TestRecoveryCurrentShelterAccountsForActionableThreats(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		x          float64
		weapons    []string
		hold       bool
	}{
		{"remote drone", "npc", 60, nil, true}, {"near drone", "npc", 30, nil, false},
		{"remote arc", "mech", 75, []string{"lightning"}, true}, {"gun and arc", "mech", 75, []string{"lightning", "gun"}, false},
		{"empty slots", "mech", 20, []string{}, true}, {"mortar without range field", "mech", 75, []string{"mortar"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, o := sample(t, false)
			c.cfg.NPC["height"] = 3.5
			c.cfg.Weapons["mortar"] = map[string]float64{"speed": 72, "gravity": 22, "splash_radius": 7}
			o.Self.HP = 250
			o.Mechs = []wire.Actor{{ID: 8, Kind: tc.kind, Pos: wire.Vec3{tc.x, 0, 0}, Weapons: tc.weapons}}
			a := c.Decide(o)
			holding := c.recovering && a.Move == (wire.Vec2{}) && a.Fire == [2]bool{}
			if holding != tc.hold {
				t.Fatalf("holding=%v, want %v: %+v", holding, tc.hold, a)
			}
		})
	}
}

func TestRecoveryRequiresAllOfBodyToBeSheltered(t *testing.T) {
	c, o := recoveryCorner(t)
	// Midpoint ray is just inside the north edge of the wall, the north
	// side of the body is exposed to the shooter's westward ray.
	o.Self.Pos[1] = 18
	o.Mechs[0].Pos[1] = 20
	a := c.Decide(o)
	if a.Move == (wire.Vec2{}) {
		t.Fatalf("center-only cover incorrectly held while side exposed: %+v", a)
	}
}

func TestRecoveryRecentDamageGatesTravelButAllowsRealShelter(t *testing.T) {
	c, o := recoveryCorner(t)
	id := 9
	o.Events = []wire.Event{{Type: "damaged", By: &id, Amount: 50}}
	c.Decide(o)
	advance(&o, 12)
	o.Self.HP = 25
	a := c.Decide(o)
	if c.coverValid || c.target.kind != "enemy" || !a.Fire[0] {
		t.Fatalf("incoming rate should reject lethal walk: %+v goal=%+v", a, c.target)
	}
	advance(&o, 2)
	o.Self.Pos[1] = 0
	if a = c.Decide(o); !c.recovering || a.Move != (wire.Vec2{}) || a.Fire != [2]bool{} {
		t.Fatalf("damage rate should not veto present shelter: %+v", a)
	}
	o.Self.InZone = false
	o.Self.Pos[0] = -15
	o.Zone = wire.Zone{Radius: 8, NextRadius: 8}
	if a = c.Decide(o); c.target.kind != "zone" || a.Move[0] <= 0 {
		t.Fatalf("damage must not veto urgent zone escape: %+v", a)
	}
}

func TestRecoveryRecentUnseenAttackerPreventsHoldingUntilWindowExpires(t *testing.T) {
	c, o := sample(t, false)
	o.Self.HP = 200
	id := 13
	c.Decide(o) // Already holding shelter when a previously unseen attacker hits.
	advance(&o, 2)
	o.Events = []wire.Event{{Type: "damaged", By: &id, Amount: 45}}
	c.Decide(o)
	advance(&o, 2)
	o.Events = nil
	if a := c.Decide(o); a.Move == (wire.Vec2{}) || c.coverValid {
		t.Fatalf("unaccounted attacker should leave normal movement active: %+v", a)
	}
	advance(&o, 102)
	if a := c.Decide(o); a.Move != (wire.Vec2{}) || !c.recovering {
		t.Fatalf("expired attacker veto should allow holding: %+v", a)
	}
}

func TestRecoveryIncomingProjectileVetoesHolding(t *testing.T) {
	c, o := sample(t, false)
	o.Self.HP = 300
	o.Projectiles = []wire.Projectile{{Kind: "gun", Pos: wire.Vec3{15, 0, 4.5}, Vel: wire.Vec3{-100, 0, 0}}}
	if a := c.Decide(o); a.Move == (wire.Vec2{}) {
		t.Fatalf("imminent projectile reaches present shelter: %+v", a)
	}
	o.Projectiles[0].Vel[0] = 100
	advance(&o, 2)
	if a := c.Decide(o); a.Move != (wire.Vec2{}) || !c.recovering {
		t.Fatalf("outbound projectile should allow hold: %+v", a)
	}
}

func TestRecoveryRevalidatesMovingZoneForCachedEndpoint(t *testing.T) {
	c, o := recoveryCorner(t)
	c.Decide(o)
	if !c.coverValid {
		t.Fatal("expected first plan")
	}
	old := c.cover
	o.Zone = wire.Zone{Center: wire.Vec2{0, 23}, Radius: 45, NextCenter: wire.Vec2{0, 35}, NextRadius: 13, ShrinkStart: o.T, ShrinkEnd: o.T + .25}
	advance(&o, 2)
	a := c.Decide(o)
	if c.coverValid && c.cover == old || c.target.kind != "enemy" || !a.Fire[0] {
		t.Fatalf("old cover moves outside zone before arrival: %+v, goal=%+v cover=%v", a, c.target, c.cover)
	}
}

func TestRecoveryAboveEntryThresholdMatchesOrdinaryCombat(t *testing.T) {
	c1, o := recoveryCorner(t)
	c2, _ := recoveryCorner(t)
	o.Self.HP = 450
	got := c1.Decide(o)
	o.Self.HP = o.Self.MaxHP
	want := c2.Decide(o)
	if !reflect.DeepEqual(got, want) || got.Fire[0] != true || c1.target.kind != "enemy" {
		t.Fatalf("above-threshold action changed: got %+v want %+v", got, want)
	}
}

func TestRecoveryObservedHitOverridesNominalWeaponReach(t *testing.T) {
	for _, kind := range []string{"mech", "npc"} {
		t.Run(kind, func(t *testing.T) {
			c, o := sample(t, false)
			c.cfg.NPC["height"] = 3.5
			o.Self.HP = 300
			id := 8
			o.Mechs = []wire.Actor{{ID: id, Kind: kind, Pos: wire.Vec3{75, 0, 0}, Weapons: []string{"lightning"}}}
			o.Events = []wire.Event{{Type: "damaged", Amount: 60, By: &id, Weapon: "lightning"}}
			a := c.Decide(o)
			if c.coverValid || a.Move == (wire.Vec2{}) {
				t.Fatalf("actual attacker has a clear ray beyond nominal range: %+v", a)
			}
			o.Events = nil
			advance(&o, 110)
			if a = c.Decide(o); !c.coverValid || a.Move != (wire.Vec2{}) {
				t.Fatalf("expired reach evidence must revert to nominal range: %+v", a)
			}
		})
	}
}

func TestRecoveryRejectsGroundInsideRoofSupport(t *testing.T) {
	c, o := sample(t, true)
	o.Self.Pos = wire.Vec3{7.5, 0, 20} // standing on the top of the solid wall
	o.Self.Stats["walk_speed"] = 4
	o.Self.HP = 300
	o.Zone = wire.Zone{Center: wire.Vec2{7.5, 0}, Radius: 3, NextCenter: wire.Vec2{7.5, 0}, NextRadius: 3}
	o.Mechs = []wire.Actor{{ID: 8, Kind: "mech", Pos: wire.Vec3{7.5, 40, 20}, Weapons: []string{"gun"}}}
	a := c.Decide(o)
	if c.coverValid || c.target.kind != "enemy" {
		t.Fatalf("terrain under the roof is solid, got cover=%v goal=%+v action=%+v", c.cover, c.target, a)
	}
}

func TestRecoveryTravelIncludesThreatApproach(t *testing.T) {
	c, o := recoveryCorner(t)
	// Its current 45 m arc cannot reach the sheltered side. Nominal boost
	// approach within our ETA can, and that side has a clear ray to this actor.
	o.Mechs = append(o.Mechs, wire.Actor{ID: 10, Kind: "mech", Pos: wire.Vec3{-48, -4, 0}, Weapons: []string{"lightning"}})
	a := c.Decide(o)
	if c.coverValid || c.target.kind != "enemy" || !a.Fire[0] {
		t.Fatalf("approaching arc should invalidate travel: %+v cover=%v", a, c.cover)
	}
}

func TestRecoveryRateNeedsSeparatedAttributedObservations(t *testing.T) {
	c, o := recoveryCorner(t)
	o.Self.HP = 12
	id := 9
	cases := []struct {
		ticks  int
		events []wire.Event
	}{
		{0, []wire.Event{{Type: "damaged", By: &id, Amount: 250}, {Type: "damaged", By: &id, Amount: 250}}},
		{4, []wire.Event{{Type: "damaged", By: &id, Amount: 200}}},
		{8, []wire.Event{{Type: "damaged", Amount: 1000, Weapon: "zone"}, {Type: "hit", Target: id, Amount: 1000}}},
		{112, []wire.Event{{Type: "damaged", By: &id, Amount: 30}}},
	}
	for _, tc := range cases {
		advance(&o, tc.ticks)
		o.Events = tc.events
		a := c.Decide(o)
		if !c.coverValid || !a.Fire[0] || a.Move[1] >= 0 {
			t.Fatalf("credible walk should survive sparse/stale/unattributed samples at %.2f: %+v", o.T, a)
		}
	}
}

func TestRecoveryWallDoesNotStopReachableMortarLob(t *testing.T) {
	c, o := sample(t, true)
	o.Self.HP = 200
	o.Mechs = []wire.Actor{{ID: 9, Kind: "mech", Pos: wire.Vec3{35, 0, 0}, Weapons: []string{"gun", "mortar"}}}
	c.cfg.Weapons["mortar"] = map[string]float64{"speed": 72, "gravity": 22, "splash_radius": 7}
	a := c.Decide(o)
	if c.coverValid || a.Move == (wire.Vec2{}) {
		t.Fatalf("the wall blocks gun rays but a mortar can lob over: %+v cover=%v", a, c.cover)
	}
}

func TestRecoveryConfirmedKillRemovesOnlyThatAttacker(t *testing.T) {
	c, o := sample(t, false)
	o.Self.HP = 300
	first, second := 4, 7
	o.Events = []wire.Event{{Type: "damaged", By: &first, Amount: 45}, {Type: "damaged", By: &second, Amount: 45}}
	c.Decide(o)
	advance(&o, 2)
	o.Events = []wire.Event{{Type: "kill", Target: first}}
	if a := c.Decide(o); a.Move == (wire.Vec2{}) {
		t.Fatalf("second attacker still unresolved: %+v", a)
	}
	advance(&o, 2)
	o.Events = []wire.Event{{Type: "kill", Target: second}, {Type: "damaged", By: &second, Amount: 45}}
	if a := c.Decide(o); !c.coverValid || a.Move != (wire.Vec2{}) {
		t.Fatalf("two confirmed kills should release hold veto: %+v", a)
	}
	advance(&o, 2)
	o.Events = []wire.Event{{Type: "damaged", By: &first, Amount: 45}}
	if a := c.Decide(o); !c.coverValid || a.Move != (wire.Vec2{}) {
		t.Fatalf("old in-flight shot should not revive a dead attacker: %+v", a)
	}
}
