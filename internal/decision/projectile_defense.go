package decision

import (
	"math"

	"ownbot/internal/wire"
)

const defenseHorizon = .6

type groundCommand struct {
	move  wire.Vec2
	boost bool
}

type incomingRound struct {
	wire.Projectile
	damage, radius float64
}

type defenseModel struct {
	rounds                                  []incomingRound
	dt, horizon                             float64
	steps                                   int
	radius, height, stepUp, slope           float64
	accel, walkSpeed, boostSpeed, boostCost float64
	slowFor, slowMult                       float64
}

type groundPath struct {
	feet    []wire.Vec3
	margins []float64
}

func (c *Controller) defendProjectiles(o wire.Observation, original wire.Action) wire.Action {
	if !o.Self.Grounded || original.Jump || len(o.Projectiles) == 0 {
		return original
	}
	m := c.defenseModel(o, original)
	if m.steps == 0 || len(m.rounds) == 0 {
		return original
	}
	baseCommand := groundCommand{original.Move, original.Boost}
	base, ok := c.groundPath(o, m, baseCommand)
	if !ok {
		return original
	}
	baseDamage, first := c.pathDamage(m, base)
	if baseDamage == 0 {
		return original
	}

	bestCommand, bestDamage := baseCommand, baseDamage
	bestProgress := math.Inf(-1)
	energy := o.Self.Energy
	for i, w := range o.Self.Weapons {
		if w != nil && w.Kind == "lightning" && original.Fire[i] {
			cost := c.weapon(w, "energy")
			if cost <= 0 {
				energy = 0
			} else {
				energy -= cost
			}
		}
	}
	canBoost := m.boostSpeed > 0 && m.boostCost > 0 && energy >= m.boostCost
	const diag = math.Sqrt2 / 2
	dirs := [...]wire.Vec2{{1, 0}, {diag, diag}, {0, 1}, {-diag, diag}, {-1, 0}, {-diag, -diag}, {0, -1}, {diag, -diag}}
	seen := map[groundCommand]bool{baseCommand: true}
	try := func(cmd groundCommand) {
		if seen[cmd] || (cmd.boost && (!canBoost || cmd.move == (wire.Vec2{}))) || (!o.Self.InZone && original.Boost && !cmd.boost) {
			return
		}
		seen[cmd] = true
		path, valid := c.groundPath(o, m, cmd)
		if !valid || !zoneFeasible(o.Self.InZone, base, path) {
			return
		}
		damage, _ := c.pathDamage(m, path)
		if damage >= baseDamage || damage > bestDamage {
			return
		}
		end := path.feet[len(path.feet)-1]
		progress := (end[0]-o.Self.Pos[0])*original.Move[0] + (end[1]-o.Self.Pos[1])*original.Move[1]
		if damage == bestDamage {
			if cmd.boost && !bestCommand.boost {
				return
			}
			if cmd.boost == bestCommand.boost && progress <= bestProgress+1e-9 {
				return
			}
		}
		bestCommand, bestDamage, bestProgress = cmd, damage, progress
	}
	for _, dir := range dirs {
		try(groundCommand{dir, false})
	}
	try(groundCommand{original.Move, true})
	v := m.rounds[first].Vel
	if n := math.Hypot(v[0], v[1]); n > 0 {
		try(groundCommand{wire.Vec2{-v[1] / n, v[0] / n}, true})
		try(groundCommand{wire.Vec2{v[1] / n, -v[0] / n}, true})
	}
	original.Move, original.Boost = bestCommand.move, bestCommand.boost
	return original
}

func (c *Controller) defenseModel(o wire.Observation, a wire.Action) defenseModel {
	s := o.Self
	hz := c.cfg.Match["tick_hz"]
	steps := int(defenseHorizon * hz)
	if steps <= 0 {
		return defenseModel{}
	}
	m := defenseModel{
		dt: 1 / hz, horizon: float64(steps) / hz, steps: steps,
		radius: c.stat(s, "radius"), height: c.stat(s, "height"),
		stepUp: c.stat(s, "step_up"), slope: c.stat(s, "max_slope_deg"),
		accel: c.stat(s, "ground_accel"), walkSpeed: c.stat(s, "walk_speed"),
		boostSpeed: c.stat(s, "boost_speed"), slowMult: c.stat(s, "charge_move_mult"),
	}
	m.boostCost = c.stat(s, "boost_drain") * m.horizon
	m.slowFor = c.defenseSlowTime(o, a, m.horizon)
	// Any feasible body center stays inside this disc over the horizon.
	reach := m.horizon*max(m.walkSpeed, m.boostSpeed, math.Hypot(s.Vel[0], s.Vel[1])) + m.radius
	for _, p := range o.Projectiles {
		var dmg float64
		switch p.Kind {
		case "gun":
			dmg = c.cfg.Weapons["gun"]["damage"]
		case "npc":
			dmg = c.cfg.NPC["damage"]
		default:
			continue
		}
		r := c.cfg.Projectile[p.Kind+"_radius"]
		speed2 := p.Vel[0]*p.Vel[0] + p.Vel[1]*p.Vel[1]
		t := 0.
		if speed2 > 0 {
			t = max(0, min(m.horizon, ((s.Pos[0]-p.Pos[0])*p.Vel[0]+(s.Pos[1]-p.Pos[1])*p.Vel[1])/speed2))
		}
		if dmg > 0 && distance(add(p.Pos, p.Vel, t), s.Pos) <= reach+r {
			m.rounds = append(m.rounds, incomingRound{p, dmg, r})
		}
	}
	return m
}

