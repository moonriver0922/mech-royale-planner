package decision

import (
	"math"
	"slices"

	"ownbot/internal/wire"
)

const growthHorizon = 3.0

// Keep combat target selection, route ownership, turret control and triggers
// in their original paths. Zone and recovery branches never call this method.
func (c *Controller) engageWithGrowth(o wire.Observation, enemy shot, a *wire.Action) {
	a.Move = c.engage(o, enemy)
	c.fire(o, enemy, a, true)
	if len(o.Cubes)+len(o.Pickups) == 0 || !o.Self.Grounded {
		return
	}
	p := o.Self.Pos
	// The walking projection uses terrain feet heights. Skip elevated supports,
	// including roofs whose edges could otherwise masquerade as ground paths.
	if math.Abs(p[2]-c.world.GroundHeight(p[0], p[1])) > .1 {
		return
	}
	player := false
	var carriers []wire.Actor
	for _, m := range o.Mechs {
		if m.ID == enemy.id && m.Kind == "mech" {
			player = true
		}
		if slices.Contains(m.Weapons, "lightning") {
			carriers = append(carriers, m)
		}
	}
	if !player {
		return
	}

	walk := c.growthWalk(o, *a)
	bestTime, bestID := math.Inf(1), 0
	original := *a
	consider := func(id int, pos wire.Vec3, radius float64, weapon bool) {
		eta, move, ok := c.collectionMove(o, original, enemy, walk, carriers, pos, radius, weapon)
		if !ok || eta > bestTime || (eta == bestTime && id >= bestID) {
			return
		}
		bestTime, bestID = eta, id
		a.Move, a.Take = move, nil
		if weapon && pickupInReach(p, pos, radius) {
			a.Take = &id
		}
	}
	if float64(o.Self.Cubes) < c.cfg.Cubes.StatCap {
		for _, cube := range o.Cubes {
			consider(cube.ID, cube.Pos, c.cfg.Cubes.PickupRadius, false)
		}
	}
	if o.Self.Weapons[1] == nil {
		for _, loot := range o.Pickups {
			if (loot.Kind != "lightning" && loot.Kind != "laser") ||
				(o.Self.Weapons[0] != nil && o.Self.Weapons[0].Kind == loot.Kind) ||
				(loot.ID == c.failedTake && o.T < c.retryTakeAt) {
				continue
			}
			consider(loot.ID, loot.Pos, c.cfg.Loot["pickup_radius"], true)
		}
	}
}

type growthWalk struct {
	tick, speed, accel, slowFor, slowSpeed float64
}

func (c *Controller) growthWalk(o wire.Observation, a wire.Action) growthWalk {
	s := o.Self
	speed := c.stat(s, "walk_speed")
	slowFor := c.laserSlowTime(o)
	if c.laser.present && a.Fire[c.laser.slot] {
		w := s.Weapons[c.laser.slot]
		slowFor = max(slowFor, c.weapon(w, "charge")-w.Charge+2*c.period)
	}
	return growthWalk{
		tick: 1 / max(1, c.cfg.Match["tick_hz"]), speed: speed, accel: c.stat(s, "ground_accel"),
		slowFor: slowFor, slowSpeed: speed * c.stat(s, "charge_move_mult"),
	}
}

func (w growthWalk) speedAt(t float64) float64 {
	if t < w.slowFor {
		return w.slowSpeed
	}
	return w.speed
}

// This projection assumes the mech's feet follow the terrain. Reject even a
// walkable prism step if its elevated support would violate that assumption.
func (c *Controller) terrainWalk(s wire.Self, from, to wire.Vec3) bool {
	return c.world.Segment(from, to, c.stat(s, "radius"), c.stat(s, "height"), 0, c.stat(s, "max_slope_deg"))
}

type bodyMotion struct {
	pos wire.Vec3
	vel wire.Vec2
}

