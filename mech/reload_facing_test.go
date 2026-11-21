package mech

import (
	"math"
	"testing"
)

func TestReloadFacing(t *testing.T) {
	id := 2
	yaw := 1.4
	gun := &WeaponState{Kind: "gun", Reload: 1.5, Stats: WeaponStats{Reload: 2.4}}
	front := &Track{MechSeen: MechSeen{ID: 8}, HitMe: 12.3}
	attacked := Event{Type: "damaged", Rear: true, Weapon: "gun", By: &id, Bearing: &yaw}
	const original = -2.0
	for _, tc := range []struct {
		name      string
		reload    float64
		secondary *WeaponState
		event     Event
		wantGuard bool
	}{
		{"solo-reloading", 1.5, nil, attacked, true},
		{"almost-ready", 0.5, nil, attacked, false},
		{"secondary-available", 1.5, &WeaponState{Kind: "laser"}, attacked, false},
		{"incoming-front", 1.5, nil, Event{Type: "damaged", Weapon: "gun", By: &id, Bearing: &yaw}, false},
		{"blast-does-not-locate-shooter", 1.5, nil, Event{Type: "damaged", Rear: true, Weapon: "mortar", By: &id, Bearing: &yaw}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := Brain{}
			gun.Reload = tc.reload
			o := Obs{T: 13.1, Self: Self{Aim: [2]float64{original, 0}, Weapons: []*WeaponState{gun, tc.secondary}, Stats: MechStats{TurnRate: 4, FrontArcDeg: 180}}, Events: []Event{tc.event}}
			result := b.protectReload(&o, gun, front, original)
			if !tc.wantGuard {
				if result != original {
					t.Fatalf("unexpected turn: %v", result)
				}
				return
			}
			if math.Abs(angDiff(yaw, result)) >= math.Pi/2 || math.Abs(angDiff(original, result)) >= math.Pi/2 {
				t.Fatal("a confirmed bearing remains exposed")
			}
			o.T += 0.1
			o.Events = nil
			if b.protectReload(&o, gun, front, original) != result {
				t.Fatal("orientation did not persist")
			}
			o.T = 14.4
			gun.Reload = 0.2
			if b.protectReload(&o, gun, front, original) != original {
				t.Fatal("failed to reacquire before reload completion")
			}
		})
	}
}

func TestUnconfirmedCrossfire(t *testing.T) {
	id := 2
	bearing := 1.4
	o := Obs{T: 13.1, Self: Self{Aim: [2]float64{-2, 0}, Stats: MechStats{TurnRate: 4, FrontArcDeg: 180}},
		Events: []Event{{Type: "damaged", Weapon: "gun", Rear: true, By: &id, Bearing: &bearing}}}
	gun := &WeaponState{Kind: "gun", Reload: 1.5, Stats: WeaponStats{Reload: 2.4}}
	o.Self.Weapons = []*WeaponState{gun, nil}
	for _, front := range []*Track{nil, {MechSeen: MechSeen{ID: 8}, HitMe: 10}, {MechSeen: MechSeen{ID: 2}, HitMe: 12.3}} {
		b := Brain{}
		if b.protectReload(&o, gun, front, -2) != -2 {
			t.Fatal("turn without two distinct recently damaging opponents")
		}
	}
}
