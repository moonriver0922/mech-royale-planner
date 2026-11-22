package decision

import (
	"math"

	"ownbot/internal/wire"
)

func distance(a, b wire.Vec3) float64 { return math.Hypot(a[0]-b[0], a[1]-b[1]) }

func (c *Controller) onGround(x, y float64) wire.Vec3 {
	return wire.Vec3{x, y, c.world.GroundHeight(x, y)}
}

func (c *Controller) walk(s wire.Self, a, b wire.Vec3) bool {
	return c.world.Segment(a, b, c.stat(s, "radius"), c.stat(s, "height"), c.stat(s, "step_up"), c.stat(s, "max_slope_deg"))
}

// The current circle is already interpolated at now, including during shrink.
func circle(z wire.Zone, now, when float64) (wire.Vec2, float64) {
	if z.ShrinkEnd <= z.ShrinkStart || when <= z.ShrinkStart {
		return z.Center, z.Radius
	}
	start := max(now, z.ShrinkStart)
	if z.ShrinkEnd <= start {
		return z.NextCenter, z.NextRadius
	}
	f := min(1, (when-start)/(z.ShrinkEnd-start))
	return wire.Vec2{z.Center[0] + f*(z.NextCenter[0]-z.Center[0]), z.Center[1] + f*(z.NextCenter[1]-z.Center[1])}, z.Radius + f*(z.NextRadius-z.Radius)
}

func margin(z wire.Zone, p wire.Vec3, now, when float64) float64 {
	center, r := circle(z, now, when)
	return r - math.Hypot(p[0]-center[0], p[1]-center[1])
}

func (c *Controller) travel(o wire.Observation, length float64) float64 {
	return c.travelSlowed(o, length, c.laserSlowTime(o))
}
func (c *Controller) travelSlowed(o wire.Observation, length, slowFor float64) float64 {
	speed := max(1, c.stat(o.Self, "walk_speed"))
	mult := max(.01, min(1, c.stat(o.Self, "charge_move_mult")))
	slowTime := min(slowFor, length/(speed*mult))
	return length/speed + slowTime*(1-mult) + speed/max(1, c.stat(o.Self, "ground_accel"))
}

func (c *Controller) zoneGoal(o wire.Observation) (wire.Vec3, bool) {
	return c.zoneWithSlowTime(o, c.laserSlowTime(o))
}
func (c *Controller) zoneWithSlowTime(o wire.Observation, slowFor float64) (wire.Vec3, bool) {
	z, p := o.Zone, o.Self.Pos
	n := z.NextCenter
	if z.ShrinkEnd <= z.ShrinkStart {
		n = z.Center
		z.NextRadius = z.Radius
	}
	dx, dy := p[0]-n[0], p[1]-n[1]
	d := math.Hypot(dx, dy)
	inset := max(0, z.NextRadius-min(z.NextRadius*.3, c.stat(o.Self, "walk_speed")))
	dest := c.onGround(n[0]+dx*inset/max(d, inset, 1), n[1]+dy*inset/max(d, inset, 1))
	if !o.Self.InZone {
		return dest, true
	}
	if d <= inset {
		return dest, false
	}

	// Estimate arrival at a safe landing point. Account for a retained detour
	// and sample the line en route, so a shrinking edge can interrupt a goal.
	length := distance(p, dest)
	if c.target.kind == "zone" && len(c.route) > 0 {
		leg := 0.
		for _, waypoint := range c.route {
			leg += distance(p, waypoint)
			p = waypoint
		}
		length = max(length, leg+distance(p, dest))
	}
	deadline := c.travelSlowed(o, length*1.2, slowFor) + 2*c.period
	return dest, margin(z, o.Self.Pos, o.T, o.T+deadline) <= 0
}

func (c *Controller) follow(o wire.Observation, g goal, dest wire.Vec3) wire.Vec2 {
	s, p := o.Self, o.Self.Pos
	changed := c.target != g || distance(c.destination, dest) > 7 || math.Abs(c.destination[2]-dest[2]) > 2
	if changed || (o.T >= c.replanAt && (len(c.route) == 0 || c.blockedFor > .5)) {
		c.target, c.destination = g, dest
		c.route, _ = c.world.Route(p, dest, c.stat(s, "radius"), c.stat(s, "height"))
		c.replanAt = o.T + .6
	}
	for len(c.route) > 1 && distance(p, c.route[0]) < 2 && c.walk(s, p, c.route[1]) {
		c.route = c.route[1:]
	}
	next := dest
	if len(c.route) > 0 {
		next = c.route[0]
	}
	if distance(p, next) < .25 {
		return wire.Vec2{}
	}
	yaw := math.Atan2(next[1]-p[1], next[0]-p[0])
	if c.blockedFor > .5 {
		yaw += .6 * math.Sin(o.T*4+float64(s.ID))
	}
	m := c.step(o, yaw, g.kind == "zone")
	speed := max(1, c.stat(s, "walk_speed"))
	scale := min(1, distance(p, next)/max(2*c.period*speed, 1))
	return wire.Vec2{m[0] * scale, m[1] * scale}
}