func (c *Controller) walkTick(body bodyMotion, move wire.Vec2, speed, accel, dt float64) bodyMotion {
	dx, dy := move[0]*speed-body.vel[0], move[1]*speed-body.vel[1]
	if d := math.Hypot(dx, dy); d > 0 {
		scale := min(1, accel*dt/d)
		body.vel[0] += dx * scale
		body.vel[1] += dy * scale
	}
	body.pos = c.onGround(body.pos[0]+body.vel[0]*dt, body.pos[1]+body.vel[1]*dt)
	return body
}

func towards(p, q wire.Vec3) wire.Vec2 {
	d := distance(p, q)
	if d == 0 {
		return wire.Vec2{}
	}
	return wire.Vec2{(q[0] - p[0]) / d, (q[1] - p[1]) / d}
}

func pickupInReach(p, item wire.Vec3, r float64) bool {
	return distance(p, item) <= r && math.Abs(p[2]-item[2]) <= 4
}

// Evaluate only a direct walk. Integrate from observed velocity and re-steer
// at decision boundaries. A pickup must be reachable on an actual tick. Rank
// qualifying items by interpolated entry time inside that final motion step.
func (c *Controller) collectionMove(o wire.Observation, a wire.Action, enemy shot, walk growthWalk, carriers []wire.Actor, item wire.Vec3, r float64, weapon bool) (float64, wire.Vec2, bool) {
	s := o.Self
	maxSpeed := max(walk.speed, math.Hypot(s.Vel[0], s.Vel[1]))
	ground := c.onGround(item[0], item[1])
	if distance(s.Pos, item) > r+growthHorizon*maxSpeed || math.Abs(ground[2]-item[2]) > 4 ||
		!c.terrainWalk(s, s.Pos, ground) {
		return 0, wire.Vec2{}, false
	}
	body := bodyMotion{s.Pos, wire.Vec2{s.Vel[0], s.Vel[1]}}
	if pickupInReach(s.Pos, item, r) {
		if weapon {
			return 0, a.Move, margin(o.Zone, item, o.T, o.T) >= 0
		}
		// Cubes are automatic. Keep the original combat move when it still
		// leaves the cube collectible after the next movement step.
		normal := c.walkTick(body, a.Move, walk.speedAt(0), walk.accel, walk.tick)
		if pickupInReach(normal.pos, item, r) && c.terrainWalk(s, s.Pos, normal.pos) {
			return 0, a.Move, margin(o.Zone, item, o.T, o.T+walk.tick) >= 0
		}
	}

	original := body
	first := towards(s.Pos, item)
	if !c.heldShotsClear(o, a, enemy, body, first, walk) {
		return 0, wire.Vec2{}, false
	}
	move := first
	decideEvery := max(1, int(c.cfg.Match["decide_every"]))
	var arrival float64
	arrived := false
	// Collection can happen before the issued Move expires. Save its first
	// entry time and finish validating that held command before accepting it.
	for tick := 0; ; tick++ {
		elapsed := float64(tick) * walk.tick
		dt := walk.tick
		if !arrived {
			dt = min(dt, growthHorizon-elapsed)
			if dt <= 0 {
				break
			}
		}
		speed := walk.speedAt(elapsed)
		if tick%decideEvery == 0 {
			move = towards(body.pos, item)
		}
		next := c.walkTick(body, move, speed, walk.accel, dt)
		if !c.terrainWalk(s, body.pos, next.pos) || margin(o.Zone, next.pos, o.T, o.T+elapsed+dt) < 0 {
			break
		}
		if len(carriers) > 0 {
			normal := c.walkTick(original, a.Move, speed, walk.accel, dt)
			if c.walk(s, original.pos, normal.pos) {
				original = normal
			} else {
				original.vel = wire.Vec2{}
			}
			if c.entersLightning(s, carriers, next.pos, original.pos, elapsed+dt) {
				break
			}
		}
		if !arrived && pickupInReach(next.pos, item, r) {
			// Clip the last segment to the horizontal and vertical pickup limits.
			v := sub(next.pos, body.pos)
			length := math.Sqrt(dot(v, v))
			if length > 0 {
				dir := wire.Vec3{v[0] / length, v[1] / length, v[2] / length}
				bottom := item
				bottom[2] -= 4
				if entry, ok := cylinderEntry(body.pos, dir, bottom, r, 8, length); ok {
					arrived = true
					arrival = elapsed + dt*entry/length
					if margin(o.Zone, item, o.T, o.T+arrival) < 0 || margin(o.Zone, item, o.T, o.T+elapsed+dt) < 0 {
						return 0, wire.Vec2{}, false
					}
				}
			}
		}
		if arrived && tick+1 >= decideEvery {
			return arrival, first, true
		}
		body = next
	}
	return 0, wire.Vec2{}, false
}

