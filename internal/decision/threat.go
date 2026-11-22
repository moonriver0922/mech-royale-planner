package decision

import (
	"math"

	"ownbot/internal/wire"
)

// Player IDs are the 16 match slots. NPC, self and environmental damage
// never establish a player to acquire. Only an observed bearing is stored.
type damageRecord struct {
	damage, rearDamage, last, bearing, acquireUntil float64
}

func (c *Controller) remember(o wire.Observation, e wire.Event) {
	if e.By == nil || e.Bearing == nil || e.Amount <= 0 || *e.By == o.Self.ID || *e.By < 0 || *e.By >= len(c.threats) {
		return
	}
	r := &c.threats[*e.By]
	decay := math.Exp(-max(0, o.T-r.last) / 1.1)
	r.damage = r.damage*decay + e.Amount
	r.rearDamage *= decay
	r.last, r.bearing = o.T, *e.Bearing
	if e.Rear {
		r.rearDamage += e.Amount
		if r.rearDamage >= .035*max(1, o.Self.MaxHP) {
			r.acquireUntil = o.T + 1.35
		}
	}
}

type engagement struct {
	shot                            shot
	canShoot, priority, bearingOnly bool
	id                              int
	aim                             wire.Vec2
}

func (c *Controller) engagement(o wire.Observation, normal engagement) engagement {
	id, best := -1, 0.
	for i, r := range c.threats {
		if r.acquireUntil <= o.T {
			continue
		}
		score := (r.damage + r.rearDamage) * math.Exp(-max(0, o.T-r.last)/1.1)
		if score > best {
			id, best = i, score
		}
	}
	if id >= 0 {
		for _, actor := range o.Mechs {
			if actor.ID != id {
				continue
			}
			s, canShoot := c.actorShot(o, actor)
			if canShoot {
				return engagement{shot: s, canShoot: true, priority: true, id: id, aim: s.aim}
			}
			v := sub(actor.Pos, o.Self.Pos)
			return engagement{priority: true, id: id, aim: wire.Vec2{math.Atan2(v[1], v[0]), 0}}
		}
		return engagement{priority: true, bearingOnly: true, id: id, aim: wire.Vec2{c.threats[id].bearing, 0}}
	}
	return normal
}
