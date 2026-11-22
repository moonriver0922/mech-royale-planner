package decision

import (
	"math"
	"slices"

	"ownbot/internal/wire"
)

// The official protocol assigns IDs 0–15 to player mechs and 100+ to NPCs.
const playerSlots = 16

type incomingBin struct {
	time   float64
	tick   int
	amount [playerSlots]float64
}

type crossfireState struct {
	history                 []incomingBin
	next, count             int
	active, hasUsedEvidence bool
	dest                    wire.Vec3
	deadline                float64
	evidenceTick            int
	requiredLaser           int // -1 when damage alone justified the race
}

func (c *Controller) recordIncoming(o wire.Observation) {
	window := c.cfg.Weapons["gun"]["reload"]
	if window <= 0 || c.period <= 0 {
		return
	}
	b := &c.crossfire
	// Drop the open left boundary. The small tolerance absorbs the subtraction
	// error of decimal seconds in the official observation timestamps.
	for b.count > 0 {
		oldest := (b.next + len(b.history) - b.count) % len(b.history)
		if b.history[oldest].time > o.T-window+1e-9 {
			break
		}
		b.count--
	}
	var current incomingBin
	for _, e := range o.Events {
		if e.Type != "damaged" || e.By == nil || e.Amount <= 0 {
			continue
		}
		id := *e.By
		if id < 0 || id >= playerSlots || id == o.Self.ID {
			continue
		}
		current.amount[id] += e.Amount
	}
	if current.amount == ([playerSlots]float64{}) {
		return
	}
	if len(b.history) == 0 {
		b.history = make([]incomingBin, max(1, int(math.Ceil(window/c.period))))
	}
	current.time, current.tick = o.T, o.Tick
	b.history[b.next] = current
	b.next = (b.next + 1) % len(b.history)
	b.count = min(len(b.history), b.count+1)
}

// Return the time of the most recent hit from this source along with its
// total. A newly seen unrelated shooter cannot re-arm an expended escape.
func (b *crossfireState) damageBy(id int) (damage float64, tick int) {
	for i := 0; i < b.count; i++ {
		bin := &b.history[(b.next+len(b.history)-1-i)%len(b.history)]
		if d := bin.amount[id]; d > 0 {
			damage += d
			tick = max(tick, bin.tick)
		}
	}
	return
}

func (c *Controller) scaledDamage(cubes int) float64 {
	return 1 + c.cfg.Cubes.DamagePerCube*min(float64(cubes), c.cfg.Cubes.StatCap)
}

// Magazine transitions can be counted directly even when target HP is huge.
// The optimistic bound gives every bullet a rear hit and ignores enemy armor.
func (c *Controller) gunFinish(o wire.Observation, enemy shot) (float64, bool) {
	s := o.Self
	w := s.Weapons[0]
	damage := c.weapon(w, "damage") * c.scaledDamage(s.Cubes) * c.cfg.Mech["rear_mult"]
	mag, speed := math.Floor(c.weapon(w, "mag")), c.weapon(w, "speed")
	if damage <= 0 || mag < 1 || speed <= 0 {
		return 0, false
	}
	rounds := math.Ceil(enemy.hp / damage)
	if rounds <= 1 {
		return 0, false
	}
	first, ammo := w.Cooldown, float64(w.Ammo)
	reload, cooldown := c.weapon(w, "reload"), c.weapon(w, "cooldown")
	if w.Reload > 0 {
		first, ammo = max(first, w.Reload), mag
	} else if ammo <= 0 {
		first, ammo = max(first, reload), mag
	}
	changes := math.Ceil(max(0, rounds-ammo) / mag)
	flight := max(0, enemy.distance-enemy.radius-c.stat(s, "radius")-.5) / speed
	return first + (rounds-1-changes)*cooldown + changes*max(cooldown, reload) + flight, true
}

func (c *Controller) actorMuzzle(m wire.Actor) wire.Vec3 {
	src := m.Pos
	src[2] += c.cfg.Mech["muzzle_height"] * (1 + float64(m.Cubes)*c.cfg.Cubes.SizePerCube)
	return src
}

func (c *Controller) bodyCenter(s wire.Self, p wire.Vec3) wire.Vec3 {
	p[2] += c.stat(s, "height") / 2
	return p
}

