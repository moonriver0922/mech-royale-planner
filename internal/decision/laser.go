package decision

import (
	"math"

	"ownbot/internal/wire"
)

// Only one laser can be equipped. The slot's charge is authoritative; the
// local latch records the falling edge that can queue a later minimum shot.
type laserState struct {
	present             bool
	slot                int
	held, pending       bool
	started, shortUntil float64
	lastTick            int
}

func (c *Controller) observeLaser(o wire.Observation) {
	l := &c.laser
	for i, w := range o.Self.Weapons {
		if w == nil || w.Kind != "laser" {
			continue
		}
		if !l.present || l.slot != i {
			*l = laserState{present: true, slot: i, lastTick: o.Tick}
		}
		for _, e := range o.Events {
			if e.Type == "effect" && e.Effect == "short" {
				l.shortUntil = o.T + c.cfg.Weapons["lightning"]["short_s"]
			}
		}
		if o.T < l.shortUntil || w.Cooldown > 0 || (w.Charge == 0 && o.Tick > l.lastTick) {
			l.pending, l.held = false, false
		} else if w.Charge > 0 && !l.held && !l.pending {
			// First observation may already contain a live charge.
			l.held, l.started = true, o.T-w.Charge
		}
		if o.T >= l.shortUntil && w.Cooldown == 0 && w.Charge > 0 && o.Tick-l.lastTick > int(c.cfg.Match["decide_every"]) {
			// The protocol drops fire on a missed decision. Honor its release.
			l.held, l.pending = false, true
		}
		l.lastTick = o.Tick
		return
	}
	*l = laserState{}
}

func (l *laserState) available(o wire.Observation, w *wire.Weapon) bool {
	return l.present && !l.pending && o.T >= l.shortUntil && w.Cooldown == 0
}

func (c *Controller) rememberLaser(o wire.Observation, a wire.Action) {
	l := &c.laser
	if !l.present {
		return
	}
	if a.Fire[l.slot] {
		if !l.held {
			l.started = o.T
		}
		l.held = true
	} else if l.held {
		l.pending, l.held = true, false
	}
}

func (c *Controller) laserSlowTime(o wire.Observation) float64 {
	l := c.laser
	if !l.present {
		return 0
	}
	w := o.Self.Weapons[l.slot]
	if w == nil || w.Kind != "laser" || w.Cooldown > 0 || o.T < l.shortUntil {
		return 0
	}
	if l.pending {
		return max(c.period, c.weapon(w, "min_charge")-w.Charge+c.period)
	}
	if l.held || w.Charge > 0 {
		return max(0, c.weapon(w, "charge")-w.Charge) + 2*c.period
	}
	return 0
}

func (c *Controller) nextAim(o wire.Observation, cmd wire.Vec2) wire.Vec2 {
	dt := 1 / max(1, c.cfg.Match["tick_hz"])
	mult := 1.
	if c.laser.present {
		w := o.Self.Weapons[c.laser.slot]
		if w != nil {
			mult = c.weapon(w, "charge_turn_mult")
		}
	}
	limit := func(delta, rate float64) float64 { return max(-rate*dt*mult, min(rate*dt*mult, delta)) }
	current := o.Self.Aim
	return wire.Vec2{wrap(current[0] + limit(wrap(cmd[0]-current[0]), c.stat(o.Self, "turn_rate"))),
		current[1] + limit(max(c.stat(o.Self, "min_pitch"), min(c.stat(o.Self, "max_pitch"), cmd[1]))-current[1], c.stat(o.Self, "pitch_rate"))}
}

// Clip the segment from the forward muzzle against a finite vertical cylinder.
// First contact bounds the map query: geometry behind the actor is irrelevant.
func cylinderEntry(muzzle, dir, feet wire.Vec3, radius, height, length float64) (float64, bool) {
	offset := sub(muzzle, feet)
	lo, hi := 0., length
	a := dir[0]*dir[0] + dir[1]*dir[1]
	b := offset[0]*dir[0] + offset[1]*dir[1]
	k := offset[0]*offset[0] + offset[1]*offset[1] - radius*radius
	if a < 1e-12 {
		if k > 0 {
			return 0, false
		}
	} else {
		det := b*b - a*k
		if det < 0 {
			return 0, false
		}
		root := math.Sqrt(det)
		lo, hi = max(lo, (-b-root)/a), min(hi, (-b+root)/a)
	}
	if math.Abs(dir[2]) < 1e-9 {
		if offset[2] < 0 || offset[2] > height {
			return 0, false
		}
	} else {
		lower, upper := -offset[2]/dir[2], (height-offset[2])/dir[2]
		lo, hi = max(lo, min(lower, upper)), min(hi, max(lower, upper))
	}
	return lo, hi >= lo
}

