package decision

import (
	"math"
	"testing"

	"ownbot/internal/wire"
)

func exposedRecovery(t *testing.T, weapon string) (*Controller, wire.Observation) {
	t.Helper()
	c, o := laserScene(t, true)
	o.Self.Weapons[1] = &wire.Weapon{Kind: weapon}
	o.Self.Pos = wire.Vec3{0, 21, 0}
	o.Self.HP = 400
	o.Mechs = []wire.Actor{{ID: 1, Kind: "mech", Pos: wire.Vec3{35, 22, 0}}, {ID: 2, Kind: "mech", Pos: wire.Vec3{-35, 21, 0}}}
	o.Self.Aim = wire.Vec2{math.Pi, math.Atan2(-2.5, 35)}
	return c, o
}

func TestPartialCoverRouteKeepsNonlaserReturnFire(t *testing.T) {
	for _, kind := range []string{"lightning", "laser"} {
		t.Run(kind, func(t *testing.T) {
			c, o := exposedRecovery(t, kind)
			a := c.Decide(o)
			if c.target.kind != "cover" || c.exposure(o, c.cover) != 1 || a.Move == (wire.Vec2{}) {
				t.Fatalf("expected route blocking one of two sight lines, got %+v dest=%v", a, c.cover)
			}
			if a.Fire != [2]bool{true, kind == "lightning"} {
				t.Fatalf("should retain legal nonlaser shots during recovery, got %+v", a)
			}
		})
	}
}

func TestNearbyCoveredDestinationIsNotCurrentCover(t *testing.T) {
	c, o := sample(t, true)
	o.Self.HP = 400
	o.Mechs = []wire.Actor{{ID: 1, Kind: "mech", Pos: wire.Vec3{35, 22, 0}}}
	o.Self.Pos = wire.Vec3{0, 20.1, 0}
	o.Self.Aim = wire.Vec2{math.Atan2(1.9, 35), math.Atan2(-2.5, math.Hypot(35, 1.9))}
	c.lastDamage = o.T - .1
	c.coverValid = true
	c.cover = wire.Vec3{0, 19.5, 0}
	if c.exposure(o, c.cover) != 0 || c.exposure(o, o.Self.Pos) != 1 {
		t.Fatal("fixture should straddle building shadow")
	}
	a := c.Decide(o)
	if !a.Fire[0] || a.Move[1] >= 0 {
		t.Fatalf("still exposed; should continue toward shadow and fire: %+v", a)
	}
	o.Self.Pos = c.cover
	advance(&o, 2)
	a = c.Decide(o)
	if a.Fire != [2]bool{} || a.Move != (wire.Vec2{}) {
		t.Fatalf("should hold in actual cover: %+v", a)
	}
}

func TestZoneExitKeepsNonlaserShotDespiteRecoveryLatch(t *testing.T) {
	c, o := exposedRecovery(t, "laser")
	c.Decide(o)
	if !c.recovering {
		t.Fatal("fixture failed to start recovery")
	}
	advance(&o, 2)
	o.Self.InZone = false
	o.Zone.Radius, o.Zone.NextRadius = 10, 8
	a := c.Decide(o)
	if a.Fire != [2]bool{true, false} || a.Move[1] >= 0 || c.target.kind != "zone" {
		t.Fatalf("urgent zone exit should keep movement and gun, interrupt laser: %+v", a)
	}
}
