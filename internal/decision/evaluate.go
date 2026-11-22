package decision

import (
	"math"
	"sort"

	"ownbot/internal/wire"
)

const (
	horizon = .5
	steps   = 10
	dt      = horizon / steps
)

type groundPath [steps + 1]wire.Vec3

// Advance the observed horizontal velocity toward the desired velocity with
// the configured acceleration. Every short swept segment must be walkable.
func (c *Controller) groundPath(o wire.Observation, move wire.Vec2) (groundPath, bool) {
	var path groundPath
	s := o.Self
	path[0] = s.Pos
	vx, vy := s.Vel[0], s.Vel[1]
	speed, accel := c.stat(s, "walk_speed"), c.stat(s, "ground_accel")
	for i := 1; i <= steps; i++ {
		dx, dy := move[0]*speed-vx, move[1]*speed-vy
		l := math.Hypot(dx, dy)
		if l > 0 {
			scale := min(1, accel*dt/l)
			vx, vy = vx+scale*dx, vy+scale*dy
		}
		p := path[i-1]
		q := c.onGround(p[0]+vx*dt, p[1]+vy*dt)
		if !c.walk(s, p, q) || margin(o.Zone, q, o.T, o.T+float64(i)*dt) < 0 {
			return path, false
		}
		path[i] = q
	}
	return path, true
}

type bulletPath struct {
	point        groundPath
	count        int
	radius       float64
	lastFraction float64
	blast        float64
}

// Shortlist by closest horizontal approach to our current center. Clip the
// selected shots at their first static impact before comparing candidate moves.
func (c *Controller) nearbyShots(o wire.Observation) []bulletPath {
	type approach struct {
		p          wire.Projectile
		separation float64
	}
	var near []approach
	radius := c.stat(o.Self, "radius")
	reach := horizon*max(c.stat(o.Self, "walk_speed"), math.Hypot(o.Self.Vel[0], o.Self.Vel[1])) + radius + 2
	for _, p := range o.Projectiles {
		d := sub(p.Pos, o.Self.Pos)
		v := p.Vel
		t := min(horizon, max(0, -(d[0]*v[0]+d[1]*v[1])/max(1e-9, v[0]*v[0]+v[1]*v[1])))
		miss := math.Hypot(d[0]+v[0]*t, d[1]+v[1]*t)
		if p.Kind == "mortar" {
			miss -= c.cfg.Weapons["mortar"]["splash_radius"]
		}
		if miss <= reach {
			near = append(near, approach{p, miss})
		}
	}
	sort.Slice(near, func(i, j int) bool {
		if near[i].separation == near[j].separation {
			return near[i].p.ID < near[j].p.ID
		}
		return near[i].separation < near[j].separation
	})
	var shots []bulletPath
	for _, entry := range near[:min(len(near), 8)] {
		p := entry.p
		b := bulletPath{radius: c.cfg.Projectile[p.Kind+"_radius"], lastFraction: 1}
		b.point[0] = p.Pos
		for i := 1; i <= steps; i++ {
			t := float64(i) * dt
			q := add(p.Pos, p.Vel, t)
			if p.Kind == "mortar" {
				q[2] -= .5 * c.cfg.Weapons["mortar"]["gravity"] * t * t
			}
			prev := b.point[i-1]
			b.count = i
			if c.world.ClearShot(prev, q) {
				b.point[i] = q
				continue
			}
			delta := sub(q, prev)
			lo, hi := 0., 1.
			for range 5 {
				mid := (lo + hi) / 2
				if c.world.ClearShot(prev, add(prev, delta, mid)) {
					lo = mid
				} else {
					hi = mid
				}
			}
			b.lastFraction = lo
			b.point[i] = add(prev, delta, lo)
			if p.Kind == "mortar" {
				b.blast = c.cfg.Weapons["mortar"]["splash_radius"]
			}
			break
		}
		shots = append(shots, b)
	}
	return shots
}

