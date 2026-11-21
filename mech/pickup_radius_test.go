package mech

import "testing"

func TestTakeAtPickupBoundary(t *testing.T) {
	w := FlatWorld()
	h := Hello{}
	h.Config.Loot.PickupRadius = 4
	h.Config.Mech = MechStats{WalkSpeed: 10, Radius: 2.5, Height: 9, EyeHeight: 8, MuzzleHeight: 7}
	for _, tc := range []struct {
		name           string
		pos            Vec3
		occupied, take bool
	}{
		{"empty slot at boundary", Vec3{4, 0, 4}, false, true},
		{"outside horizontal radius", Vec3{4.01, 0, 0}, false, false},
		{"outside height limit", Vec3{0, 0, 4.01}, false, false},
		{"no swap at extended radius", Vec3{3.9, 0, 0}, true, false},
		{"normal swap", Vec3{3, 0, 0}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBrain(&h, w)
			me := Self{Grounded: true, InZone: true, HP: 1400, MaxHP: 1400, Energy: 100, MaxEnergy: 100,
				Weapons: []*WeaponState{{Kind: "gun", Ammo: 12}, nil}}
			if tc.occupied {
				me.Weapons[1] = &WeaponState{Kind: "mortar"}
			}
			obs := Obs{Alive: 16, Self: me, Zone: Zone{Radius: 300, NextRadius: 250, ShrinkStart: 120, ShrinkEnd: 200},
				Pickups: []Item{{ID: 8001, Kind: "laser", Pos: tc.pos}}}
			act := b.Act(&obs)
			if b.Panics != 0 || (act.Take != nil) != tc.take || (act.Take != nil && *act.Take != 8001) ||
				(act.Drop != nil) != (tc.take && tc.occupied) {
				t.Fatalf("unexpected pickup: take=%v drop=%v panics=%d", act.Take, act.Drop, b.Panics)
			}
		})
	}
}