// Test three orientations along a legal charging slew toward our current
// cylinder. The public observation does not reveal how long the charge ran.
func (c *Controller) chargingBeam(o wire.Observation, m wire.Actor, src, center wire.Vec3) bool {
	if !m.Charging || !slices.Contains(m.Weapons, "laser") {
		return false
	}
	laser := c.cfg.Weapons["laser"]
	radius := c.cfg.Mech["radius"] * (1 + float64(m.Cubes)*c.cfg.Cubes.SizePerCube)
	want := angles(sub(center, src))
	want[1] = max(c.cfg.Mech["min_pitch"], min(c.cfg.Mech["max_pitch"], want[1]))
	for _, dt := range [3]float64{0, 1 / max(1, c.cfg.Match["tick_hz"]), c.period} {
		yaw := dt * c.cfg.Mech["turn_rate"] * laser["charge_turn_mult"]
		pitch := dt * c.cfg.Mech["pitch_rate"] * laser["charge_turn_mult"]
		aim := wire.Vec2{m.Aim[0] + max(-yaw, min(yaw, wrap(want[0]-m.Aim[0]))), m.Aim[1] + max(-pitch, min(pitch, want[1]-m.Aim[1]))}
		dir := direction(aim)
		muzzle := add(src, dir, radius+.5)
		entry, ok := cylinderEntry(muzzle, dir, o.Self.Pos, c.stat(o.Self, "radius"), c.stat(o.Self, "height"), laser["range"])
		if ok && c.world.ClearShot(src, muzzle) && c.world.ClearShot(muzzle, add(muzzle, dir, entry)) {
			return true
		}
	}
	return false
}

type fireLane struct {
	source             int
	muzzle             wire.Vec3
	confirmed, blocked bool
}

type crossfireRisk struct {
	lanes           []fireLane
	evidence, laser int
	needsLaser      bool
}

func (c *Controller) incomingRace(o wire.Observation, finish float64) (crossfireRisk, bool) {
	b := &c.crossfire
	risk := crossfireRisk{laser: -1}
	center := c.bodyCenter(o.Self, o.Self.Pos)
	// First count distinct visible IDs with fresh hits. The frequent single
	// opponent case needs no map rays, beam intersections or allocations.
	var recent [playerSlots]float64
	var ticks [playerSlots]int
	var candidates uint16
	for _, m := range o.Mechs {
		if m.Kind != "mech" || m.ID < 0 || m.ID >= playerSlots || m.ID == o.Self.ID {
			continue
		}
		recent[m.ID], ticks[m.ID] = b.damageBy(m.ID)
		if recent[m.ID] > 0 {
			candidates |= 1 << m.ID
		}
	}
	if candidates&(candidates-1) == 0 {
		return risk, false
	}
	var checked uint16
	var confirmed int
	var total, burst float64
	for _, m := range o.Mechs {
		if m.Kind != "mech" || m.ID < 0 || m.ID >= playerSlots {
			continue
		}
		bit := uint16(1) << m.ID
		if candidates&bit == 0 || checked&bit != 0 {
			continue
		}
		checked |= bit
		src := c.actorMuzzle(m)
		if !c.world.ClearShot(src, center) {
			risk.lanes = append(risk.lanes, fireLane{source: m.ID, muzzle: src, blocked: true})
			continue
		}
		confirmed++
		risk.lanes = append(risk.lanes, fireLane{source: m.ID, muzzle: src, confirmed: true})
		risk.evidence = max(risk.evidence, ticks[m.ID])
		total += recent[m.ID]
		if c.chargingBeam(o, m, src, center) {
			d := c.cfg.Weapons["laser"]["damage"] * c.scaledDamage(m.Cubes) * (1 - c.stat(o.Self, "armor"))
			if d > burst || (d == burst && d > 0 && m.ID < risk.laser) {
				risk.laser, burst = m.ID, d
			}
		}
	}
	if confirmed < 2 || (b.hasUsedEvidence && risk.evidence <= b.evidenceTick) {
		return risk, false
	}
	rate := total / c.cfg.Weapons["gun"]["reload"]
	losing := func(beam float64) bool {
		in := max(0, o.Self.HP-beam) / rate
		return in <= 2*c.cfg.Mech["regen_delay"] && finish > in+c.period
	}
	if !losing(burst) {
		return risk, false
	}
	risk.needsLaser = !losing(0)
	// Only currently blocked non-shooters can introduce a new exposure.
	for _, m := range o.Mechs {
		if m.Kind != "mech" || m.ID < 0 || m.ID >= playerSlots || m.ID == o.Self.ID || checked&(1<<m.ID) != 0 {
			continue
		}
		checked |= 1 << m.ID
		src := c.actorMuzzle(m)
		if !c.world.ClearShot(src, center) {
			risk.lanes = append(risk.lanes, fireLane{source: m.ID, muzzle: src, blocked: true})
		}
	}
	return risk, true
}

