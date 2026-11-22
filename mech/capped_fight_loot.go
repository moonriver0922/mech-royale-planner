package mech

import "math"

const cappedCubeMaxDist = 12.0

func finiteFightLoot(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func (b *Brain) cappedFightLootActive(o *Obs) bool {
	return b.pracOn && b.Cfg.Cubes.StatCap > 0 && o.Self.Cubes >= b.Cfg.Cubes.StatCap &&
		len(b.Cfg.Cubes.UpgradeThresholds) > 0
}

func (b *Brain) fightLootCube(o *Obs, seen []*Track, radius float64) *Vec3 {
	active := b.cappedFightLootActive(o)
	var best *Vec3
	var bestID int
	for id, cube := range b.cubes {
		d := hyp(cube.Pos[0]-o.Self.Pos[0], cube.Pos[1]-o.Self.Pos[1])
		if (d < radius || active && best != nil && d == radius && id < bestID) &&
			b.allowCappedFightCube(o, cube, seen) {
			point := cube.Pos
			best, bestID, radius = &point, id, d
		}
	}
	return best
}

func (b *Brain) allowCappedFightCube(o *Obs, cube *known, seen []*Track) bool {
	if !b.cappedFightLootActive(o) {
		return true
	}
	me := &o.Self
	next := 0
	for _, threshold := range b.Cfg.Cubes.UpgradeThresholds {
		if threshold <= 0 {
			return true // Corrupt or incomplete upgrade configuration.
		}
		if threshold > me.Cubes && (next == 0 || threshold < next) {
			next = threshold
		}
	}
	if next == 0 || next-1 != me.Cubes {
		return false
	}

	start, end := me.Pos, cube.Pos
	for axis := range start {
		if !finiteFightLoot(start[axis]) || !finiteFightLoot(end[axis]) {
			return false
		}
	}
	if hyp(end[0]-start[0], end[1]-start[1]) > cappedCubeMaxDist ||
		math.Abs(end[2]-start[2]) >= 4 ||
		!b.W.LineWalkable(start[0], start[1], end[0], end[1]) {
		return false
	}
	mid := Vec3{(start[0] + end[0]) * 0.5, (start[1] + end[1]) * 0.5, (start[2] + end[2]) * 0.5}
	startPenalty := b.zonePenalty(&o.Zone, start[0], start[1])
	if b.zonePenalty(&o.Zone, mid[0], mid[1]) > startPenalty ||
		b.zonePenalty(&o.Zone, end[0], end[1]) > startPenalty {
		return false
	}
	for _, foe := range seen {
		if foe.Seen != o.T {
			continue
		}
		muzzle := foe.Pos
		muzzle[2] += b.Cfg.Mech.MuzzleHeight * b.scaleOf(foe.Cubes)
		exposed := func(p Vec3) bool {
			return b.W.Clear(muzzle, Vec3{p[0], p[1], p[2] + me.Stats.Height*0.5}) ||
				b.W.Clear(muzzle, Vec3{p[0], p[1], p[2] + me.Stats.Height*0.85})
		}
		if !exposed(start) && (exposed(mid) || exposed(end)) {
			return false
		}
	}
	return true
}
