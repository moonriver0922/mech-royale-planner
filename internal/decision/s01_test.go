package decision

import (
	"math"
	"testing"

	"ownbot/internal/wire"
)

func incoming(id int, bearing, amount float64) wire.Event {
	return wire.Event{Type: "damaged", By: &id, Bearing: &bearing, Amount: amount, Rear: true, Weapon: "gun"}
}

func frontTarget(o *wire.Observation) {
	o.Mechs = []wire.Actor{{ID: 8, Kind: "mech", Pos: wire.Vec3{40, 0, 0}}}
	o.Self.Aim = wire.Vec2{0, math.Atan2(-2.5, 40)}
}

func TestThreatAcquisitionAndExpiry(t *testing.T) {
	c, o := sample(t, false)
	frontTarget(&o)
	o.Events = []wire.Event{incoming(13, -3, 67.5)}
	if a := c.Decide(o); math.Abs(a.Aim[0]+3) > .01 || a.Fire != [2]bool{} {
		t.Fatalf("rear report must displace shootable front target, got %+v", a)
	}
	o.Events = nil
	o.T += .6
	if a := c.Decide(o); math.Abs(a.Aim[0]+3) > .01 || a.Fire != [2]bool{} {
		t.Fatalf("lost bearing before there was time to acquire it: %+v", a)
	}
	o.Mechs = append(o.Mechs, wire.Actor{ID: 13, Kind: "mech", Pos: wire.Vec3{-30, -20, 0}})
	o.T += .1
	if a := c.Decide(o); math.Abs(a.Aim[0]-math.Atan2(-20, -30)) > .01 || a.Fire != [2]bool{} {
		t.Fatalf("current attacker position must replace old bearing: %+v", a)
	}
	o.Self.Aim = wire.Vec2{math.Atan2(-20, -30), math.Atan2(-2.5, math.Hypot(20, 30))}
	if a := c.Decide(o); !a.Fire[0] {
		t.Fatalf("acquired attacker should be fired on: %+v", a)
	}
	o.T += 3
	frontTarget(&o)
	if a := c.Decide(o); math.Abs(a.Aim[0]) > .01 || !a.Fire[0] {
		t.Fatalf("stale threat prevented normal engagement: %+v", a)
	}
}

func TestPartialCoverStillFiresAtRemainingEnemy(t *testing.T) {
	c, o := sample(t, true)
	o.Self.Pos = wire.Vec3{0, 21, 0}
	o.Self.HP = 400
	o.Mechs = []wire.Actor{{ID: 1, Kind: "mech", Pos: wire.Vec3{35, 22, 0}}, {ID: 2, Kind: "mech", Pos: wire.Vec3{-35, 21, 0}}}
	o.Self.Aim = wire.Vec2{math.Pi, math.Atan2(-2.5, 35)}
	if a := c.Decide(o); !a.Fire[0] {
		t.Fatalf("exposed to west even on improved route around wall, got %+v", a)
	}
}

func TestZoneExitKeepsLegalShotAfterPartialRecovery(t *testing.T) {
	c, o := sample(t, true)
	o.Self.Pos = wire.Vec3{0, 21, 0}
	o.Self.HP = 400
	o.Mechs = []wire.Actor{{ID: 1, Kind: "mech", Pos: wire.Vec3{35, 22, 0}}, {ID: 2, Kind: "mech", Pos: wire.Vec3{-35, 21, 0}}}
	o.Self.Aim = wire.Vec2{math.Pi, math.Atan2(-2.5, 35)}
	c.Decide(o)
	o.T += .1
	o.Self.InZone = false
	o.Zone.Radius, o.Zone.NextRadius = 10, 8
	a := c.Decide(o)
	if !a.Fire[0] || a.Move[1] >= 0 {
		t.Fatalf("zone correction should allow the exposed return shot: %+v", a)
	}
}

