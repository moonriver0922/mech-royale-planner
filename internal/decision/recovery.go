package decision

import (
	"math"

	"ownbot/internal/wire"
)

const shelterTravelLimit = 2.0 // seconds

type incomingHit struct {
	at, amount float64
	attacker   int
}

func (c *Controller) trimDamage(cutoff float64) {
	for len(c.damage) > 0 && c.damage[0].at < cutoff {
		c.damage = c.damage[1:]
	}
}

// The first hit marks the start of observation. Count only later damage
// to avoid charging the initial impulse to an interval preceding its arrival.
func (c *Controller) incomingRate(now float64) float64 {
	if len(c.damage) < 2 {
		return 0
	}
	start := c.damage[0].at
	if c.damage[len(c.damage)-1].at-start < .5 {
		return 0
	}
	sum := 0.
	for _, hit := range c.damage {
		if hit.at > start {
			sum += hit.amount
		}
	}
	return sum / (now - start)
}

// A recent hit proves the attacker could reach us. Retain that evidence
// even beyond nominal weapon range, and reject plans against missing actors.
func (c *Controller) recentAttackers(o wire.Observation) (map[int]bool, bool) {
	if len(c.damage) == 0 {
		return nil, true
	}
	seen := make(map[int]bool)
	for _, hit := range c.damage {
		seen[hit.attacker] = false
	}
	for _, m := range o.Mechs {
		if _, tracked := seen[m.ID]; tracked {
			seen[m.ID] = true
		}
	}
	for _, visible := range seen {
		if !visible {
			return seen, false
		}
	}
	return seen, true
}

func (c *Controller) recover(o wire.Observation) (wire.Vec2, bool) {
	s := o.Self
	if !s.InZone || (!c.recovering && s.HP >= .45*s.MaxHP) {
		return wire.Vec2{}, false
	}
	recent, accounted := c.recentAttackers(o)
	if !accounted {
		c.coverValid = false
		return wire.Vec2{}, false
	}

	if margin(o.Zone, s.Pos, o.T, o.T+c.period) > 0 && !c.exposed(o, recent, s.Pos, 0) {
		// A projectile can outlive the agent that fired it. Let ordinary
		// movement continue if an observed shot is about to cross our body.
		if c.incomingProjectile(o) {
			c.coverValid = false
			return wire.Vec2{}, false
		}
		c.cover, c.coverValid, c.recovering = s.Pos, true, true
		return wire.Vec2{}, true
	}

	rate := c.incomingRate(o.T)
	credible := func(p wire.Vec3) bool {
		length := distance(s.Pos, p)
		if length < .01 {
			return false
		}
		eta := c.travel(o, length)
		return eta <= shelterTravelLimit && rate*eta < s.HP &&
			margin(o.Zone, p, o.T, o.T+eta) > 0 &&
			c.walk(s, p, p) && c.walk(s, s.Pos, p) && !c.exposed(o, recent, p, eta)
	}
	if !c.coverValid || !credible(c.cover) {
		c.coverValid = false
		speed := c.stat(s, "walk_speed")
		// Match the acceleration allowance used by travel(). Shorter rings
		// are preferred. Charging, if present, is still checked in the ETA.
		reach := speed * max(0, shelterTravelLimit-speed/max(1, c.stat(s, "ground_accel")))
	search:
		for ring := 1; ring <= 4; ring++ {
			length := reach * float64(ring) / 4
			for i := range 8 {
				a := s.Aim[0] + float64(i)*math.Pi/4
				p := c.onGround(s.Pos[0]+length*math.Cos(a), s.Pos[1]+length*math.Sin(a))
				if credible(p) {
					c.cover, c.coverValid = p, true
					break search
				}
			}
		}
	}
	if !c.coverValid {
		return wire.Vec2{}, false
	}
	// The exact segment has just passed the walking check. Reusing a
	// cached route for a different cover point could take a stale first leg.
	c.recovering = true
	c.target, c.destination, c.route = goal{kind: "cover"}, c.cover, nil
	dx, dy := c.cover[0]-s.Pos[0], c.cover[1]-s.Pos[1]
	scale := max(math.Hypot(dx, dy), 2*c.period*c.stat(s, "walk_speed"), 1)
	return wire.Vec2{dx / scale, dy / scale}, true
}

