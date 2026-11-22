package decision

import (
	"encoding/json"
	"math"
	"testing"

	"ownbot/internal/wire"
)

func defenseFixture(t *testing.T, wall bool) (*Controller, wire.Observation) {
	t.Helper()
	c, o := sample(t, wall)
	c.cfg.Mech["boost_speed"], c.cfg.Mech["boost_drain"] = 20, 17
	c.cfg.Mech["step_up"], c.cfg.Mech["max_slope_deg"] = 1.2, 50
	c.cfg.Mech["charge_move_mult"] = .5
	c.cfg.Weapons["gun"]["damage"], c.cfg.NPC["damage"] = 45, 25
	c.cfg.Projectile = wire.Numbers{"npc_radius": .3, "gun_radius": .3}
	c.cfg.Weapons["laser"] = map[string]float64{"min_charge": .15, "charge": .75, "hold_s": 2}
	return c, o
}

func incoming(kind string, x, y, vx, vy float64) []wire.Projectile {
	return []wire.Projectile{{ID: 1, Kind: kind, Pos: wire.Vec3{x, y, 4}, Vel: wire.Vec3{vx, vy, 0}}}
}

func TestProjectileSweptHitChangesOnlyMovement(t *testing.T) {
	c, o := defenseFixture(t, false)
	p := 1
	a := wire.Action{Tick: 22, Move: wire.Vec2{1, 0}, Aim: wire.Vec2{.6, .3}, Fire: [2]bool{true, false}, Pick: &p, Take: &p, Drop: &p, Reload: true}
	// At 252 m/s the bullet crosses 12.6 m per tick, wider than the body.
	o.Projectiles = incoming("gun", 99, 0, -252, 0)
	got := c.defendProjectiles(o, a)
	if math.Abs(got.Move[1]) < .7 || got.Boost {
		t.Fatalf("wanted a walking sidestep, got %+v", got)
	}
	got.Move, got.Boost = a.Move, a.Boost
	if got != a {
		t.Fatalf("changed original action fields: %+v", got)
	}
}

func TestProjectileNearMissWallAndAirborneStayByteEquivalent(t *testing.T) {
	for _, k := range []string{"none", "miss", "overhead", "wall", "airborne", "jump", "mortar"} {
		t.Run(k, func(t *testing.T) {
			c, o := defenseFixture(t, k == "wall")
			a := wire.Action{Tick: 7, Aim: wire.Vec2{.2, .1}, Fire: [2]bool{true, true}, Reload: true}
			o.Projectiles = incoming("npc", 36, 0, -90, 0)
			switch k {
			case "none":
				o.Projectiles = nil
			case "miss":
				o.Projectiles[0].Pos[1] = 2.81
			case "overhead":
				o.Projectiles[0].Pos[2] = 9.31
			case "airborne":
				o.Self.Grounded = false
			case "jump":
				a.Jump = true
			case "mortar":
				o.Projectiles[0].Kind = "mortar"
			}
			want, _ := json.Marshal(a)
			got, _ := json.Marshal(c.defendProjectiles(o, a))
			if string(got) != string(want) {
				t.Errorf("got %s want %s", got, want)
			}
		})
	}
}

func TestProjectileInertiaMakesLateSidestepFail(t *testing.T) {
	c, o := defenseFixture(t, false)
	o.Projectiles = incoming("npc", 36, 0, -90, 0)
	o.Self.Energy = 0 // Compare the same walking choices at both velocities.
	a := wire.Action{Move: wire.Vec2{1, 0}}
	if c.defendProjectiles(o, a) == a {
		t.Fatal("stationary mech can escape")
	}
	o.Self.Vel[0] = 19
	if got := c.defendProjectiles(o, a); got != a {
		t.Fatalf("fast forward inertia leaves no escape: %+v", got)
	}
}

func TestProjectileBlockedSidestepChoosesOpenSide(t *testing.T) {
	c, o := defenseFixture(t, true)
	o.Self.Pos[0] = 2
	o.Projectiles = incoming("npc", 2, -36, 0, 90)
	got := c.defendProjectiles(o, wire.Action{})
	if got.Move[0] >= -.7 {
		t.Fatalf("wall to east, wanted west: %+v", got)
	}
}

func TestProjectileChargingSlowsEscape(t *testing.T) {
	c, o := defenseFixture(t, false)
	o.Projectiles = incoming("npc", 36, 0, -90, 0)
	o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"}
	o.Self.Energy = 0
	if got := c.defendProjectiles(o, wire.Action{}); got.Move == (wire.Vec2{}) {
		t.Fatal("unrestricted walker can escape")
	}
	c.observeLaser(o)
	a := wire.Action{Fire: [2]bool{false, true}}
	if got := c.defendProjectiles(o, a); got != a {
		t.Fatalf("newly charging walker cannot clear shot: %+v", got)
	}
}

