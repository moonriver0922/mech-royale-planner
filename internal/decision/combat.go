package decision

import (
	"math"

	"ownbot/internal/wire"
)

type shot struct {
	id                            int
	pos, center, velocity, origin wire.Vec3
	aim                           wire.Vec2
	time, distance, radius, hp    float64
	height                        float64
}

func (c *Controller) weapon(w *wire.Weapon, name string) float64 {
	if v, found := w.Stats[name]; found {
		return v
	}
	return c.cfg.Weapons[w.Kind][name]
}

func (c *Controller) bestEnemy(o wire.Observation) (shot, bool) {
	var best, charged, threat shot
	found, observedCharge := false, false
	highScore := 0.
	for _, p := range o.Mechs {
		height, radius := c.actorSize(p)
		s, ok := c.solveTarget(o, p.ID, p.Pos, p.Vel, height, radius, p.HP, true)
		if c.laser.held && c.target == (goal{"enemy", p.ID}) {
			charged, observedCharge = s, true
		}
		if ok && (!found || c.prefer(s, best)) {
			best, found = s, true
		}
		if ok && p.Kind == "mech" && playerID(s.id) && c.pressure.total[s.id] > 0 {
			score := c.pressure.total[s.id] / max(c.period, c.killTime(o, s))
			if score > highScore || (score > 0 && score == highScore && c.preferPressureTie(s, threat)) {
				threat, highScore = s, score
			}
		}
	}
	if !found && observedCharge {
		return charged, true
	}
	if highScore > 0 && !c.finishingVolley(o, best) {
		if observedCharge && threat.id != charged.id && !c.clearRelease(o, threat) {
			return charged, true
		}
		return threat, true
	}
	return best, found
}

func (c *Controller) prefer(a, b shot) bool {
	if math.Abs(a.time-b.time) > c.period {
		return a.time < b.time
	}
	if (a.id == c.target.id) != (b.id == c.target.id) && c.target.kind == "enemy" {
		return a.id == c.target.id
	}
	if a.hp > 0 && b.hp > 0 && a.hp != b.hp {
		return a.hp < b.hp
	}
	return a.id < b.id
}

// Intercept solves |offset + velocity*t| = projectileSpeed*t.
func intercept(offset, velocity wire.Vec3, speed float64) float64 {
	a := dot(velocity, velocity) - speed*speed
	b := 2 * dot(offset, velocity)
	d := dot(offset, offset)
	if math.Abs(a) < 1e-9 {
		if b < 0 {
			return -d / b
		}
		return 0
	}
	discriminant := b*b - 4*a*d
	if discriminant < 0 {
		return 0
	}
	t1, t2 := (-b-math.Sqrt(discriminant))/(2*a), (-b+math.Sqrt(discriminant))/(2*a)
	if t2 > 0 && (t1 <= 0 || t2 < t1) {
		return t2
	}
	return max(0, t1)
}

func (c *Controller) solve(o wire.Observation, id int, pos, vel wire.Vec3, height, radius, hp float64) (shot, bool) {
	return c.solveTarget(o, id, pos, vel, height, radius, hp, false)
}

func (c *Controller) solveTarget(o wire.Observation, id int, pos, vel wire.Vec3, height, radius, hp float64, enemy bool) (shot, bool) {
	base := o.Self.Pos
	base[2] += c.stat(o.Self, "muzzle_height")
	center := pos
	center[2] += height / 2
	v := sub(center, base)
	target := shot{id: id, pos: pos, center: center, velocity: vel, origin: base, radius: radius, hp: hp, height: height, aim: angles(v), distance: math.Sqrt(dot(v, v))}
	best := target
	found := false
	for _, w := range o.Self.Weapons {
		if w == nil {
			continue
		}
		switch w.Kind {
		case "gun", "lightning":
		case "laser":
			if !enemy || !c.laser.available(o, w) {
				continue
			}
		default:
			continue
		}
		point := center
		if w.Kind == "gun" {
			point = c.lead(w, center, base, vel)
		}
		v := sub(point, base)
		aim := angles(v)
		dir := direction(aim)
		d := math.Sqrt(dot(v, v))
		if d == 0 {
			continue
		}
		yawTime := math.Abs(wrap(aim[0]-o.Self.Aim[0])) / max(.01, c.stat(o.Self, "turn_rate"))
		pitchTime := math.Abs(aim[1]-o.Self.Aim[1]) / max(.01, c.stat(o.Self, "pitch_rate"))
		if w.Kind == "lightning" {
			pitchTime = 0
		} else if aim[1] < c.stat(o.Self, "min_pitch") || aim[1] > c.stat(o.Self, "max_pitch") {
			continue
		}
		if w.Kind == "laser" {
			if !c.laserRay(o, w, target, aim, 0) {
				continue
			}
		} else {
			muzzle := add(base, dir, min(d/2, c.stat(o.Self, "radius")+.5))
			if !c.world.ClearShot(base, muzzle) || !c.world.ClearShot(muzzle, point) {
				continue
			}
		}
		closing := max(0, d-c.weapon(w, "range")) / max(1, c.stat(o.Self, "walk_speed"))
		ready := max(w.Cooldown, w.Reload)
		if w.Kind == "lightning" && o.Self.Energy < c.weapon(w, "energy") {
			continue
		}
		if w.Kind == "laser" {
			closing = 0 // finite muzzle ray already intersects the target
			ready = max(ready, c.weapon(w, "min_charge")-w.Charge)
		}
		time := max(yawTime, pitchTime, closing, ready)
		if !found || time < best.time {
			best.aim, best.time, best.distance = aim, time, d
			found = true
		}
	}
	return best, found
}

