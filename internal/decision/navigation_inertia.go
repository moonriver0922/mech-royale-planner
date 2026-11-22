package decision

import (
	"math"

	"ownbot/internal/wire"
)

// Apply only after an ordinary navigation branch has finished assembling its
// action. Acceleration can carry the body across a wall even if the command's
// forward ray clears it. The original route and aim remain valid inputs.
func (c *Controller) navigationMove(o wire.Observation, a wire.Action) wire.Vec2 {
	s, p := o.Self, o.Self.Pos
	if !s.Grounded || !s.InZone || a.Move == (wire.Vec2{}) || a.Boost || a.Jump ||
		a.Fire != [2]bool{} || a.Take != nil || a.Pick != nil || c.laser.held || c.laser.pending {
		return a.Move
	}
	for _, w := range s.Weapons {
		if w != nil && w.Kind == "laser" && w.Charge > 0 {
			return a.Move
		}
	}
	if math.Abs(p[2]-c.world.GroundHeight(p[0], p[1])) > .1 || !c.walk(s, p, p) {
		return a.Move
	}

	speed, accel := c.stat(s, "walk_speed"), c.stat(s, "ground_accel")
	dt := 1 / max(1, c.cfg.Match["tick_hz"])
	ticks := int(c.cfg.Match["decide_every"])
	heldClear := func(move wire.Vec2, checkZone bool) bool {
		body := bodyMotion{p, wire.Vec2{s.Vel[0], s.Vel[1]}}
		for i := range ticks {
			next := c.walkTick(body, move, speed, accel, dt)
			if !c.walk(s, body.pos, next.pos) || (checkZone && margin(o.Zone, next.pos, o.T, o.T+float64(i+1)*dt) < 0) {
				return false
			}
			body = next
		}
		return true
	}
	// Only a predicted collision can trigger a replacement.
	if heldClear(a.Move, false) {
		return a.Move
	}
	yaw := math.Atan2(a.Move[1], a.Move[0])
	magnitude := math.Hypot(a.Move[0], a.Move[1])
	for _, turn := range stepTurns[1:] {
		unit := wire.Vec2{math.Cos(yaw + turn), math.Sin(yaw + turn)}
		room, clear := c.forwardStep(o, unit)
		if !clear || room < 0 {
			continue
		}
		move := wire.Vec2{unit[0] * magnitude, unit[1] * magnitude}
		if heldClear(move, true) {
			return move
		}
	}
	if heldClear(wire.Vec2{}, true) {
		return wire.Vec2{}
	}
	return a.Move
}
