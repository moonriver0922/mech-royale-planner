// Package wire describes the JSON lines exchanged with the arena server.
package wire

import "encoding/json"

type Vec2 [2]float64
type Vec3 [3]float64

// Numbers exposes numeric balance settings while tolerating additional nested rules.
type Numbers map[string]float64

func (n *Numbers) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	out := make(Numbers, len(fields))
	for k, v := range fields {
		if len(v) == 0 || (v[0] != '-' && (v[0] < '0' || v[0] > '9')) {
			continue
		}
		var f float64
		if err := json.Unmarshal(v, &f); err != nil {
			return err
		}
		out[k] = f
	}
	*n = out
	return nil
}

type Effect struct {
	Path     string
	Mul, Add float64
}

type Upgrade struct {
	ID      string
	Effects []Effect
}

type Config struct {
	Match, Bot, Mech, NPC, Loot, Crates, Projectile, Vision, Sound, Drops Numbers
	Weapons                                                               map[string]map[string]float64
	Upgrades                                                              map[string]Upgrade
	Cubes                                                                 struct {
		PickupRadius  float64 `json:"pickup_radius"`
		SizePerCube   float64 `json:"size_per_cube"`
		StatCap       float64 `json:"stat_cap"`
		Thresholds    []int   `json:"upgrade_thresholds"`
		DamagePerCube float64 `json:"damage_per_cube"`
		HPPerCube     float64 `json:"hp_per_cube"`
	}
}

// UnmarshalJSON indexes the official array of upgrade definitions by ID.
func (c *Config) UnmarshalJSON(data []byte) error {
	type fields Config
	var raw struct {
		*fields
		Upgrades []Upgrade
	}
	raw.fields = (*fields)(c)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Upgrades = make(map[string]Upgrade, len(raw.Upgrades))
	for _, upgrade := range raw.Upgrades {
		c.Upgrades[upgrade.ID] = upgrade
	}
	return nil
}

type Hello struct {
	Protocol string
	Slot     int
	Seed     uint64
	Map      struct{ Dir string }
	Config   Config
}
type Weapon struct {
	Kind                     string
	Cooldown, Reload, Charge float64
	Ammo                     int
	Stats                    map[string]float64
}
type Self struct {
	ID         int
	Pos, Vel   Vec3
	Aim        Vec2
	HP, Energy float64
	MaxHP      float64 `json:"max_hp"`
	MaxEnergy  float64 `json:"max_energy"`
	Grounded   bool
	InZone     bool `json:"in_zone"`
	Cubes      int
	Upgrades   []string
	Weapons    [2]*Weapon
	Stats      map[string]float64
}
type Actor struct {
	ID       int
	Kind     string
	Pos, Vel Vec3
	Aim      Vec2
	HP       float64
	MaxHP    float64 `json:"max_hp"`
	Cubes    int
	Weapons  []string
	Charging bool
}
type Projectile struct {
	ID       int
	Kind     string
	Pos, Vel Vec3
}
type Crate struct {
	ID  int
	Pos Vec3
	HP  float64
}
type Cube struct {
	ID  int
	Pos Vec3
}
type Pickup struct {
	ID   int
	Kind string
	Pos  Vec3
}
type Sound struct {
	Kind, Dist   string
	Bearing, Age float64
}
type Beam struct {
	By   int
	Kind string
	Path []Vec3
	Age  float64
}
type Event struct {
	Type, Weapon, Effect, Kind, Reason string
	ID                                 json.RawMessage // Numeric for items and string for upgrades.
	By                                 *int
	Target, Slot, Cubes                int
	Amount                             float64
	Bearing                            *float64
	Rear                               bool
}
type Drop struct {
	ID               int
	Pos              Vec3
	Radius, Progress float64
	LandsIn          float64 `json:"lands_in"`
	CaptureS         float64 `json:"capture_s"`
	Holder           *int
}
type Zone struct {
	Center      Vec2
	Radius      float64
	NextCenter  Vec2    `json:"next_center"`
	NextRadius  float64 `json:"next_radius"`
	ShrinkStart float64 `json:"shrink_start"`
	ShrinkEnd   float64 `json:"shrink_end"`
	DPS         float64
}
type Choice struct{ ID, Name, Desc string }
type Offer struct {
	ID      int
	Expires float64
	Choices []Choice
}
type Observation struct {
	Tick, Alive int
	T           float64
	Self        Self
	Mechs       []Actor
	Projectiles []Projectile
	Crates      []Crate
	Cubes       []Cube
	Pickups     []Pickup
	Sounds      []Sound
	Beams       []Beam
	Events      []Event
	Drops       []Drop
	Zone        Zone
	Offer       *Offer
}
type Action struct {
	Tick   int     `json:"tick"`
	Move   Vec2    `json:"move"`
	Aim    Vec2    `json:"aim"`
	Fire   [2]bool `json:"fire"`
	Boost  bool    `json:"boost,omitempty"`
	Jump   bool    `json:"jump,omitempty"`
	Reload bool    `json:"reload,omitempty"`
	Pick   *int    `json:"pick,omitempty"`
	Take   *int    `json:"take,omitempty"`
	Drop   *int    `json:"drop,omitempty"`
}