func bulletRisk(path *groundPath, b *bulletPath, radius, height float64) float64 {
	worst := 0.
	for i := 1; i <= b.count; i++ {
		end := path[i]
		if i == b.count && b.lastFraction < 1 {
			end = add(path[i-1], sub(end, path[i-1]), b.lastFraction)
		}
		a := sub(b.point[i-1], path[i-1])
		z := sub(b.point[i], end)
		v := sub(z, a)
		r := radius + b.radius
		lo, hi := 0., 1.
		if math.Abs(v[2]) < 1e-9 {
			if a[2] < -b.radius || a[2] > height+b.radius {
				continue
			}
		} else {
			t0, t1 := (-b.radius-a[2])/v[2], (height+b.radius-a[2])/v[2]
			lo, hi = max(lo, min(t0, t1)), min(hi, max(t0, t1))
			if lo > hi {
				continue
			}
		}
		u := min(hi, max(lo, -(a[0]*v[0]+a[1]*v[1])/max(1e-9, v[0]*v[0]+v[1]*v[1])))
		dist := math.Hypot(a[0]+u*v[0], a[1]+u*v[1])
		worst = max(worst, 40*max(0, 1-dist/(r+1.5)))
	}
	if b.blast > 0 {
		i := b.count
		end := add(path[i-1], sub(path[i], path[i-1]), b.lastFraction)
		d := sub(b.point[i], end)
		d[2] = max(0, -d[2], d[2]-height)
		worst = max(worst, 32*max(0, 1-math.Sqrt(dot(d, d))/(radius+b.blast)))
	}
	return worst
}

// Use actual reported positions for exposure, limiting map rays to the
// nearest three actors. A bearing has no range and cannot certify cover.
func (c *Controller) nearMuzzles(o wire.Observation) []wire.Vec3 {
	var nearest []wire.Actor
	for _, m := range o.Mechs {
		i := sort.Search(len(nearest), func(i int) bool {
			a, b := distance(nearest[i].Pos, o.Self.Pos), distance(m.Pos, o.Self.Pos)
			return a > b || (a == b && nearest[i].ID > m.ID)
		})
		if i >= 3 {
			continue
		}
		if len(nearest) < 3 {
			nearest = append(nearest, wire.Actor{})
		}
		copy(nearest[i+1:], nearest[i:])
		nearest[i] = m
	}
	var origins []wire.Vec3
	for _, m := range nearest {
		origins = append(origins, c.threatOrigin(m))
	}
	return origins
}

func (c *Controller) chooseMove(o wire.Observation, base wire.Vec2, intent engagement, combat bool) wire.Vec2 {
	s := o.Self
	// On a roof the terrain grid cannot give the supporting floor height.
	if math.Abs(s.Pos[2]-c.world.GroundHeight(s.Pos[0], s.Pos[1])) > .3 {
		return base
	}
	shots := c.nearbyShots(o)
	if !combat && len(shots) == 0 {
		return base
	}
	origins := c.nearMuzzles(o)
	radius, height := c.stat(s, "radius"), c.stat(s, "height")
	yaw := intent.aim[0]
	candidates := [10]wire.Vec2{base, {}}
	for i := range 8 {
		a := yaw + float64(i)*math.Pi/4
		candidates[i+2] = wire.Vec2{math.Cos(a), math.Sin(a)}
	}
	best, lowest := base, math.Inf(1)
	for _, m := range candidates {
		path, ok := c.groundPath(o, m)
		if !ok {
			continue
		}
		cost := math.Hypot(m[0]-base[0], m[1]-base[1])
		for i := range shots {
			cost += bulletRisk(&path, &shots[i], radius, height)
		}
		end := path[steps]
		displacement := sub(end, s.Pos)
		end[2] += height / 2
		for _, origin := range origins {
			if c.world.ClearShot(origin, end) {
				dx, dy := origin[0]-s.Pos[0], origin[1]-s.Pos[1]
				lateral := math.Abs(displacement[0]*dy-displacement[1]*dx) / max(1, math.Hypot(dx, dy))
				cost += 4 - .14*min(10, lateral)
			}
		}
		if intent.bearingOnly {
			lateral := math.Abs(displacement[0]*math.Sin(yaw) - displacement[1]*math.Cos(yaw))
			cost -= .4 * min(10, lateral)
		}
		if cost < lowest {
			best, lowest = m, cost
		}
	}
	return best
}
