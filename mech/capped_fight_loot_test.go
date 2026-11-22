package mech

import "testing"

func TestCappedFightCube(t *testing.T) {
	var h Hello
	h.Config.Cubes.StatCap = 10
	h.Config.Cubes.UpgradeThresholds = []int{21, 10, 15}
	b := NewBrain(&h, FlatWorld())
	b.pracOn = true
	o := &Obs{Self: Self{Cubes: 14, Stats: MechStats{Height: 9}},
		Zone: Zone{Radius: 300, NextRadius: 250}}
	c := &known{Pos: Vec3{10, 0, 0}}
	if !b.allowCappedFightCube(o, c, nil) {
		t.Fatal("14 to 15 must be eligible")
	}
	o.Self.Cubes = 20
	if !b.allowCappedFightCube(o, c, nil) {
		t.Fatal("20 to 21 must be eligible")
	}
	o.Self.Cubes = 15
	if b.allowCappedFightCube(o, c, nil) {
		t.Fatal("must reach the next upgrade")
	}
	o.Self.Cubes = 9
	c.Pos = Vec3{25, 0, 5}
	if !b.allowCappedFightCube(o, c, nil) {
		t.Fatal("below cap must keep the baseline")
	}
	o.Self.Cubes = 14
	if b.allowCappedFightCube(o, c, nil) {
		t.Fatal("distance must limit the override")
	}
	c.Pos = Vec3{10, 0, 4}
	if b.allowCappedFightCube(o, c, nil) {
		t.Fatal("invalid height must limit the override")
	}
	c.Pos = Vec3{10, 0, 0}
	cell := b.W.Cell(5, 0)
	b.W.Blocked[cell] = true
	if b.allowCappedFightCube(o, c, nil) {
		t.Fatal("blocked route must be rejected")
	}
	b.W.Blocked[cell] = false
	o.Zone = Zone{Radius: 12, NextRadius: 12}
	if b.allowCappedFightCube(o, c, nil) {
		t.Fatal("worse circle penalty must reject")
	}
	b.pracOn = false
	if !b.allowCappedFightCube(o, c, nil) {
		t.Fatal("outside Practice must keep the baseline")
	}
}

func TestCappedFightLootKeepsMovementAndSkipsIneligibleNearest(t *testing.T) {
	var h Hello
	h.Config.Mech = MechStats{Radius: 2.5, Height: 9, MuzzleHeight: 7, EyeHeight: 8, WalkSpeed: 12}
	h.Config.Cubes.StatCap = 10
	h.Config.Cubes.UpgradeThresholds = []int{10, 15, 21}
	w := FlatWorld()
	testBrain := func() *Brain {
		b := NewBrain(&h, w)
		b.base, b.baseSet, b.pracOn = b.P, true, true
		b.P = squadParams(b.P)
		return b
	}
	o := Obs{T: 25, Alive: 16, Self: Self{Cubes: 13, HP: 1400, MaxHP: 1400, Grounded: true, InZone: true,
		Vel: Vec3{2, 0, 0}, Stats: h.Config.Mech},
		Zone:  Zone{Radius: 300, NextRadius: 250, ShrinkStart: 100, ShrinkEnd: 150},
		Mechs: []MechSeen{{Kind: "mech", ID: 1, Pos: Vec3{50, 0, 0}, HP: 1400, MaxHP: 1400}},
		Cubes: []Item{{ID: 1, Pos: Vec3{0, 45, 0}}}}
	withLoot := testBrain().Act(&o)
	without := testBrain()
	without.P.FightLoot = 0
	want := without.Act(&o)
	if withLoot.Move != want.Move || withLoot.Boost != want.Boost || without.Mode != "fight" {
		t.Fatalf("rejected override changed the intended fight action: %v / %v", withLoot, want)
	}
	b := testBrain()
	b.cubes[1] = &known{Pos: Vec3{3, 0, 5}}
	b.cubes[2] = &known{Pos: Vec3{0, 8, 0}}
	o.Self.Cubes = 14
	if got := b.fightLootCube(&o, nil, 70); got == nil || *got != b.cubes[2].Pos {
		t.Fatalf("expected the eligible farther cube, got %v", got)
	}
	wall := Prism{Kind: "building", MinX: 18, MaxX: 20, MinY: -2, MaxY: 2, Top: 14,
		Edges: []float64{18, -2, 20, -2, 20, -2, 20, 2, 20, 2, 18, 2, 18, 2, 18, -2}}
	w.initGeo([]Prism{wall}, nil, Rules{})
	seen := []*Track{{MechSeen: MechSeen{Kind: "mech", Pos: Vec3{40, 0, 0}}, Seen: o.T}}
	if b.allowCappedFightCube(&o, b.cubes[2], seen) {
		t.Fatal("new exposure along the route must reject a loot override")
	}
}