func (c *Controller) lead(w *wire.Weapon, center, origin, velocity wire.Vec3) wire.Vec3 {
	t := intercept(sub(center, origin), velocity, c.weapon(w, "speed"))
	return add(center, velocity, t)
}

func (c *Controller) fire(o wire.Observation, s shot, a *wire.Action, allowLaser bool) {
	// Set the common command before authorizing either trigger.
	laserControls := allowLaser && c.planLaser(o, s, a)

	for i, w := range o.Self.Weapons {
		if w == nil || w.Cooldown > c.period {
			continue
		}
		switch w.Kind {
		case "gun":
			if w.Ammo <= 0 || w.Reload > 0 {
				continue
			}
			if c.gunAligned(o, w, s, o.Self.Aim) && (!laserControls || c.gunAligned(o, w, s, c.nextAim(o, a.Aim))) {
				a.Fire[i] = true
			}
		case "lightning":
			p := s.center
			v := sub(p, s.origin)
			bearing := angles(v)[0]
			if dot(v, v) <= math.Pow(c.weapon(w, "range"), 2) && o.Self.Energy >= c.weapon(w, "energy") && math.Abs(wrap(bearing-o.Self.Aim[0])) <= c.weapon(w, "cone_deg")*math.Pi/360 &&
				c.world.ClearShot(s.origin, p) {
				a.Fire[i] = true
			}
		}
	}
}

func (c *Controller) engage(o wire.Observation, s shot) wire.Vec2 {
	c.target = goal{kind: "enemy", id: s.id}
	c.route = nil
	p := o.Self.Pos
	yaw := math.Atan2(s.pos[1]-p[1], s.pos[0]-p[0])
	orbit := math.Pi / 2
	if (int(o.T/3)+o.Self.ID)%2 == 1 {
		orbit = -orbit
	}
	r := c.cfg.NPC["aggro_range"]
	if s.distance > r*2 {
		orbit *= .55
	}
	if s.distance < r/2 && o.Self.Weapons[1] == nil {
		orbit *= 1.35
	}
	return c.step(o, yaw+orbit, false)
}

func dot(a, b wire.Vec3) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

func sub(a, b wire.Vec3) wire.Vec3 {
	return wire.Vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

func add(a, b wire.Vec3, scale float64) wire.Vec3 {
	return wire.Vec3{a[0] + scale*b[0], a[1] + scale*b[1], a[2] + scale*b[2]}
}

func angles(v wire.Vec3) wire.Vec2 {
	return wire.Vec2{math.Atan2(v[1], v[0]), math.Atan2(v[2], math.Hypot(v[0], v[1]))}
}

func direction(a wire.Vec2) wire.Vec3 {
	return wire.Vec3{math.Cos(a[1]) * math.Cos(a[0]), math.Cos(a[1]) * math.Sin(a[0]), math.Sin(a[1])}
}

func wrap(x float64) float64 { return math.Remainder(x, 2*math.Pi) }

func (c *Controller) gunAligned(o wire.Observation, w *wire.Weapon, s shot, aim wire.Vec2) bool {
	dir := direction(aim)
	v := sub(c.lead(w, s.center, s.origin, s.velocity), s.origin)
	along := dot(dir, v)
	tolerance := .9*s.radius + c.cfg.Projectile["gun_radius"]
	if along <= 0 || along > c.weapon(w, "range") || dot(v, v)-along*along > tolerance*tolerance {
		return false
	}
	muzzle := add(s.origin, dir, min(along/2, c.stat(o.Self, "radius")+.5))
	return c.world.ClearShot(s.origin, muzzle) && c.world.ClearShot(muzzle, add(s.origin, dir, along))
}
