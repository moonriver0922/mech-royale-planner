package decision

import (
	"math"

	"ownbot/internal/wire"
)

const burstHorizon = .3

type warningRay struct {
	muzzle, dir wire.Vec3
}

func (c *Controller) defensiveBoost(o wire.Observation, a wire.Action, enemy shot) bool {
	s := o.Self
	if !s.Grounded || !s.InZone || a.Take != nil || a.Jump || a.Move == (wire.Vec2{}) ||
		s.Weapons[0] == nil || s.Weapons[0].Kind != "gun" || s.Weapons[1] != nil ||
		math.Abs(s.Pos[2]-c.world.GroundHeight(s.Pos[0], s.Pos[1])) > .1 ||
		s.Energy < c.stat(s, "boost_drain")*burstHorizon {
		return false
	}

	var rays []warningRay
	player := false
	for _, actor := range o.Mechs {
		if actor.Kind != "mech" {
			continue
		}
		player = player || actor.ID == enemy.id
		if !actor.Charging {
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
	if !player || len(rays) == 0 {
		return false
	}
	hz := c.cfg.Match["tick_hz"]
	steps := int(math.Round(burstHorizon * hz))
	if steps < 1 || steps > 32 || math.Abs(float64(steps)-burstHorizon*hz) > 1e-9 {
		return false
	}
	move := a.Move
	length := max(1, math.Hypot(move[0], move[1]))
	move[0], move[1] = move[0]/length, move[1]/length
	start := bodyMotion{s.Pos, wire.Vec2{s.Vel[0], s.Vel[1]}}
	walk, burst := start, start
	dt, accel := 1/hz, c.stat(s, "ground_accel")
	walkSpeed, boostSpeed := c.stat(s, "walk_speed"), c.stat(s, "boost_speed")
	radius, height := c.stat(s, "radius"), c.stat(s, "height")
	reach := c.cfg.Weapons["laser"]["range"]
	hits := func(ray warningRay, feet wire.Vec3) bool {
		entry, ok := cylinderEntry(ray.muzzle, ray.dir, feet, radius, height, reach)
		return ok && c.world.ClearShot(ray.muzzle, add(ray.muzzle, ray.dir, entry))
	}
	improved := false
	for step := 1; step <= steps; step++ {
		when := o.T + float64(step)*dt
		nextWalk := c.walkTick(walk, move, walkSpeed, accel, dt)
		nextBurst := c.walkTick(burst, move, boostSpeed, accel, dt)
		if !c.terrainWalk(s, walk.pos, nextWalk.pos) || !c.terrainWalk(s, burst.pos, nextBurst.pos) ||
			margin(o.Zone, nextWalk.pos, o.T, when) < 0 || margin(o.Zone, nextBurst.pos, o.T, when) < 0 {
			return false
		}
		walk, burst = nextWalk, nextBurst
		for _, ray := range rays {
			walkHit, burstHit := hits(ray, walk.pos), hits(ray, burst.pos)
			if burstHit && !walkHit {
				return false
			}
			improved = improved || walkHit && !burstHit
		}
	}
	// A gun-only loadout has no charge slowdown. Validate every scheduled
	// discharge of the held action from its boosted, accelerating origin.
	return improved && c.heldShotsClear(o, a, enemy, start, move, growthWalk{tick: dt, speed: boostSpeed, accel: accel})
}
