package mech

import (
	"math"
	"strings"
)

type Intent struct {
	Kind  string
	Goal  Vec3
	Until float64
}

type Recovery struct {
	intent                 Intent
	lastDamage, lastThreat float64
}

func NewRecovery() *Recovery {
	return &Recovery{lastDamage: -100, lastThreat: -100}
}

func (s *Recovery) Apply(b *Brain, o *Obs, action Action) Action {
	incoming := false
	for _, event := range o.Events {
		if event.Amount > 0 && (event.Type == "damaged" || event.Type == "hit") {
			s.lastDamage = o.T
		}
		if event.Amount > 0 && event.Type == "damaged" {
			incoming, s.lastThreat = true, o.T
		}
	}
	next := s.choose(b, o, action, incoming)
	s.intent = next
	if next.Kind == "" {
		return action
	}
	output, _ := b.executeIntent(o, action, next)
	return output
}

func (s *Recovery) choose(b *Brain, o *Obs, action Action, incoming bool) Intent {
	me := &o.Self
	fraction := me.HP / math.Max(1, me.MaxHP)
	if action.Jump || !me.Grounded || !me.InZone || o.Alive <= 2 ||
		b.Mode == "escape" || b.Mode == "unstuck" || b.Mode == "dodge" ||
		strings.HasPrefix(b.Mode, "zone") || chargedLaser(me) ||
		incoming || o.T-s.lastThreat < 0.5 ||
		(b.Cfg.Match.DurationS > 0 && b.Cfg.Match.DurationS-o.T < 5) ||
		b.directThreat(o) {
		return Intent{}
	}
	if old := s.intent; old.Kind != "" && o.T < old.Until && fraction < 0.75 && b.covered(o, old.Goal) {
		return old
	}
	if fraction < 0.72 && o.T-s.lastThreat > 0.8 && me.Stats.RegenPerS > 0 {
		needed := math.Max(0, me.Stats.RegenDelay-(o.T-s.lastDamage)) + (0.75*me.MaxHP-me.HP)/me.Stats.RegenPerS
		if needed > 0 && needed < 8 {
			return b.coverIntent(o, needed+0.5)
		}
	}
	return Intent{}
}
func chargedLaser(me *Self) bool {
	_, laser := hasWeapon(me.Weapons, "laser")
	return laser != nil && laser.Charge > 0
}

func (b *Brain) directThreat(o *Obs) bool {
	for _, actor := range o.Mechs {
		track := b.tracks[actor.ID]
		if actor.Kind != "mech" {
			track = b.npcs[actor.ID]
			if track != nil && o.T-track.HitMe < 2 {
				return true
			}
			continue
		}
		if track == nil || !track.LOS {
			continue
		}
		toMe := math.Atan2(o.Self.Pos[1]-actor.Pos[1], o.Self.Pos[0]-actor.Pos[0])
		if track.Dist < 32 || o.T-track.HitMe < 3 ||
			(track.Dist < 90 && math.Abs(angDiff(actor.Aim[0], toMe)) < 0.30) {
			return true
		}
	}
	return false
}

func distanceXY(a, b Vec3) float64 { return hyp(a[0]-b[0], a[1]-b[1]) }

func (b *Brain) covered(o *Obs, point Vec3) bool {
	if o.T+7 >= b.zoneDeadline(&o.Zone, point[0], point[1], 8) {
		return false
	}
	for _, actor := range o.Mechs {
		if actor.Kind != "mech" || distanceXY(actor.Pos, point) > 155 {
			continue
		}
		target := Vec3{point[0], point[1], point[2] + o.Self.Stats.Height*0.7}
		eye := actor.Pos
		eye[2] += b.Cfg.Mech.MuzzleHeight * b.scaleOf(actor.Cubes)
		if b.W.Clear(eye, target) {
			return false
		}
	}
	return true
}

func (b *Brain) coverIntent(o *Obs, duration float64) Intent {
	me := &o.Self
	point := me.Pos
	if !b.covered(o, point) && b.coverOK {
		point = Vec3{b.coverGoal[0], b.coverGoal[1], b.W.WalkAt(b.coverGoal[0], b.coverGoal[1])}
	}
	if distanceXY(point, me.Pos) > 16 || !b.covered(o, point) ||
		!b.W.LineWalkable(me.Pos[0], me.Pos[1], point[0], point[1]) ||
		o.T+duration+3 >= b.zoneDeadline(&o.Zone, point[0], point[1], 8) {
		return Intent{}
	}
	return Intent{Kind: "recover", Goal: point, Until: o.T + duration}
}

func (b *Brain) executeIntent(o *Obs, original Action, intent Intent) (Action, Vec3) {
	me := &o.Self
	baseline := b.predictGroundMove(me, original)
	candidate := original
	d := distanceXY(me.Pos, intent.Goal)
	candidate.Move = [2]float64{}
	if d > 1 {
		factor := math.Min(1, d/math.Max(1, me.Stats.WalkSpeed*0.3))
		candidate.Move = [2]float64{factor * (intent.Goal[0] - me.Pos[0]) / d, factor * (intent.Goal[1] - me.Pos[1]) / d}
	}
	candidate.Boost = false
	candidate.Fire = [2]bool{}
	if _, gun := hasWeapon(me.Weapons, "gun"); gun != nil && gun.Reload <= 0 && gun.Ammo < gun.Stats.Mag {
		candidate.Reload = true
	}
	predicted := b.predictGroundMove(me, candidate)
	next := b.zoneAt(&o.Zone, o.T+0.1)
	if hyp(predicted[0]-next.X, predicted[1]-next.Y) > next.R-3 ||
		distanceXY(predicted, intent.Goal) > distanceXY(baseline, intent.Goal)+0.05 {
		return original, baseline
	}
	return candidate, predicted
}

func (b *Brain) predictGroundMove(me *Self, action Action) Vec3 {
	stats := me.Stats
	if stats.WalkSpeed <= 0 || stats.Radius <= 0 || stats.Height <= 0 || !me.Grounded {
		return me.Pos
	}
	hz := b.Cfg.Match.TickHz
	steps := b.Cfg.Match.DecideEvery
	if hz <= 0 || steps < 1 || steps > 4 {
		hz, steps = 20, 2
	}
	speed := stats.WalkSpeed
	if action.Boost && me.Energy > 0 {
		speed = stats.BoostSpeed
	}
	if chargedLaser(me) {
		speed *= stats.ChargeMoveMult
	}
	limit := math.Max(1, hyp(action.Move[0], action.Move[1]))
	targetX, targetY := action.Move[0]*speed/limit, action.Move[1]*speed/limit
	point := me.Pos
	vx, vy := me.Vel[0], me.Vel[1]
	for range steps {
		dx, dy := targetX-vx, targetY-vy
		blend := math.Min(1, stats.GroundAccel/hz/math.Max(1e-9, hyp(dx, dy)))
		vx, vy = vx+dx*blend, vy+dy*blend
		m := b.W.MoveAndSlide(point[0], point[1], point[2], stats.Radius, stats.Height, vx/hz, vy/hz, 0)
		point = Vec3{m.X, m.Y, m.Z}
	}
	return point
}
