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

func TestCrossingProjectileRespectsCurrentVelocityAndRetainsFire(t *testing.T) {
	for _, vy := range []float64{-12, 0, 12} {
		c, o := sample(t, false)
		frontTarget(&o)
		o.Self.Vel[1] = vy
		// At t=.40 the bullet crosses our x. From rest it intersects the
		// usual +y orbit. With negative velocity, reversing drives into it.
		y := 0.
		if vy == 0 {
			y = 4
		}
		o.Projectiles = []wire.Projectile{{Kind: "gun", Pos: wire.Vec3{80, y, 4.5}, Vel: wire.Vec3{-200, 0, 0}}}
		a := c.Decide(o)
		if !a.Fire[0] || (vy < 0 && a.Move[1] >= -.4) || (vy > 0 && a.Move[1] <= .4) || (vy == 0 && a.Move[1] > .15) {
			t.Errorf("velocity %.0f: should clear crossing and keep the shot, got %+v", vy, a)
		}
	}
}

func TestMovementChecksWholeTrajectoryNearWall(t *testing.T) {
	c, o := sample(t, true)
	o.Self.Pos = wire.Vec3{-1.3, 0, 0}
	o.Mechs = []wire.Actor{{ID: 1, Kind: "mech", Pos: wire.Vec3{-1.3, -40, 0}}}
	o.Self.Aim = wire.Vec2{-math.Pi / 2, math.Atan2(-2.5, 40)}
	a := c.Decide(o)
	// East is clear for S00's 3.6 m probe but the cylinder hits the x=5
	// wall after 3.8 m. Half a second of walking from rest covers >4.8 m.
	if a.Move[0] > .78 || !a.Fire[0] {
		t.Fatalf("walks into wall or lost valid shot: %+v", a)
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