// Assume the completed trigger command persists during the movement forecast.
// A release below minimum charge remains slow until the queued shot can fire.
func (c *Controller) defenseSlowTime(o wire.Observation, a wire.Action, horizon float64) float64 {
	l := c.laser
	if !l.present || o.T < l.shortUntil {
		return 0
	}
	w := o.Self.Weapons[l.slot]
	if w == nil || w.Kind != "laser" || w.Cooldown > 0 {
		return 0
	}
	if a.Fire[l.slot] && !l.pending {
		if !l.held {
			return horizon
		}
		return min(horizon, max(0, l.started+c.weapon(w, "charge")+c.weapon(w, "hold_s")-o.T))
	}
	if w.Charge > 0 || l.held || l.pending {
		return max(0, c.weapon(w, "min_charge")-w.Charge)
	}
	return 0
}

func (c *Controller) groundPath(o wire.Observation, m defenseModel, cmd groundCommand) (groundPath, bool) {
	path := groundPath{feet: make([]wire.Vec3, m.steps+1), margins: make([]float64, m.steps)}
	p, v := o.Self.Pos, o.Self.Vel
	path.feet[0] = p
	speed := m.walkSpeed
	if cmd.boost {
		speed = m.boostSpeed
	}
	scale := speed / max(1, math.Hypot(cmd.move[0], cmd.move[1]))
	for i := 0; i < m.steps; i++ {
		target := scale
		if float64(i)*m.dt < m.slowFor {
			target *= m.slowMult
		}
		dx, dy := target*cmd.move[0]-v[0], target*cmd.move[1]-v[1]
		if n := math.Hypot(dx, dy); n > 0 {
			f := min(1, m.accel*m.dt/n)
			v[0] += dx * f
			v[1] += dy * f
		}
		next := wire.Vec3{p[0] + v[0]*m.dt, p[1] + v[1]*m.dt, p[2]}
		z, ok := c.world.GroundSegment(p, next, m.radius, m.height, m.stepUp, m.slope)
		// A drop larger than a step may start a fall, outside this grounded model.
		if !ok || p[2]-z > m.stepUp+1e-6 {
			return groundPath{}, false
		}
		next[2] = z
		path.feet[i+1] = next
		path.margins[i] = margin(o.Zone, next, o.T, o.T+float64(i+1)*m.dt)
		p = next
	}
	return path, true
}

func zoneFeasible(inside bool, base, alt groundPath) bool {
	for i, left := range alt.margins {
		required := base.margins[i]
		if inside {
			required = 0
		}
		if left+1e-8 < required {
			return false
		}
	}
	return true
}

// Test a relative segment against the moving body. Terrain or a prism must
// leave the bullet's center ray clear all the way from observation to contact.
func (c *Controller) pathDamage(m defenseModel, path groundPath) (float64, int) {
	damage, earliest, first := 0., math.Inf(1), -1
	for j, p := range m.rounds {
		for i := 0; i < m.steps; i++ {
			start := add(p.Pos, p.Vel, float64(i)*m.dt)
			feet, next := path.feet[i], path.feet[i+1]
			relative := sub(add(wire.Vec3{}, p.Vel, m.dt), sub(next, feet))
			feet[2] -= p.radius
			f, hit := cylinderEntry(start, relative, feet, m.radius+p.radius, m.height+2*p.radius, 1)
			if !hit {
				continue
			}
			t := (float64(i) + f) * m.dt
			if c.world.ClearShot(p.Pos, add(p.Pos, p.Vel, t)) {
				damage += p.damage
				if t < earliest {
					earliest, first = t, j
				}
			}
			break
		}
	}
	return damage, first
}