func (c *Controller) exposed(o wire.Observation, recent map[int]bool, p wire.Vec3, eta float64) bool {
	for _, m := range o.Mechs {
		if c.actorExposes(o.Self, m, p, eta, recent[m.ID]) {
			return true
		}
	}
	return false
}

func (c *Controller) actorExposes(self wire.Self, m wire.Actor, p wire.Vec3, eta float64, recentHit bool) bool {
	d := distance(m.Pos, p)
	muzzle := m.Pos
	if m.Kind == "npc" {
		if !recentHit && d > c.cfg.NPC["aggro_range"] {
			return false
		}
		muzzle[2] += c.cfg.NPC["height"] / 2
		return c.visibleBody(self, muzzle, p)
	}

	scale := 1 + float64(m.Cubes)*c.cfg.Cubes.SizePerCube
	// Reach includes forward muzzle and body radii. Enemy upgrades and
	// energy are unobserved. Use nominal boost speed for the approach bound.
	extension := c.cfg.Mech["radius"]*scale + .5 + c.stat(self, "radius") +
		max(c.cfg.Mech["walk_speed"], c.cfg.Mech["boost_speed"], math.Hypot(m.Vel[0], m.Vel[1]))*eta
	direct := recentHit
	for _, kind := range m.Weapons {
		w := c.cfg.Weapons[kind]
		reach := w["range"]
		if kind == "mortar" && w["gravity"] > 0 {
			reach = w["speed"]*w["speed"]/w["gravity"] + w["splash_radius"]
		}
		if reach > 0 && (recentHit || d <= reach+extension) {
			if kind == "mortar" {
				// A clear trajectory over the obstacle cannot be ruled out by a direct ray.
				return true
			}
			direct = true
		}
	}
	if !direct {
		return false
	}
	muzzle[2] = m.Pos[2] + c.cfg.Mech["muzzle_height"]*scale
	return c.visibleBody(self, muzzle, p)
}

// Check the midpoint first, then both horizontal silhouette edges and upper
// and lower body. This catches partly exposed sides, heads and bridge gaps.
func (c *Controller) visibleBody(s wire.Self, muzzle, feet wire.Vec3) bool {
	h, r := c.stat(s, "height"), c.stat(s, "radius")
	d := max(.001, distance(muzzle, feet))
	x, y := -(feet[1]-muzzle[1])*r/d, (feet[0]-muzzle[0])*r/d
	for _, z := range [...]float64{.5, .98, .02} {
		for _, side := range [...]float64{0, 1, -1} {
			p := wire.Vec3{feet[0] + side*x, feet[1] + side*y, feet[2] + z*h}
			if c.world.ClearShot(muzzle, p) {
				return true
			}
		}
	}
	return false
}

func (c *Controller) incomingProjectile(o wire.Observation) bool {
	horizon := 2 * c.period
	for _, p := range o.Projectiles {
		delta := p.Vel
		if p.Kind == "mortar" {
			// Over this short horizon the chord approximates gravitational fall.
			delta[2] -= .5 * c.cfg.Weapons["mortar"]["gravity"] * horizon
		}
		speed := math.Sqrt(dot(delta, delta))
		if speed == 0 {
			continue
		}
		dir := wire.Vec3{delta[0] / speed, delta[1] / speed, delta[2] / speed}
		r := c.stat(o.Self, "radius") + c.cfg.Projectile[p.Kind+"_radius"]
		entry, ok := cylinderEntry(p.Pos, dir, o.Self.Pos, r, c.stat(o.Self, "height"), speed*horizon)
		if ok && c.world.ClearShot(p.Pos, add(p.Pos, dir, entry)) {
			return true
		}
	}
	return false
}
