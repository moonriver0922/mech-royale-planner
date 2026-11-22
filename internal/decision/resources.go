package decision

import (
	"encoding/json"
	"math"

	"ownbot/internal/wire"
)

type resource struct {
	goal
	pos wire.Vec3
	eta float64
}

func pickupID(raw json.RawMessage) (int, error) {
	var id int
	err := json.Unmarshal(raw, &id)
	return id, err
}

func (c *Controller) pickupAllowed(o wire.Observation, item wire.Pickup) bool {
	primary := o.Self.Weapons[0]
	return (primary == nil || primary.Kind != item.Kind) &&
		(item.ID != c.failedTake || o.T >= c.retryTakeAt)
}

func (c *Controller) pickupReach(self, pos wire.Vec3) (float64, bool) {
	d := distance(self, pos)
	return d, d <= c.cfg.Loot["pickup_radius"] && math.Abs(self[2]-pos[2]) <= 4
}

func (c *Controller) nearbyLaser(o wire.Observation) *int {
	if o.Self.Weapons[1] != nil {
		return nil
	}
	bestID, bestDistance := 0, math.Inf(1)
	for _, item := range o.Pickups {
		if item.Kind != "laser" || !c.pickupAllowed(o, item) {
			continue
		}
		d, inReach := c.pickupReach(o.Self.Pos, item.Pos)
		if inReach && (d < bestDistance || d == bestDistance && item.ID < bestID) {
			bestDistance, bestID = d, item.ID
		}
	}
	if math.IsInf(bestDistance, 1) {
		return nil
	}
	return &bestID
}

func (c *Controller) resource(o wire.Observation) (resource, bool) {
	best := resource{eta: math.Inf(1)}
	s := o.Self
	consider := func(kind string, id int, pos wire.Vec3, extra float64) {
		if math.Abs(pos[2]-c.world.GroundHeight(pos[0], pos[1])) > 4 {
			return
		}
		p := c.onGround(pos[0], pos[1])
		eta := c.travel(o, distance(s.Pos, p)) + extra
		if margin(o.Zone, p, o.T, o.T+eta) < 0 || eta > best.eta {
			return
		}
		g := goal{kind, id}
		if !c.walk(s, s.Pos, p) {
			if g == c.target && len(c.route) == 0 {
				return
			}
			// The selected fallback gets a bounded route search in follow.
			eta += c.travel(o, 20)
		}
		if eta < best.eta || (eta == best.eta && id < best.id) {
			best = resource{g, pos, eta}
		}
	}
	canGrow := float64(s.Cubes) < c.cfg.Cubes.StatCap
	for _, threshold := range c.cfg.Cubes.Thresholds {
		if s.Cubes+1 == threshold {
			canGrow = true
		}
	}
	if canGrow {
		for _, item := range o.Cubes {
			consider("cube", item.ID, item.Pos, 0)
		}
	}
	if s.Weapons[1] == nil {
		for _, item := range o.Pickups {
			if !c.pickupAllowed(o, item) {
				continue
			}
			equip := c.cfg.Loot["equip_s"]
			switch item.Kind {
			case "laser":
				equip = max(equip, c.cfg.Weapons["laser"]["cooldown"])
			case "lightning":
			default:
				continue
			}
			consider("weapon", item.ID, item.Pos, equip)
		}
	}
	if best.kind == "" && canGrow && s.Weapons[0] != nil && s.Weapons[0].Kind == "gun" {
		w := s.Weapons[0]
		for _, crate := range o.Crates {
			damage := c.weapon(w, "damage") * (1 + c.cfg.Cubes.DamagePerCube*min(float64(s.Cubes), c.cfg.Cubes.StatCap))
			shots := math.Ceil(crate.HP / max(1, damage))
			extra := shots * c.weapon(w, "cooldown")
			if shots > float64(w.Ammo) {
				extra += max(w.Reload, c.weapon(w, "reload"))
			}
			consider("crate", crate.ID, crate.Pos, extra)
		}
	}
	return best, best.kind != ""
}