func (c *Controller) step(o wire.Observation, yaw float64, emergency bool) wire.Vec2 {
	p := o.Self.Pos
	l := 3 * c.stat(o.Self, "walk_speed") * c.period
	current := margin(o.Zone, p, o.T, o.T)
	best := wire.Vec2{}
	bestRoom := math.Inf(-1)
	for _, turn := range [...]float64{0, .5, -.5, 1.1, -1.1, 1.7, -1.7, math.Pi} {
		x, y := math.Cos(yaw+turn), math.Sin(yaw+turn)
		q := c.onGround(p[0]+l*x, p[1]+l*y)
		room := margin(o.Zone, q, o.T, o.T+3*c.period)
		if !c.walk(o.Self, p, q) {
			continue
		}
		if (o.Self.InZone && room >= 0) || (!o.Self.InZone && room > current) {
			return wire.Vec2{x, y}
		}
		if emergency && room > bestRoom {
			best, bestRoom = wire.Vec2{x, y}, room
		}
	}
	return best
}

func (c *Controller) recover(o wire.Observation) (move wire.Vec2, covered, escape bool) {
	s := o.Self
	if !s.InZone || (!c.recovering && s.HP >= .65*s.MaxHP) {
		return wire.Vec2{}, false, false
	}
	if c.coverValid && (margin(o.Zone, c.cover, o.T, o.T+c.travel(o, distance(s.Pos, c.cover))) <= 0 ||
		c.exposure(o, c.cover) > 0) {
		c.coverValid = false
	}
	if !c.coverValid {
		exposure := c.exposure(o, s.Pos)
		quietFor := o.T - max(c.lastDamage, c.lastDealt)
		if exposure == 0 && (quietFor >= c.stat(s, "regen_delay") || (c.recovering && len(o.Mechs) == 0)) {
			c.cover, c.coverValid = s.Pos, true
		} else {
			best := exposure
			for _, length := range [...]float64{12, 24} {
				for i := range 8 {
					y := float64(i)*math.Pi/4 + s.Aim[0]
					p := c.onGround(s.Pos[0]+math.Cos(y)*length, s.Pos[1]+math.Sin(y)*length)
					if margin(o.Zone, p, o.T, o.T+c.travel(o, length)) < 0 || !c.walk(s, s.Pos, p) {
						continue
					}
					if n := c.exposure(o, p); n < best {
						c.cover, c.coverValid, best = p, true, n
					}
				}
			}
		}
	}
	if !c.coverValid {
		if len(o.Mechs) == 0 {
			return wire.Vec2{}, false, false
		}
		closest := o.Mechs[0]
		for _, m := range o.Mechs[1:] {
			if distance(m.Pos, s.Pos) < distance(closest.Pos, s.Pos) {
				closest = m
			}
		}
		y := math.Atan2(s.Pos[1]-closest.Pos[1], s.Pos[0]-closest.Pos[0])
		move := c.step(o, y, false)
		// An uncovered escape still allows firing at a visible target.
		return move, false, move != (wire.Vec2{})
	}
	c.recovering = true
	if distance(s.Pos, c.cover) <= 1 {
		return wire.Vec2{}, true, false
	}
	return c.follow(o, goal{kind: "cover"}, c.cover), true, false
}

func (c *Controller) exposure(o wire.Observation, p wire.Vec3) int {
	p[2] += c.stat(o.Self, "height") / 2
	n := 0
	for _, m := range o.Mechs {
		q := m.Pos
		if m.Kind == "npc" {
			q[2] += c.cfg.NPC["height"] / 2
		} else {
			q[2] += c.cfg.Mech["muzzle_height"] * (1 + float64(m.Cubes)*c.cfg.Cubes.SizePerCube)
		}
		if c.world.ClearShot(q, p) {
			n++
		}
	}
	return n
}