func TestProjectileZoneRejectsUnsafeEscapeAndPreservesUrgentBoost(t *testing.T) {
	c, o := defenseFixture(t, false)
	o.Projectiles = incoming("npc", 36, 0, -90, 0)
	if got := c.defendProjectiles(o, wire.Action{}); got.Move == (wire.Vec2{}) {
		t.Fatal("large zone allows escape")
	}
	o.Zone.NextRadius, o.Zone.ShrinkStart, o.Zone.ShrinkEnd = 1, o.T, o.T+.6
	if got := c.defendProjectiles(o, wire.Action{}); got.Move != (wire.Vec2{}) {
		t.Fatalf("escape exits shrinking zone: %+v", got)
	}
	o.Self.InZone = false
	o.Zone = wire.Zone{Center: wire.Vec2{30, 0}, Radius: 1}
	a := wire.Action{Move: wire.Vec2{1, 0}, Boost: true}
	if got := c.defendProjectiles(o, a); got != a {
		t.Fatalf("escape loses original inward progress: %+v", got)
	}
}

func TestProjectileBoostRequiresFullEnergyAndIssuedLightning(t *testing.T) {
	c, o := defenseFixture(t, false)
	o.Projectiles = incoming("gun", 75.6, 0, -252, 0)
	a := wire.Action{}
	if got := c.defendProjectiles(o, a); !got.Boost || math.Abs(got.Move[1]) < .99 {
		t.Fatalf("only boost clears this shot: %+v", got)
	}
	o.Self.Energy = 10
	if got := c.defendProjectiles(o, a); got != a {
		t.Fatalf("insufficient energy: %+v", got)
	}
	o.Self.Energy = 12
	o.Self.Weapons[1] = &wire.Weapon{Kind: "lightning"}
	a.Fire[1] = true
	if got := c.defendProjectiles(o, a); got != a {
		t.Fatalf("boost starves issued lightning: %+v", got)
	}
}

func TestProjectileDecideUsesFinalMovementForBookkeeping(t *testing.T) {
	c, o := defenseFixture(t, false)
	o.Pickups = []wire.Pickup{{ID: 8010, Kind: "lightning", Pos: o.Self.Pos}}
	base := c.Decide(o)
	if base.Move != (wire.Vec2{}) || base.Take == nil {
		t.Fatalf("stationary pickup fixture: %+v", base)
	}
	o.Projectiles = incoming("npc", 36, 0, -90, 0)
	a := c.Decide(o)
	if a.Move == (wire.Vec2{}) || !c.moving || a.Take == nil || *a.Take != 8010 {
		t.Fatalf("expected moving pickup: %+v moving=%v", a, c.moving)
	}
}

func TestProjectileNominalDamageCountsEachVisibleRoundOnce(t *testing.T) {
	c, o := defenseFixture(t, false)
	o.Self.Stats["radius"], o.Self.Stats["height"] = 3.5, 12
	o.Projectiles = append(incoming("gun", 75.6, 3.6, -252, 0), incoming("npc", 36, 0, -90, 0)...)
	o.Projectiles[0].Pos[2] = 11
	o.Projectiles[1].ID = 2
	o.Projectiles = append(o.Projectiles, wire.Projectile{Kind: "mortar", Pos: wire.Vec3{0, 0, 4}})
	m := c.defenseModel(o, wire.Action{})
	p, ok := c.groundPath(o, m, groundCommand{})
	if !ok {
		t.Fatal("stationary flat path rejected")
	}
	dmg, first := c.pathDamage(m, p)
	if dmg != 70 || first != 0 {
		t.Fatalf("nominal gun plus NPC, gun first: got damage %v first %v", dmg, first)
	}
}

func TestProjectileContactPrecedesWallBehindTarget(t *testing.T) {
	c, o := defenseFixture(t, true)
	o.Projectiles = incoming("npc", -36, 0, 90, 0)
	if got := c.defendProjectiles(o, wire.Action{}); math.Abs(got.Move[1]) < .7 {
		t.Fatalf("wall behind mech incorrectly masked earlier contact: %+v", got)
	}
}

func TestProjectileRoofEdgeRejectsUnsupportedEscape(t *testing.T) {
	c, o := defenseFixture(t, true)
	o.Self.Pos = wire.Vec3{7.5, 0, 20}
	o.Projectiles = incoming("npc", 7.5, -52, 0, 90)
	o.Projectiles[0].Pos[2] = 24
	if got := c.defendProjectiles(o, wire.Action{}); got.Move[0] != 0 {
		t.Fatalf("escape from a 5 m wide roof enters an unsupported edge: %+v", got)
	}
	// At 52 m the mech can run north until the forecast ends. At 40 m even
	// that supported route gets hit within the horizon, so keep the original.
	o.Projectiles[0].Pos[1] = -40
	if got := c.defendProjectiles(o, wire.Action{}); got.Move != (wire.Vec2{}) {
		t.Fatalf("all supported paths take equal damage: %+v", got)
	}
}

func TestProjectileInteriorRoofStillAllowsWalkingEscape(t *testing.T) {
	c, o := defenseFixture(t, true)
	o.Self.Pos = wire.Vec3{7.5, 0, 20}
	o.Projectiles = incoming("npc", -28.5, 0, 90, 0)
	o.Projectiles[0].Pos[2] = 24
	got := c.defendProjectiles(o, wire.Action{})
	if math.Abs(got.Move[1]) < .99 || got.Boost {
		t.Fatalf("long north/south roof supports a walking escape: %+v", got)
	}
}