func (c *Controller) laserRay(o wire.Observation, w *wire.Weapon, s shot, aim wire.Vec2, dt float64) bool {
	if aim[1] < c.stat(o.Self, "min_pitch") || aim[1] > c.stat(o.Self, "max_pitch") {
		return false
	}
	base := add(s.origin, o.Self.Vel, dt)
	feet := add(s.pos, s.velocity, dt)
	dir := direction(aim)
	muzzle := add(base, dir, c.stat(o.Self, "radius")+.5)
	entry, ok := cylinderEntry(muzzle, dir, feet, s.radius, s.height, c.weapon(w, "range"))
	return ok && c.world.ClearShot(base, muzzle) && c.world.ClearShot(muzzle, add(muzzle, dir, entry))
}

func (c *Controller) planLaser(o wire.Observation, s shot, a *wire.Action) bool {
	l := c.laser
	if !l.present {
		return false
	}
	w := o.Self.Weapons[l.slot]
	if !l.available(o, w) {
		return false
	}
	full, minCharge := c.weapon(w, "charge"), c.weapon(w, "min_charge")
	aim := angles(sub(s.center, s.origin))
	clear := c.laserRay(o, w, s, aim, 0)
	if !l.held {
		if !clear {
			return false
		}
		if _, urgent := c.zoneWithSlowTime(o, full+2*c.period); urgent {
			return false
		}
	}
	tick := 1 / max(1, c.cfg.Match["tick_hz"])
	// One decision of linear motion detects a disappearing shot window.
	// The final release also checks the observed and next physics-tick rays.
	nextPoint := sub(add(s.center, s.velocity, c.period), add(s.origin, o.Self.Vel, c.period))
	losing := w.Charge >= minCharge && clear && !c.laserRay(o, w, s, angles(nextPoint), c.period)
	turnMult := c.weapon(w, "charge_turn_mult")
	alignTime := func(from wire.Vec2) float64 {
		yaw := math.Abs(wrap(aim[0]-from[0])) / max(.01, c.stat(o.Self, "turn_rate")*turnMult)
		pitch := math.Abs(aim[1]-from[1]) / max(.01, c.stat(o.Self, "pitch_rate")*turnMult)
		return max(yaw, pitch)
	}
	a.Fire[l.slot] = true
	if !losing {
		// A loaded gun keeps its intercept early; a reloading gun yields aim.
		for _, g := range o.Self.Weapons {
			if g != nil && g.Kind == "gun" && g.Ammo > 0 && g.Reload == 0 && g.Cooldown <= c.period {
				lead := angles(sub(c.lead(g, s.center, s.origin, s.velocity), s.origin))
				// Reserve the larger turn from the actual and requested gun
				// bearings, plus one decision for this final gun opportunity.
				reserve := c.period + max(alignTime(o.Self.Aim), alignTime(lead))
				if full-w.Charge > reserve && c.gunAligned(o, g, s, lead) {
					a.Aim = lead
					return true
				}
			}
		}
	}
	a.Aim = aim
	aligned := clear && c.laserRay(o, w, s, o.Self.Aim, 0) && c.laserRay(o, w, s, c.nextAim(o, aim), tick)
	if aligned && (w.Charge >= full-1e-6 || losing) {
		a.Fire[l.slot] = false
	}
	// Once fully charged, permit only a brief alignment correction. A release
	// at this deadline disposes of an unavoidable shot; it is not a hit claim.
	if l.held && o.T-l.started >= full+min(2*c.period, max(0, c.weapon(w, "hold_s")-c.period)) {
		a.Fire[l.slot] = false
	}
	return true
}
