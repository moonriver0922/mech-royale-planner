package mech

import "testing"

func TestRecoveryClockAndLaserRelease(t *testing.T) {
	var hello Hello
	brain := NewBrain(&hello, FlatWorld())
	brain.now = 10
	obs := &Obs{T: 10, Alive: 6, Self: Self{HP: 700, MaxHP: 1400, Grounded: true, InZone: true,
		Stats: MechStats{WalkSpeed: 11.7, GroundAccel: 58.5, Radius: 2.5, Height: 9, RegenDelay: 5, RegenPerS: 45}},
		Zone:   Zone{Radius: 500, NextRadius: 400, ShrinkStart: 20, ShrinkEnd: 50},
		Events: []Event{{Type: "hit", Amount: 45}},
	}
	brain.observe(obs)
	strategy := NewRecovery()
	original := Action{Fire: [2]bool{true, false}}
	if output := strategy.Apply(brain, obs, original); strategy.lastDamage != 10 || output.Fire != original.Fire {
		t.Fatal("a recent outgoing hit must reset the recovery clock and make this recovery window too short")
	}
	obs.T, obs.Events = 15, nil
	brain.now = 15
	if output := strategy.Apply(brain, obs, original); output.Fire[0] || strategy.intent.Kind != "recover" {
		t.Fatal("an eligible recovery must suppress an optional gun shot")
	}
	obs.Self.Weapons = []*WeaponState{nil, {Kind: "laser", Charge: 0.4}}
	original.Fire[1] = true
	if output := strategy.Apply(brain, obs, original); output.Fire != original.Fire || strategy.intent.Kind != "" {
		t.Fatal("a charged laser must retain the native trigger action")
	}
}
