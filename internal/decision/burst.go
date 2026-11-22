package decision

import (
	"math"

	"ownbot/internal/wire"
)

const burstHorizon = .3

type warningRay struct {
	muzzle, dir wire.Vec3
}

func (c *Controller) visibleWarnings(o wire.Observation) []warningRay {
	var rays []warningRay
	for _, actor := range o.Mechs {
		if actor.Kind != "mech" || !actor.Charging {
			continue
		}
		scale := 1 + float64(actor.Cubes)*c.cfg.Cubes.SizePerCube
		base := actor.Pos
		base[2] += c.cfg.Mech["muzzle_height"] * scale
		dir := direction(actor.Aim)
		muzzle := add(base, dir, c.cfg.Mech["radius"]*scale+.5)
		if c.world.ClearShot(base, muzzle) {
			rays = append(rays, warningRay{muzzle, dir})
		}
	}
	return rays
}

type burstPath struct {
	feet, vel wire.Vec3
}

func (c *Controller) burstStep(o wire.Observation, path *burstPath, move wire.Vec2, speed, dt, elapsed float64) bool {
	s := o.Self
	dx, dy := speed*move[0]-path.vel[0], speed*move[1]-path.vel[1]
	dv := math.Hypot(dx, dy)
	if dv > 0 {
		f := min(1, c.stat(s, "ground_accel")*dt/dv)
		path.vel[0] += f * dx
		path.vel[1] += f * dy
	}
	next := wire.Vec3{path.feet[0] + path.vel[0]*dt, path.feet[1] + path.vel[1]*dt, path.feet[2]}
	if margin(o.Zone, next, o.T, o.T+elapsed) < 0 {
		return false
	}
	var ok bool
	path.feet, ok = c.world.SupportedStep(path.feet, next, c.stat(s, "radius"), c.stat(s, "height"), c.stat(s, "step_up"), c.stat(s, "max_slope_deg"))
	return ok
}

// Called after rememberLaser so a legal newly issued trigger is already held.
// Release may take until min_charge, including the tick on which it fires.
func (c *Controller) burstSlowFor(o wire.Observation, dt float64) float64 {
	l := c.laser
	if !l.present {
		return 0
	}
	w := o.Self.Weapons[l.slot]
	if w.Cooldown > 0 || o.T < l.shortUntil {
		return 0
	}
	if l.held {
		return burstHorizon
	}
	if l.pending {
		return max(0, c.weapon(w, "min_charge")-w.Charge) + dt
	}
	return 0
}

func (c *Controller) burstSafer(o wire.Observation, a wire.Action) bool {
	s := o.Self
	if !s.Grounded || !s.InZone || a.Jump || a.Move == (wire.Vec2{}) {
		return false
	}
	rays := c.visibleWarnings(o)
	if len(rays) == 0 {
		return false
	}
	cost := c.stat(s, "boost_drain") * burstHorizon
	for i, w := range s.Weapons {
		if w != nil && w.Kind == "lightning" && a.Fire[i] {
			cost += c.weapon(w, "energy")
		}
	}
	hz := c.cfg.Match["tick_hz"]
	steps := int(math.Round(burstHorizon * hz))
	if s.Energy < cost || steps < 1 || steps > 32 || math.Abs(float64(steps)-burstHorizon*hz) > 1e-9 {
		return false
	}
	dt := 1 / hz
	slowFor := c.burstSlowFor(o, dt)
	walk, burst := burstPath{s.Pos, s.Vel}, burstPath{s.Pos, s.Vel}
	move := a.Move
	length := max(1, math.Hypot(move[0], move[1]))
	move[0], move[1] = move[0]/length, move[1]/length
	radius, height := c.stat(s, "radius"), c.stat(s, "height")
	reach := c.cfg.Weapons["laser"]["range"]
	hits := func(ray warningRay, feet wire.Vec3) bool {
		entry, ok := cylinderEntry(ray.muzzle, ray.dir, feet, radius, height, reach)
		return ok && c.world.ClearShot(ray.muzzle, add(ray.muzzle, ray.dir, entry))
	}
	improved := false
	for step := 1; step <= steps; step++ {
		when, mult := float64(step)*dt, 1.
		if float64(step-1)*dt < slowFor-1e-9 {
			mult = c.stat(s, "charge_move_mult")
		}
		if !c.burstStep(o, &walk, move, mult*c.stat(s, "walk_speed"), dt, when) ||
			!c.burstStep(o, &burst, move, mult*c.stat(s, "boost_speed"), dt, when) {
			return false
		}
		for _, ray := range rays {
			walkHit, burstHit := hits(ray, walk.feet), hits(ray, burst.feet)
			if burstHit && !walkHit {
				return false
			}
			improved = improved || walkHit && !burstHit
		}
	}
	return improved
}