func TestRepeatedRearDamageAndStableArbitration(t *testing.T) {
	for _, ids := range [][2]int{{3, 4}, {4, 3}} {
		c, o := sample(t, false)
		frontTarget(&o)
		for i := range 2 {
			o.Events = nil
			for _, id := range ids {
				o.Events = append(o.Events, incoming(id, float64(id)-.2, 20))
			}
			a := c.Decide(o)
			if i == 0 && !a.Fire[0] {
				t.Fatalf("weak single hit displaced shot: %+v", a)
			}
			if i == 1 && (math.Abs(wrap(a.Aim[0]-2.8)) > .01 || a.Fire[0]) {
				t.Fatalf("repeated rear damage did not select stable strongest ID: %+v", a)
			}
			o.T += .1
		}
	}
}

func TestKillEndsAcquisitionEvenWithLateDamage(t *testing.T) {
	for _, killFirst := range []bool{false, true} {
		c, o := sample(t, false)
		frontTarget(&o)
		hit := incoming(13, -3, 67.5)
		o.Events = []wire.Event{hit}
		c.Decide(o)
		o.T += .1
		kill := wire.Event{Type: "kill", Target: 13}
		o.Events = []wire.Event{hit, kill}
		if killFirst {
			o.Events = []wire.Event{kill, hit}
		}
		if a := c.Decide(o); math.Abs(a.Aim[0]) > .01 || !a.Fire[0] {
			t.Fatalf("killFirst=%v: confirmed kill should return to live target: %+v", killFirst, a)
		}
		o.T += .1
		o.Events = []wire.Event{hit}
		if a := c.Decide(o); math.Abs(a.Aim[0]) > .01 || !a.Fire[0] {
			t.Fatalf("projectile arriving after kill reopened acquisition: %+v", a)
		}
		// Even a retained same-ID observation cannot make a confirmed kill
		// targetable again.
		o.Mechs[0].ID = 13
		o.Events = nil
		if a := c.Decide(o); a.Fire[0] {
			t.Fatalf("fire at confirmed eliminated ID: %+v", a)
		}
	}
}

func TestBearingOnlyKeepsInRangePickup(t *testing.T) {
	c, o := sample(t, false)
	o.Pickups = []wire.Pickup{{ID: 8001, Kind: "lightning", Pos: wire.Vec3{3, 0, 0}}}
	o.Events = []wire.Event{incoming(13, -3, 67.5)}
	a := c.Decide(o)
	if a.Take == nil || *a.Take != 8001 || math.Abs(a.Aim[0]+3) > .01 {
		t.Fatalf("pickup and facing conflict: %+v", a)
	}
}

func TestBearingOnlyRetainsExistingDetour(t *testing.T) {
	c, o := sample(t, true)
	o.Pickups = []wire.Pickup{{ID: 8001, Kind: "lightning", Pos: wire.Vec3{24, 0, 0}}}
	c.Decide(o)
	if c.target != (goal{"weapon", 8001}) || len(c.route) < 2 {
		t.Fatalf("fixture needs existing detour; got %v %v", c.target, c.route)
	}
	replanAt := c.replanAt
	o.T += .1
	o.Events = []wire.Event{incoming(13, -3, 67.5)}
	for range 2 {
		a := c.Decide(o)
		if a.Move == (wire.Vec2{}) || len(c.route) < 2 || c.target != (goal{"weapon", 8001}) || c.replanAt != replanAt {
			t.Fatalf("bearing discarded movement owner's detour: %+v goal=%v route=%v", a, c.target, c.route)
		}
		o.Events = nil
		o.T += .1
	}
}

func TestAcquiredAttackerOwnsCombatOrbit(t *testing.T) {
	c, o := sample(t, false)
	frontTarget(&o)
	o.Events = []wire.Event{incoming(13, math.Pi, 67.5)}
	o.Mechs = append(o.Mechs, wire.Actor{ID: 13, Kind: "mech", Pos: wire.Vec3{-40, 0, 0}})
	a := c.Decide(o)
	// At this time the nominal orbit is counter-clockwise. Facing the
	// target in the west puts the tangent to the south instead of north.
	if a.Move[1] > -.9 || math.Abs(wrap(a.Aim[0]-math.Pi)) > .01 || a.Fire[0] {
		t.Fatalf("facing and orbit should both switch to the acquired actor: %+v", a)
	}
}
