package mech

import (
	"math"
	"testing"
)

func TestLaserAimHandoff(t *testing.T) {
	var b Brain
	b.Cfg.Match.TickHz, b.Cfg.Match.DecideEvery = 20, 2
	b.Cfg.Weapons = map[string]WeaponStats{"laser": {ChargeTurnMult: 1}}
	b.pracOn = true
	gun := &WeaponState{Kind: "gun", Ammo: 4}
	me := &Self{Stats: MechStats{TurnRate: 4, PitchRate: 3}}
	laser := &WeaponState{Kind: "laser", Charge: .4, Stats: WeaponStats{Charge: .75}}
	if b.laserAimDue(me, gun, laser, true, [2]float64{.1, 0}) {
		t.Fatal("retain the gun aim while plenty of time remains")
	}
	if !b.laserAimDue(me, gun, laser, true, [2]float64{1.2, 0}) {
		t.Fatal("a new distant target requires an early handoff")
	}
	me.Aim = [2]float64{math.Pi - .03, 0}
	if b.laserAimDue(me, gun, laser, true, [2]float64{-math.Pi + .03, 0}) {
		t.Fatal("yaw wraps at pi")
	}
	b.Cfg.Match.DecideEvery = 6
	if !b.laserAimDue(me, gun, laser, true, [2]float64{-math.Pi + .03, 0}) {
		t.Fatal("must read decision interval")
	}
	b.Cfg.Match.DecideEvery = 2
	if !b.laserAimDue(me, nil, laser, true, me.Aim) {
		t.Fatal("no gun must fall back")
	}
	gun.Ammo = 0
	if !b.laserAimDue(me, gun, laser, true, me.Aim) {
		t.Fatal("empty gun must fall back")
	}
	gun.Ammo, gun.Reload = 4, 1
	if !b.laserAimDue(me, gun, laser, true, me.Aim) {
		t.Fatal("reloading gun must fall back")
	}
	gun.Reload = 0
	b.Cfg.Weapons = nil
	if !b.laserAimDue(me, gun, laser, true, me.Aim) {
		t.Fatal("missing factor must fall back")
	}
}
