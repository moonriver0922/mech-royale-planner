package decision

import (
	"math"
	"ownbot/internal/wire"
	"reflect"
	"testing"
)

func laserScene(t *testing.T, wall bool) (*Controller, wire.Observation) {
	t.Helper()
	c, o := sample(t, wall)
	c.cfg.Weapons["laser"] = map[string]float64{"range": 80, "charge": .75, "min_charge": .15, "hold_s": 2, "cooldown": 1.65, "charge_turn_mult": 1}
	c.cfg.Weapons["lightning"]["short_s"] = 2
	c.cfg.Mech["charge_move_mult"] = .5
	o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"}
	o.Mechs = []wire.Actor{{ID: 11, Kind: "mech", Pos: wire.Vec3{50, 0, 0}}}
	o.Self.Aim = wire.Vec2{0, math.Atan2(-2.5, 50)}
	return c, o
}

func advance(o *wire.Observation, n int) { o.Tick += n; o.T += float64(n) / 20 }

func TestLaserCycleSharedTurret(t *testing.T) {
	c, o := laserScene(t, false)
	if a := c.Decide(o); a.Fire != [2]bool{true, true} {
		t.Fatalf("aligned gun and start charge: %+v", a)
	}
	advance(&o, 2)
	o.Self.Weapons[1].Charge = .1
	if a := c.Decide(o); a.Fire != [2]bool{true, true} {
		t.Fatalf("aligned gun while charging: %+v", a)
	}
	advance(&o, 2)
	o.Mechs[0].Vel[1] = 30
	o.Self.Aim[0] = .12
	o.Self.Weapons[1].Charge = .75
	a := c.Decide(o)
	if !a.Fire[1] || a.Fire[0] || math.Abs(a.Aim[0]) > 1e-8 {
		t.Fatalf("slew from gun lead to hitscan before release: %+v", a)
	}
	advance(&o, 2)
	o.Self.Aim = a.Aim
	a = c.Decide(o)
	if a.Fire[1] || a.Fire[0] || math.Abs(a.Aim[0]) > 1e-8 {
		t.Fatalf("aligned hitscan release: %+v", a)
	}
	advance(&o, 2)
	if a = c.Decide(o); a.Fire[1] {
		t.Fatalf("released charge was pressed again: %+v", a)
	}
	advance(&o, 2)
	o.Self.Weapons[1].Cooldown = 1.5
	o.Self.Weapons[1].Charge = 0
	if a = c.Decide(o); a.Fire[1] {
		t.Fatal("charged during cooldown")
	}
	advance(&o, 32)
	o.Self.Weapons[1].Cooldown = 0
	if a = c.Decide(o); !a.Fire[1] {
		t.Fatalf("did not restart after observed reset: %+v", a)
	}
}

func TestLaserRangeClearanceAndObservedStats(t *testing.T) {
	for _, tt := range []struct {
		name        string
		wall        bool
		x, z, limit float64
		want        bool
	}{
		{"clear", false, 50, 0, 55, true}, {"outside", false, 50, 0, 40, false}, {"wall", true, 50, 0, 55, false}, {"muzzle wall", true, 8, 0, 55, false}, {"vertical limit", false, 5, 50, 80, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, o := laserScene(t, tt.wall)
			o.Self.Weapons[0].Reload = 2
			o.Mechs[0].Pos = wire.Vec3{tt.x, 0, tt.z}
			o.Self.Weapons[1].Stats = map[string]float64{"range": tt.limit}
			if a := c.Decide(o); a.Fire[1] != tt.want {
				t.Fatalf("charge=%t, want %t: %+v", a.Fire[1], tt.want, a)
			}
		})
	}
}

func TestLaserEmergencyQueueShortAndSlotReset(t *testing.T) {
	c, o := laserScene(t, false)
	if !c.Decide(o).Fire[1] {
		t.Fatal("never charged")
	}
	advance(&o, 2)
	o.Self.Weapons[1].Charge = .1
	o.Self.InZone = false
	o.Self.Pos[0] = 10
	o.Zone.Radius, o.Zone.NextRadius = 8, 8
	a := c.Decide(o)
	if a.Fire[1] || a.Move[0] >= 0 {
		t.Fatalf("zone must interrupt charge: %+v", a)
	}
	advance(&o, 2)
	o.Self.InZone = true
	o.Zone.Radius, o.Zone.NextRadius = 90, 90
	if c.Decide(o).Fire[1] {
		t.Fatal("pressed a queued early release")
	}
	o.Events = []wire.Event{{Type: "effect", Effect: "short"}}
	o.Self.Weapons[1].Charge = 0
	advance(&o, 2)
	if c.Decide(o).Fire[1] {
		t.Fatal("charged on short")
	}
	o.Events = nil
	advance(&o, 20)
	if c.Decide(o).Fire[1] {
		t.Fatal("charged while short")
	}
	advance(&o, 22)
	if !c.Decide(o).Fire[1] {
		t.Fatal("never restarted after short")
	}
	advance(&o, 2)
	o.Self.Weapons[1] = nil
	c.Decide(o)
	advance(&o, 2)
	o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"}
	if !c.Decide(o).Fire[1] {
		t.Fatal("new slot retained old laser state")
	}
}