// Endpoint visibility says nothing about exposure during the intervening walk.
func (c *Controller) cutsAt(center wire.Vec3, risk crossfireRisk) (count int, laser bool) {
	for _, lane := range risk.lanes {
		clear := c.world.ClearShot(lane.muzzle, center)
		if lane.blocked && clear {
			return 0, false
		}
		if lane.confirmed && !clear {
			count++
			laser = laser || lane.source == risk.laser
		}
	}
	return
}

func (c *Controller) breakSteer(o wire.Observation, dest wire.Vec3) wire.Vec2 {
	move := towards(o.Self.Pos, dest)
	scale := min(1, distance(o.Self.Pos, dest)/max(1, 2*c.period*c.stat(o.Self, "walk_speed")))
	return wire.Vec2{move[0] * scale, move[1] * scale}
}

func (c *Controller) breakShotClear(o wire.Observation, a wire.Action, enemy shot, move wire.Vec2) bool {
	if !a.Fire[0] {
		return true
	}
	body := bodyMotion{o.Self.Pos, wire.Vec2{o.Self.Vel[0], o.Self.Vel[1]}}
	return c.heldShotsClear(o, a, enemy, body, move, c.growthWalk(o, a))
}

func (c *Controller) selectBreak(o wire.Observation, a wire.Action, enemy shot, risk crossfireRisk) (dest wire.Vec3, eta float64, ok bool) {
	bestCount, bestLaser := 0, false
	for _, length := range [2]float64{12, 24} {
		travel := c.travel(o, length)
		for i := 0; i < 8; i++ {
			yaw := o.Self.Aim[0] + float64(i)*math.Pi/4
			p := c.onGround(o.Self.Pos[0]+length*math.Cos(yaw), o.Self.Pos[1]+length*math.Sin(yaw))
			if margin(o.Zone, p, o.T, o.T+travel) < 0 || !c.walk(o.Self, o.Self.Pos, p) {
				continue
			}
			count, laser := c.cutsAt(c.bodyCenter(o.Self, p), risk)
			if count == 0 || (risk.needsLaser && !laser) || (bestLaser && !laser) || (bestLaser == laser && count <= bestCount) {
				continue
			}
			if !c.breakShotClear(o, a, enemy, c.breakSteer(o, p)) {
				continue
			}
			dest, eta, ok = p, travel, true
			bestCount, bestLaser = count, laser
		}
	}
	return
}

func (c *Controller) continueBreak(o wire.Observation, a wire.Action, enemy shot) (wire.Vec2, bool) {
	b := &c.crossfire
	length := distance(o.Self.Pos, b.dest)
	if length <= 1 || o.T >= b.deadline || margin(o.Zone, b.dest, o.T, o.T+c.travel(o, length)) < 0 || !c.walk(o.Self, o.Self.Pos, b.dest) {
		return wire.Vec2{}, false
	}
	if b.requiredLaser >= 0 {
		for _, m := range o.Mechs {
			if m.Kind == "mech" && m.ID == b.requiredLaser && c.world.ClearShot(c.actorMuzzle(m), c.bodyCenter(o.Self, b.dest)) {
				return wire.Vec2{}, false
			}
		}
	}
	move := c.breakSteer(o, b.dest)
	return move, c.breakShotClear(o, a, enemy, move)
}

func (c *Controller) crossfireMove(o wire.Observation, enemy shot, engaging, scouting bool, a *wire.Action) {
	b := &c.crossfire
	if !b.active && (b.count == 0 || !engaging) {
		return
	}
	s := o.Self
	if (!engaging && !scouting) || !s.Grounded || !s.InZone || c.recovering || s.HP < .45*s.MaxHP ||
		s.Weapons[0] == nil || s.Weapons[0].Kind != "gun" || s.Weapons[1] != nil || a.Take != nil {
		b.active = false
		return
	}
	var finish float64
	if engaging {
		player := false
		for _, m := range o.Mechs {
			if m.ID == enemy.id && m.Kind == "mech" && m.ID != s.ID {
				player = true
				break
			}
		}
		var multiRound bool
		finish, multiRound = c.gunFinish(o, enemy)
		if !player || enemy.hp <= 0 || !multiRound {
			b.active = false
			return
		}
	}
	if b.active {
		move, ok := c.continueBreak(o, *a, enemy)
		b.active = ok
		if ok {
			a.Move = move
		}
		return
	}
	risk, losing := c.incomingRace(o, finish)
	if !losing {
		return
	}
	dest, eta, ok := c.selectBreak(o, *a, enemy, risk)
	if !ok {
		return
	}
	b.active, b.hasUsedEvidence = true, true
	b.dest, b.deadline, b.evidenceTick = dest, o.T+eta+2*c.period, risk.evidence
	b.requiredLaser = -1
	if risk.needsLaser {
		b.requiredLaser = risk.laser
	}
	a.Move = c.breakSteer(o, dest)
}
