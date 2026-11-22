package decision

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"ownbot/internal/arena"
	"ownbot/internal/wire"
)

func sample(t *testing.T, wall bool) (*Controller, wire.Observation) {
	t.Helper()
	dir := t.TempDir()
	prisms := "[]"
	if wall {
		prisms = `[{"base":-1,"top":20,"ring":[5,-20,10,-20,10,20,5,20]}]`
	}
	world := `{"bounds":{"minX":-100,"maxX":100,"minY":-100,"maxY":100},
	"terrain":{"file":"terrain.i16","minX":-100,"minY":-100,"step":100,"scale":1,"nx":3,"ny":3},
	"rules":{"maxStep":1.2,"maxSlopeDeg":50},"prisms":` + prisms + `}`
	if err := os.WriteFile(filepath.Join(dir, "world.json"), []byte(world), 0600); err != nil {
		t.Fatal(err)
	}
	terrain := make([]byte, 9*binary.Size(int16(0)))
	if err := os.WriteFile(filepath.Join(dir, "terrain.i16"), terrain, 0600); err != nil {
		t.Fatal(err)
	}
	m, err := arena.LoadMap(dir)
	if err != nil {
		t.Fatal(err)
	}
	var cfg wire.Config
	err = json.Unmarshal([]byte(`{
		"match":{"tick_hz":20,"decide_every":2},
		"mech":{"radius":2.5,"height":9,"muzzle_height":7,"walk_speed":12,"ground_accel":60,"turn_rate":4,"pitch_rate":3,"min_pitch":-0.7,"max_pitch":1.3,"regen_delay":5},
		"npc":{"aggro_range":45},
		"loot":{"pickup_radius":4,"equip_s":1},
		"cubes":{"stat_cap":10},
		"weapons":{"gun":{"speed":250,"range":200,"mag":12,"cooldown":0.24},"lightning":{"range":45,"cone_deg":30,"energy":3}},
		"upgrades":[{"id":"plating","effects":[{"path":"mech.hp","mul":1.15}]}]
	}`), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	self := wire.Self{HP: 1000, MaxHP: 1000, Energy: 100, InZone: true, Grounded: true, Stats: map[string]float64{"hp": 1000}}
	self.Weapons[0] = &wire.Weapon{Kind: "gun", Ammo: 12}
	o := wire.Observation{Self: self, Tick: 40, T: 2, Zone: wire.Zone{Radius: 90, NextRadius: 90, ShrinkStart: 10, ShrinkEnd: 20}}
	return NewController(cfg, m), o
}

func TestZonePrecedesInRangeLoot(t *testing.T) {
	c, o := sample(t, false)
	o.Self.InZone = false
	o.Self.Pos[0] = 25
	o.Zone.Radius, o.Zone.NextRadius = 10, 8
	o.Pickups = []wire.Pickup{{ID: 8001, Kind: "lightning", Pos: o.Self.Pos}}
	action := c.Decide(o)
	if action.Take != nil || action.Move[0] >= 0 || c.target.kind != "zone" {
		t.Fatalf("expected an inward move, got %+v", action)
	}
}

func TestRecoveryWaitsWithoutFiring(t *testing.T) {
	c, o := sample(t, true)
	o.Self.HP = 300
	o.Mechs = []wire.Actor{{ID: 9, Pos: wire.Vec3{20, 0, 0}}}
	for _, hp := range []float64{300, 600} {
		o.Self.HP = hp
		o.T++
		a := c.Decide(o)
		if a.Fire != [2]bool{} || a.Move != (wire.Vec2{}) || !c.recovering {
			t.Fatalf("expected recovery at %v HP, got %+v", hp, a)
		}
	}
	o.Self.HP = 760
	c.Decide(o)
	if c.recovering {
		t.Fatal("recovery persisted after reaching the exit threshold")
	}
}

func TestGunUsesActualTorsoAndReloadState(t *testing.T) {
	c, o := sample(t, false)
	o.Mechs = []wire.Actor{{ID: 9, Pos: wire.Vec3{40, 0, 0}, Vel: wire.Vec3{0, 10, 0}}}
	o.Self.Aim[0] = math.Pi / 2
	turning := c.Decide(o)
	if turning.Fire[0] || turning.Aim[0] <= 0 || turning.Aim[0] >= .2 {
		t.Fatalf("expected positive lead without premature fire, got %+v", turning)
	}
	o.Self.Aim = turning.Aim
	if next := c.Decide(o); !next.Fire[0] {
		t.Fatalf("expected fire when aligned, got %+v", next)
	}
	o.Self.Weapons[0].Reload = 1
	if next := c.Decide(o); next.Fire[0] {
		t.Fatalf("fired while reloading: %+v", next)
	}
}

func TestCombatDoesNotTakeLoot(t *testing.T) {
	c, o := sample(t, false)
	o.Mechs = []wire.Actor{{ID: 8, Pos: wire.Vec3{30, 0, 0}}}
	o.Self.Aim[1] = math.Atan2(-2.5, 30)
	o.Pickups = []wire.Pickup{{ID: 8002, Kind: "lightning", Pos: o.Self.Pos}}
	a := c.Decide(o)
	if !a.Fire[0] || a.Take != nil {
		t.Fatalf("resolved combat and pickup inconsistently: %+v", a)
	}
}

func TestLightningUsesRangeConeAndEnergy(t *testing.T) {
	c, o := sample(t, false)
	o.Mechs = []wire.Actor{{ID: 8, Pos: wire.Vec3{20, 0, 0}}}
	o.Self.Weapons[1] = &wire.Weapon{Kind: "lightning"}
	if a := c.Decide(o); !a.Fire[1] || a.Fire[0] {
		t.Fatalf("expected aligned arc only: %+v", a)
	}
	o.Self.Energy = 2
	if a := c.Decide(o); a.Fire[1] {
		t.Fatalf("expected no arc at low energy: %+v", a)
	}
}

func TestPickupAndUpgradeUseOfficialFields(t *testing.T) {
	c, o := sample(t, false)
	o.Pickups = []wire.Pickup{{ID: 8010, Kind: "lightning", Pos: wire.Vec3{3, 0, 0}}}
	o.Offer = &wire.Offer{Choices: []wire.Choice{{ID: "unknown"}, {ID: "plating"}}}
	a := c.Decide(o)
	if a.Take == nil || *a.Take != 8010 || a.Pick == nil || *a.Pick != 1 {
		t.Fatalf("expected in-range loot and supplied upgrade effect, got %+v", a)
	}
	o.Pickups[0].Pos[0] = 5
	if a := c.Decide(o); a.Take != nil {
		t.Fatalf("sent out-of-range take: %+v", a)
	}
}