func TestLaserPickupOpportunityAndCurrentRadius(t *testing.T) {
	c, o := laserScene(t, false)
	o.Mechs = nil
	o.Self.Weapons[1] = nil
	o.Pickups = []wire.Pickup{{ID: 810, Kind: "laser", Pos: wire.Vec3{6, 0, 0}}}
	a := c.Decide(o)
	if a.Take != nil || c.target != (goal{"weapon", 810}) || a.Move[0] <= 0 {
		t.Fatalf("laser should be approached: %+v goal=%+v", a, c.target)
	}
	o.Pickups[0].Pos = wire.Vec3{4, 0, 4}
	a = c.Decide(o)
	if a.Take == nil || *a.Take != 810 {
		t.Fatalf("legal radius edge: %+v", a)
	}
	for _, v := range []wire.Vec3{{4.01, 0, 0}, {1, 0, 4.01}} {
		o.Pickups[0].Pos = v
		if c.Decide(o).Take != nil {
			t.Fatalf("illegal pickup at %v", v)
		}
	}
	o.Pickups[0].Pos = wire.Vec3{}
	o.Cubes = []wire.Cube{{ID: 22, Pos: wire.Vec3{14, 0, 0}}}
	if a := c.Decide(o); a.Take == nil || *a.Take != 810 || c.target.kind != "cube" || a.Move[0] <= 0 {
		t.Fatalf("expected pickup while continuing toward cube: %+v %+v", a, c.target)
	}
	o.Cubes = nil
	o.Self.Weapons[1] = &wire.Weapon{Kind: "lightning"}
	if c.Decide(o).Take != nil {
		t.Fatal("replaced occupied slot")
	}
}

func TestLaserStartsWhileGunReloadsWithLootInReach(t *testing.T) {
	c, o := laserScene(t, false)
	o.Self.Weapons[0].Reload = 1.8
	o.Cubes = []wire.Cube{{ID: 5, Pos: wire.Vec3{4, 0, 0}}}
	a := c.Decide(o)
	if !a.Fire[1] || a.Fire[0] || c.target != (goal{"enemy", 11}) {
		t.Fatalf("usable laser lost combat slot: %+v goal=%+v", a, c.target)
	}
}

func TestLaserRangeLossAndBoundedHold(t *testing.T) {
	c, o := laserScene(t, false)
	o.Mechs[0].Pos[0] = 49.5
	o.Mechs[0].Vel[0] = 17
	o.Self.Weapons[1].Stats = map[string]float64{"range": 45}
	if !c.Decide(o).Fire[1] {
		t.Fatal("clear chance not charged")
	}
	advance(&o, 2)
	o.Self.Weapons[1].Charge = .2
	a := c.Decide(o)
	if a.Fire[1] || math.Abs(a.Aim[0]) > 1e-8 {
		t.Fatalf("missed still-aligned last range window: %+v", a)
	}

	c, o = laserScene(t, false)
	if !c.Decide(o).Fire[1] {
		t.Fatal("never started")
	}
	o.Self.Aim[0] = 1
	for i := 1; i <= 11; i++ {
		advance(&o, 2)
		o.Self.Weapons[1].Charge = min(.75, float64(i)/10)
		a = c.Decide(o)
		if i == 8 && !a.Fire[1] {
			t.Fatal("discharged sideways at first full charge")
		}
		if i == 11 && a.Fire[1] {
			t.Fatal("waited indefinitely at full charge")
		}
	}
}

