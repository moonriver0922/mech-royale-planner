package decision

import (
	"math"
	"strings"

	"ownbot/internal/arena"
	"ownbot/internal/wire"
)

type goal struct {
	kind string
	id   int
}

type Controller struct {
	laser        laserState
	cfg          wire.Config
	world        *arena.Map
	period       float64
	target       goal
	route        []wire.Vec3
	destination  wire.Vec3
	replanAt     float64
	recovering   bool
	coverValid   bool
	cover        wire.Vec3
	lastDamage   float64
	lastDealt    float64
	lastPosition wire.Vec3
	moving       bool
	blockedFor   float64
	failedTake   int
	retryTakeAt  float64
}

func NewController(cfg wire.Config, world *arena.Map) *Controller {
	return &Controller{
		cfg: cfg, world: world,
		period:     cfg.Match["decide_every"] / max(1, cfg.Match["tick_hz"]),
		lastDamage: math.Inf(-1), lastDealt: math.Inf(-1), failedTake: -1,
	}
}

func (c *Controller) stat(s wire.Self, k string) float64 {
	if v, ok := s.Stats[k]; ok {
		return v
	}
	return c.cfg.Mech[k]
}

func (c *Controller) update(o wire.Observation) {
	for _, e := range o.Events {
		switch e.Type {
		case "damaged":
			c.lastDamage = o.T
			c.coverValid = false
		case "hit":
			c.lastDealt = o.T
		case "take_failed":
			// IDs are JSON numbers for these events.
			if id, err := pickupID(e.ID); err == nil {
				c.failedTake = id
				c.retryTakeAt = o.T + 1
			}
		}
	}
	if c.moving && distance(o.Self.Pos, c.lastPosition) < c.stat(o.Self, "walk_speed")*c.period/5 {
		c.blockedFor += c.period
	} else {
		c.blockedFor = 0
	}
	c.lastPosition = o.Self.Pos
	if o.Self.HP >= .75*o.Self.MaxHP {
		c.recovering = false
		c.coverValid = false
	}
}

func (c *Controller) Decide(o wire.Observation) wire.Action {
	c.update(o)
	c.observeLaser(o)
	a := wire.Action{Tick: o.Tick, Aim: o.Self.Aim, Pick: c.upgrade(o)}
	s := o.Self
	shootingCrate, activePlayerCombat := false, false
	enemy, hasEnemy := c.bestEnemy(o)
	if hasEnemy {
		a.Aim = enemy.aim
	} else {
		a.Aim = c.scan(o)
	}

	if dest, urgent := c.zoneGoal(o); urgent {
		a.Move = c.follow(o, goal{kind: "zone"}, dest)
		a.Boost = a.Move != (wire.Vec2{}) && !s.InZone && s.Grounded &&
			s.Energy >= 2*c.stat(s, "boost_drain")*c.period
		if hasEnemy && !c.recovering {
			c.fire(o, enemy, &a, false)
		}
	} else if move, covered, escape := c.recover(o); covered || escape {
		a.Move = move
		if escape && hasEnemy {
			c.fire(o, enemy, &a, false)
		}
	} else if hasEnemy && enemy.time <= c.period*2 {
		activePlayerCombat = enemy.player
		a.Move = c.engage(o, enemy)
		c.fire(o, enemy, &a, true)
	} else if r, ok := c.resource(o); ok {
		a.Move = c.follow(o, r.goal, r.pos)
		switch r.kind {
		case "weapon":
			if distance(s.Pos, r.pos) <= c.cfg.Loot["pickup_radius"] && math.Abs(s.Pos[2]-r.pos[2]) <= 4 {
				a.Take = &r.id
			}
		case "crate":
			if shot, ok := c.solve(o, r.id, r.pos, wire.Vec3{}, c.cfg.Crates["height"], c.cfg.Crates["radius"], 0); ok && shot.time <= max(s.Weapons[0].Reload, 1) {
				shootingCrate = true
				a.Aim = shot.aim
				c.fire(o, shot, &a, false)
				a.Move = wire.Vec2{}
				c.target = r.goal
			}
		}
	} else if hasEnemy {
		activePlayerCombat = enemy.player
		a.Move = c.engage(o, enemy)
		c.fire(o, enemy, &a, true)
	} else {
		center, radius := circle(o.Zone, o.T, o.T+3)
		phase := math.Floor(o.T/9) + float64(s.ID)
		dest := c.onGround(center[0]+math.Cos(phase*2.4)*radius*.55, center[1]+math.Sin(phase*2.4)*radius*.55)
		a.Move = c.follow(o, goal{kind: "scout", id: int(phase)}, dest)
	}

	for i, w := range s.Weapons {
		if w == nil || w.Kind != "gun" || w.Reload > 0 || a.Fire[i] {
			continue
		}
		if w.Ammo == 0 || (!hasEnemy && !shootingCrate && float64(w.Ammo) < c.weapon(w, "mag")) {
			a.Reload = true
		}
	}
	c.rememberLaser(o, a)
	if activePlayerCombat && !a.Boost {
		a.Boost = c.burstSafer(o, a)
	}
	c.moving = a.Move != (wire.Vec2{})
	return a
}

func (c *Controller) scan(o wire.Observation) wire.Vec2 {
	for _, e := range o.Events {
		if e.Type == "damaged" && e.Bearing != nil {
			return wire.Vec2{*e.Bearing, 0}
		}
	}
	for _, s := range o.Sounds {
		if s.Dist == "near" && s.Age <= c.period && s.Kind != "step" {
			return wire.Vec2{s.Bearing, 0}
		}
	}
	return wire.Vec2{wrap(o.Self.Aim[0] + c.period), 0}
}

func (c *Controller) upgrade(o wire.Observation) *int {
	if o.Offer == nil || len(o.Offer.Choices) == 0 {
		return nil
	}
	index, best := 0, -1.
	for i, choice := range o.Offer.Choices {
		u := c.cfg.Upgrades[choice.ID]
		score := 0.
		for _, e := range u.Effects {
			base, weight := 0., 0.
			parts := strings.Split(e.Path, ".")
			switch {
			case len(parts) == 2 && parts[0] == "mech":
				base = c.stat(o.Self, parts[1])
				switch parts[1] {
				case "hp", "walk_speed", "regen_per_s":
					weight = 1
				case "armor":
					base, weight = 1-base, 1
				case "regen_delay", "turn_rate", "pitch_rate", "energy_regen":
					weight = .25
				case "energy":
					weight = .12
				}
			case len(parts) == 3 && parts[0] == "weapons":
				for _, w := range o.Self.Weapons {
					if w == nil || w.Kind != parts[1] {
						continue
					}
					base, weight = c.weapon(w, parts[2]), .25
					if parts[2] == "damage" || parts[2] == "cooldown" {
						weight = 1
					}
				}
			}
			mul := e.Mul
			if mul == 0 {
				mul = 1
			}
			if base > 0 {
				score += weight * math.Abs(mul-1+e.Add/base)
			}
		}
		if score > best {
			index, best = i, score
		}
	}
	return &index
}
