package decision

import (
	"math"

	"ownbot/internal/wire"
)

const playerSlots = 16 // mechfight/1 player ID range

func playerID(id int) bool { return id >= 0 && id < playerSlots }

type pressureSample struct {
	at, amount float64
}

type pressureHistory struct {
	hits  [playerSlots][]pressureSample
	dead  [playerSlots]bool
	total [playerSlots]float64
}

func (h *pressureHistory) observe(o wire.Observation, window float64) {
	for _, e := range o.Events {
		switch e.Type {
		case "kill":
			if playerID(e.Target) {
				h.dead[e.Target] = true
				h.hits[e.Target] = nil
			}
		case "damaged":
			if e.By == nil || !playerID(*e.By) || *e.By == o.Self.ID || h.dead[*e.By] ||
				e.Weapon == "zone" || e.Weapon == "npc" || e.Amount <= 0 {
				continue
			}
			id := *e.By
			hits := h.hits[id]
			if n := len(hits); n > 0 && hits[n-1].at == o.T {
				hits[n-1].amount += e.Amount
				continue
			}
			// 128 observations per source exceed two official regen windows.
			if len(hits) == 128 {
				copy(hits, hits[1:])
				hits = hits[:127]
			}
			h.hits[id] = append(hits, pressureSample{o.T, e.Amount})
		}
	}
	for id, hits := range h.hits {
		sum := 0.
		expired := 0
		for _, hit := range hits {
			age := o.T - hit.at
			if age >= window || window <= 0 {
				expired++
				continue
			}
			sum += hit.amount * (1 - age/window)
		}
		if expired > 0 {
			h.hits[id] = hits[:copy(hits, hits[expired:])]
		}
		h.total[id] = sum
	}
}

func (c *Controller) preferPressureTie(a, b shot) bool {
	if c.target.kind == "enemy" && (a.id == c.target.id || b.id == c.target.id) {
		return a.id == c.target.id
	}
	return c.prefer(a, b)
}

type damageStream struct {
	next, interval, damage, reload float64
	rounds, mag, remaining         int
}

func (d *damageStream) advance() {
	d.remaining--
	d.next += d.interval
	if d.mag > 0 {
		d.rounds--
		if d.rounds == 0 {
			d.rounds = d.mag
			d.next += max(0, d.reload-d.interval)
		}
	}
}

func (c *Controller) nominalDamage(s wire.Self, w *wire.Weapon) float64 {
	scale := 1 + c.cfg.Cubes.DamagePerCube*min(float64(s.Cubes), c.cfg.Cubes.StatCap)
	return c.weapon(w, "damage") * scale
}

// Use a fixed-state projection of the two equipped weapons. Each event is an
// impact, so the gun's flight time is present even on the first aligned shot.
func (c *Controller) killTime(o wire.Observation, s shot) float64 {
	const maxHits = 128
	var streams [2]damageStream
	for slot, w := range o.Self.Weapons {
		if w == nil {
			continue
		}
		d := damageStream{damage: c.nominalDamage(o.Self, w), interval: c.weapon(w, "cooldown"), remaining: maxHits}
		if d.damage <= 0 || d.interval <= 0 {
			continue
		}
		aim := angles(sub(s.center, s.origin))
		start, flight := max(0, w.Cooldown), 0.
		switch w.Kind {
		case "gun":
			lead := sub(c.lead(w, s.center, s.origin, s.velocity), s.origin)
			aim = angles(lead)
			if !c.gunAligned(o, w, s, aim) || c.weapon(w, "speed") <= 0 {
				continue
			}
			flight = math.Sqrt(dot(lead, lead)) / c.weapon(w, "speed")
			d.mag, d.rounds, d.reload = int(c.weapon(w, "mag")), w.Ammo, c.weapon(w, "reload")
			if d.mag <= 0 {
				continue
			}
			if w.Reload > 0 {
				start, d.rounds = max(start, w.Reload), d.mag
			} else if d.rounds <= 0 {
				start, d.rounds = max(start, d.reload), d.mag
			}
		case "laser":
			if !c.laser.present || c.laser.pending || !c.laserRay(o, w, s, aim, 0) {
				continue
			}
			full := c.weapon(w, "charge")
			start = max(start, c.laser.shortUntil-o.T) + max(0, full-w.Charge)
			d.interval += full
		case "lightning":
			offset := sub(s.center, s.origin)
			if dot(offset, offset) > math.Pow(c.weapon(w, "range"), 2) || !c.world.ClearShot(s.origin, s.center) {
				continue
			}
			if cost := c.weapon(w, "energy"); cost > 0 {
				d.remaining = min(maxHits, int(o.Self.Energy/cost))
			}
			aim[1] = o.Self.Aim[1] // yaw-only cone
		default:
			continue
		}
		if w.Kind != "lightning" && (aim[1] < c.stat(o.Self, "min_pitch") || aim[1] > c.stat(o.Self, "max_pitch")) {
			continue
		}
		turn := max(math.Abs(wrap(aim[0]-o.Self.Aim[0]))/max(.01, c.stat(o.Self, "turn_rate")), math.Abs(aim[1]-o.Self.Aim[1])/max(.01, c.stat(o.Self, "pitch_rate")))
		d.next = max(start, turn) + flight
		streams[slot] = d
	}
	hp := s.hp
	if hp <= 0 {
		return math.Inf(1)
	}
	for range maxHits {
		next := -1
		for slot, d := range streams {
			if d.remaining > 0 && (next < 0 || d.next < streams[next].next) {
				next = slot
			}
		}
		if next < 0 {
			break
		}
		d := &streams[next]
		hp -= d.damage
		if hp <= 0 {
			return d.next
		}
		d.advance()
	}
	return math.Inf(1)
}