func TestLaserHoldsBrieflyWhenObservedTargetCrossesWall(t *testing.T) {
	c, o := laserScene(t, true)
	o.Mechs[0].Pos = wire.Vec3{0, 30, 0}
	o.Self.Aim[0] = math.Pi / 2
	if !c.Decide(o).Fire[1] {
		t.Fatal("clear shot not charged")
	}
	// Target is still reported but its firing ray crosses the wall.
	advance(&o, 2)
	o.Self.Weapons[1].Charge = .7
	o.Mechs[0].Pos = wire.Vec3{20, 30, 0}
	if a := c.Decide(o); !a.Fire[1] || a.Fire[0] {
		t.Fatalf("released immediately into the wall: %+v", a)
	}
	// An actual disappearance ends combat and sends a release.
	o.Mechs = nil
	advance(&o, 2)
	o.Self.Weapons[1].Charge = .75
	if c.Decide(o).Fire[1] {
		t.Fatal("kept following invisible target")
	}
}

func TestLaserCoastQueueAndRecovery(t *testing.T) {
	c, o := laserScene(t, false)
	if !c.Decide(o).Fire[1] {
		t.Fatal("never started")
	}
	advance(&o, 4)
	o.Self.Weapons[1].Charge = .13
	if c.Decide(o).Fire[1] {
		t.Fatal("missed-decision release was re-pressed")
	}
	advance(&o, 2)
	o.Self.Weapons[1].Charge = .145
	if c.Decide(o).Fire[1] {
		t.Fatal("queued charge re-pressed before reset")
	}

	c, o = laserScene(t, true)
	o.Self.HP = 320
	o.Self.Weapons[1].Charge = .11
	o.Mechs[0].Pos[0] = 25
	if a := c.Decide(o); a.Fire != [2]bool{} || !c.recovering {
		t.Fatalf("failed to protect covered recovery: %+v", a)
	}
	advance(&o, 2)
	o.Events = []wire.Event{{Type: "hit", Target: 11, Weapon: "laser", Amount: 100}}
	if a := c.Decide(o); c.lastDealt != o.T || a.Fire != [2]bool{} {
		t.Fatalf("tail hit missing in recovery bookkeeping: %+v", a)
	}
}

func TestLaserSlowingAdvancesZoneDeparture(t *testing.T) {
	c, o := laserScene(t, false)
	o.Self.Pos[0] = 8
	o.Zone = wire.Zone{Radius: 10, NextRadius: 3, ShrinkStart: o.T, ShrinkEnd: o.T + 4}
	if _, urgent := c.zoneGoal(o); urgent {
		t.Fatal("fixture already urgent at walking speed")
	}
	if a := c.Decide(o); a.Fire[1] {
		t.Fatalf("started too late to leave the zone: %+v", a)
	}
	advance(&o, 2)
	o.Self.Weapons[1].Charge = .1
	if a := c.Decide(o); a.Fire[1] || c.target.kind != "zone" || a.Move[0] >= 0 {
		t.Fatalf("charging speed absent from zone ETA: %+v %+v", a, c.target)
	}
	if c.travel(o, 12) <= 1.2 {
		t.Fatal("queued release restored full speed too early")
	}
}

func TestLaserHandoffLeavesTimeForCompatibleGunOpportunity(t *testing.T) {
	c, o := laserScene(t, false)
	o.Mechs[0].Vel[1] = 30
	o.Self.Aim[0] = .12
	// At the official four rad/s the approximately 0.12 rad correction
	// takes 0.03 s. Another entire 0.1 s gun decision fits in 0.17 s.
	o.Self.Weapons[1].Charge = .58
	if a := c.Decide(o); !a.Fire[0] || !a.Fire[1] || a.Aim[0] < .1 {
		t.Fatalf("gave up an aligned gun opportunity prematurely: %+v", a)
	}
	advance(&o, 2)
	o.Self.Weapons[1].Charge = .68
	if a := c.Decide(o); a.Fire[0] || !a.Fire[1] || math.Abs(a.Aim[0]) > 1e-8 {
		t.Fatalf("late laser handoff did not start in time: %+v", a)
	}
}

func TestLaserShortCancelsSpeedPenaltyImmediately(t *testing.T) {
	c, o := laserScene(t, false)
	o.Self.Weapons[1].Charge = .12
	o.Events = []wire.Event{{Type: "effect", Effect: "short"}}
	a := c.Decide(o)
	if a.Fire[1] || c.travel(o, 12) > 1.2000001 {
		t.Fatalf("short should cancel both charge and slow movement, got fire=%v eta=%v", a.Fire, c.travel(o, 12))
	}
}