// Predict every possible discharge of the held command before the next
// decision, even when the item would be collected on the first physics tick.
func (c *Controller) heldShotsClear(o wire.Observation, a wire.Action, enemy shot, body bodyMotion, move wire.Vec2, walk growthWalk) bool {
	nextShot := [2]float64{math.Inf(1), math.Inf(1)}
	for i, w := range o.Self.Weapons {
		if w == nil {
			continue
		}
		switch {
		case w.Kind == "gun" && a.Fire[i]:
			nextShot[i] = w.Cooldown
		case w.Kind == "laser" && !a.Fire[i] && (c.laser.held || c.laser.pending):
			nextShot[i] = max(0, c.weapon(w, "min_charge")-w.Charge)
		case w.Kind == "laser" && a.Fire[i] && c.laser.held:
			nextShot[i] = max(0, c.laser.started+c.weapon(w, "charge")+c.weapon(w, "hold_s")-o.T)
		}
	}

	const eps = 1e-9
	for tick := 0; tick < int(c.cfg.Match["decide_every"]) && min(nextShot[0], nextShot[1]) <= c.period+eps; tick++ {
		start := float64(tick) * walk.tick
		at := float64(tick+1) * walk.tick
		next := c.walkTick(body, move, walk.speedAt(start), walk.accel, walk.tick)
		if !c.terrainWalk(o.Self, body.pos, next.pos) {
			return false
		}
		body = next
		o.Self.Aim = c.nextAim(o, a.Aim)
		projected := enemy
		projected.origin = body.pos
		projected.origin[2] += c.stat(o.Self, "muzzle_height")
		projected.center = add(enemy.center, enemy.velocity, at)
		projected.pos = add(enemy.pos, enemy.velocity, at)
		for i, w := range o.Self.Weapons {
			if w == nil || math.IsInf(nextShot[i], 1) {
				continue
			}
			due := nextShot[i] <= at+eps
			if w.Kind == "gun" {
				// Retain the original first-tick safeguard as well as the
				// tick on which the observed cooldown actually clears.
				if (tick == 0 || due) && !c.gunAligned(o, w, projected, o.Self.Aim) {
					return false
				}
				if due {
					nextShot[i] += max(walk.tick, c.weapon(w, "cooldown"))
				}
			} else if due {
				if !c.laserRay(o, w, projected, o.Self.Aim, 0) {
					return false
				}
				nextShot[i] = math.Inf(1)
			}
		}
	}
	return true
}

func (c *Controller) entersLightning(s wire.Self, carriers []wire.Actor, proposed, original wire.Vec3, dt float64) bool {
	proposed[2] += c.stat(s, "height") / 2
	original[2] += c.stat(s, "height") / 2
	reach := c.cfg.Weapons["lightning"]["range"]
	for _, carrier := range carriers {
		muzzle := add(carrier.Pos, carrier.Vel, dt)
		muzzle[2] += c.cfg.Mech["muzzle_height"] * (1 + float64(carrier.Cubes)*c.cfg.Cubes.SizePerCube)
		toItem, toOriginal := sub(proposed, muzzle), sub(original, muzzle)
		if dot(toItem, toItem) <= reach*reach && dot(toOriginal, toOriginal) > reach*reach {
			return true
		}
	}
	return false
}