// Both aim samples must intersect the target before a live charge changes hands.
func (c *Controller) clearRelease(o wire.Observation, s shot) bool {
	w := o.Self.Weapons[c.laser.slot]
	aim := angles(sub(s.center, s.origin))
	tick := 1 / max(1, c.cfg.Match["tick_hz"])
	return w != nil && c.laser.available(o, w) && c.reachesLaserTarget(o, w, s, o.Self.Aim, 0) &&
		c.reachesLaserTarget(o, w, s, c.nextAim(o, aim), tick)
}

func (c *Controller) actorSize(p wire.Actor) (height, radius float64) {
	stats := c.cfg.Mech
	if p.Kind == "npc" {
		stats = c.cfg.NPC
	}
	scale := 1 + float64(p.Cubes)*c.cfg.Cubes.SizePerCube
	return stats["height"] * scale, stats["radius"] * scale
}

func (c *Controller) reachesLaserTarget(o wire.Observation, w *wire.Weapon, s shot, aim wire.Vec2, dt float64) bool {
	if !c.laserRay(o, w, s, aim, dt) {
		return false
	}
	dir := direction(aim)
	muzzle := add(add(s.origin, o.Self.Vel, dt), dir, c.stat(o.Self, "radius")+.5)
	contact, _ := cylinderEntry(muzzle, dir, add(s.pos, s.velocity, dt), s.radius, s.height, c.weapon(w, "range"))
	blockers := 0
	for _, p := range o.Mechs {
		if p.ID == s.id {
			continue
		}
		h, r := c.actorSize(p)
		if _, hit := cylinderEntry(muzzle, dir, add(p.Pos, p.Vel, dt), r, h, contact); hit {
			blockers++
		}
	}
	for _, crate := range o.Crates {
		if _, hit := cylinderEntry(muzzle, dir, crate.Pos, c.cfg.Crates["radius"], c.cfg.Crates["height"], contact); hit {
			blockers++
		}
	}
	return blockers <= int(c.weapon(w, "pierce"))
}

func (c *Controller) finishingVolley(o wire.Observation, s shot) bool {
	if s.hp <= 0 {
		return false
	}
	a := wire.Action{Aim: s.aim}
	_, urgent := c.zoneGoal(o)
	c.fire(o, s, &a, !urgent && !c.recovering)
	damage := 0.
	for i, w := range o.Self.Weapons {
		if w == nil {
			continue
		}
		switch w.Kind {
		case "gun":
			if a.Fire[i] && c.directGun(o, w, s) {
				damage += c.nominalDamage(o.Self, w)
			}
		case "lightning":
			if a.Fire[i] && c.directArc(o, w, s) {
				damage += c.nominalDamage(o.Self, w)
			}
		case "laser":
			if !urgent && !c.recovering && c.laser.held && !a.Fire[i] && w.Charge >= c.weapon(w, "min_charge") && c.clearRelease(o, s) {
				full := max(.001, c.weapon(w, "charge"))
				fraction := c.weapon(w, "min_frac")
				damage += c.nominalDamage(o.Self, w) * (fraction + (1-fraction)*min(1, w.Charge/full))
			}
		}
	}
	return damage >= s.hp
}

// Credit the nominal gun ray only up to its first contact with the target.
// The projectile travels from the current muzzle along the observed aim.
// Actors keep their observed velocity during flight; crates are stationary.
func (c *Controller) directGun(o wire.Observation, w *wire.Weapon, s shot) bool {
	speed := c.weapon(w, "speed")
	if speed <= 0 {
		return false
	}
	dir := direction(o.Self.Aim)
	muzzle := add(s.origin, dir, c.stat(o.Self, "radius")+.5)
	bullet := c.cfg.Projectile["gun_radius"]
	entry := func(feet, velocity wire.Vec3, radius, height, length float64) (float64, bool) {
		// Parameterize by bullet distance. In the body's frame its path is
		// dir − bodyVelocity/speed per meter. Expand the cylinder by the
		// projectile radius, conservatively including its top/bottom rims.
		relative := add(dir, velocity, -1/speed)
		feet[2] -= bullet
		return cylinderEntry(muzzle, relative, feet, radius+bullet, height+2*bullet, length)
	}
	contact, hit := entry(s.pos, s.velocity, s.radius, s.height, c.weapon(w, "range"))
	if !hit {
		return false
	}
	for _, p := range o.Mechs {
		if p.ID == s.id {
			continue
		}
		height, radius := c.actorSize(p)
		if _, blocks := entry(p.Pos, p.Vel, radius, height, contact); blocks {
			return false
		}
	}
	for _, crate := range o.Crates {
		if _, blocks := entry(crate.Pos, wire.Vec3{}, c.cfg.Crates["radius"], c.cfg.Crates["height"], contact); blocks {
			return false
		}
	}
	return true
}

// The arc picks the closest clear actor or crate inside the current cone.
// Count a finishing direct hit only when no closer candidate can take it.
func (c *Controller) directArc(o wire.Observation, w *wire.Weapon, s shot) bool {
	target := sub(s.center, s.origin)
	closer := func(pos wire.Vec3, height float64) bool {
		pos[2] += height / 2
		v := sub(pos, s.origin)
		return dot(v, v) <= dot(target, target) && math.Abs(wrap(angles(v)[0]-o.Self.Aim[0])) <= c.weapon(w, "cone_deg")*math.Pi/360 && c.world.ClearShot(s.origin, pos)
	}
	for _, p := range o.Mechs {
		if p.ID == s.id {
			continue
		}
		height, _ := c.actorSize(p)
		if closer(p.Pos, height) {
			return false
		}
	}
	for _, crate := range o.Crates {
		if closer(crate.Pos, c.cfg.Crates["height"]) {
			return false
		}
	}
	return true
}