func TestUnsafeLaserStartYieldsToInwardCube(t *testing.T) {
	c, o := laserScene(t, false)
	o.Self.Pos[0] = 8
	o.Zone = wire.Zone{Radius: 10, NextRadius: 3, ShrinkStart: o.T, ShrinkEnd: o.T + 4}
	o.Self.Weapons[0].Reload = 1.8
	o.Cubes = []wire.Cube{{ID: 5, Pos: wire.Vec3{6, 0, 0}}}
	a := c.Decide(o)
	if c.target != (goal{"cube", 5}) || a.Move[0] >= 0 || a.Fire != [2]bool{} {
		t.Fatalf("expected inward cube movement while neither weapon can fire: action=%+v goal=%+v", a, c.target)
	}
}

func TestLaserPickupAlongsideGunshot(t *testing.T) {
	c, o := laserScene(t, false)
	o.Self.Weapons[1] = nil
	o.Offer = &wire.Offer{Choices: []wire.Choice{{ID: "unknown"}, {ID: "plating"}}}
	base := NewController(c.cfg, c.world).Decide(o)
	if !base.Fire[0] || base.Fire[1] || base.Move == (wire.Vec2{}) || base.Pick == nil || *base.Pick != 1 {
		t.Fatalf("expected walking, firing gun and choosing upgrade: %+v", base)
	}
	o.Pickups = []wire.Pickup{
		{ID: 8009, Kind: "laser", Pos: wire.Vec3{4, 0, 0}},
		{ID: 8002, Kind: "laser", Pos: wire.Vec3{3, 0, 4}},
		{ID: 8001, Kind: "laser", Pos: wire.Vec3{-3, 0, 4}},
	}
	a := c.Decide(o)
	if a.Take == nil || *a.Take != 8001 {
		t.Fatalf("expected nearest laser with lowest tied ID, got %+v", a)
	}
	a.Take = nil
	if !reflect.DeepEqual(base, a) {
		t.Fatalf("pickup changed another action: with=%+v without=%+v", a, base)
	}
	advance(&o, 2)
	o.Self.Weapons[1] = &wire.Weapon{Kind: "laser", Cooldown: 1.65}
	if a := c.Decide(o); a.Take != nil || a.Fire[1] {
		t.Fatalf("must wait for observed equip cooldown: %+v", a)
	}
}

func TestLaserPickupDuringCombatHonorsEligibility(t *testing.T) {
	for _, tt := range []struct {
		name, kind, primary, secondary string
		pos                            wire.Vec3
		failed                         bool
	}{
		{name: "beyond horizontal", kind: "laser", pos: wire.Vec3{4.01, 0, 0}},
		{name: "above reach", kind: "laser", pos: wire.Vec3{0, 0, 4.01}},
		{name: "filled slot", kind: "laser", secondary: "lightning"},
		{name: "same primary", kind: "laser", primary: "laser"},
		{name: "other pickup", kind: "mortar"},
		{name: "recently failed", kind: "laser", failed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, o := laserScene(t, false)
			o.Self.Weapons[1] = nil
			if tt.primary != "" {
				o.Self.Weapons[0] = &wire.Weapon{Kind: tt.primary}
			}
			if tt.secondary != "" {
				o.Self.Weapons[1] = &wire.Weapon{Kind: tt.secondary}
			}
			o.Pickups = []wire.Pickup{{ID: 8001, Kind: tt.kind, Pos: tt.pos}}
			if tt.failed {
				o.Events = []wire.Event{{Type: "take_failed", ID: []byte("8001")}}
			}
			if a := c.Decide(o); a.Take != nil {
				t.Fatalf("unexpected pickup: %+v", a)
			}
			if tt.failed {
				o.Events = nil
				advance(&o, 20)
				if a := c.Decide(o); a.Take == nil || *a.Take != 8001 {
					t.Fatalf("retry still suppressed: %+v", a)
				}
			}
		})
	}
}

func TestOpportunisticLaserPreservesChosenPickup(t *testing.T) {
	c, o := laserScene(t, false)
	o.Mechs = nil
	o.Self.Weapons[1] = nil
	o.Pickups = []wire.Pickup{{ID: 8001, Kind: "laser", Pos: o.Self.Pos}, {ID: 8002, Kind: "lightning", Pos: wire.Vec3{1, 0, 0}}}
	if a := c.Decide(o); a.Take == nil || *a.Take != 8002 {
		t.Fatalf("overrode selected lightning pickup: %+v", a)
	}
}
