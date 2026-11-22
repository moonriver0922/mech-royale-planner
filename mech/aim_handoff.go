package mech

import "math"

func (b *Brain) laserAimDue(me *Self, gun, laser *WeaponState, gunInRange bool, direct [2]float64) bool {
	legacy := laser.Charge > laser.Stats.Charge*0.45
	if !b.pracOn || !gunInRange || gun == nil || !(gun.Ammo > 0) || gun.Reload != 0 || laser.Charge <= 0 {
		return legacy
	}
	factor := laser.Stats.ChargeTurnMult
	if factor == 0 {
		factor = b.Cfg.Weapons["laser"].ChargeTurnMult
	}
	tickHz := b.Cfg.Match.TickHz
	yawRate, pitchRate := me.Stats.TurnRate*factor, me.Stats.PitchRate*factor
	for _, value := range [...]float64{factor, tickHz, laser.Stats.Charge, laser.Charge, yawRate, pitchRate} {
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return legacy
		}
	}
	if b.Cfg.Match.DecideEvery <= 0 {
		return legacy
	}
	yaw := direct[0] - me.Aim[0]
	pitch := direct[1] - me.Aim[1]
	if math.IsNaN(yaw) || math.IsInf(yaw, 0) || math.IsNaN(pitch) || math.IsInf(pitch, 0) {
		return legacy
	}
	decisionDT := float64(b.Cfg.Match.DecideEvery) / tickHz
	remaining := math.Max(0, laser.Stats.Charge-0.051-laser.Charge)
	align := math.Max(math.Abs(angDiff(direct[0], me.Aim[0]))/yawRate, math.Abs(pitch)/pitchRate)
	return remaining <= align+decisionDT
}
