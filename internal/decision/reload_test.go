package decision

import (
	"math"
	"testing"

	"ownbot/internal/wire"
)

func TestGunKeepsNearFullMagazineWhenPlayerRoundsCorner(t *testing.T) {
	c, o := sample(t, true)
	c.cfg.Weapons["gun"]["reload"] = 2.4
	o.Self.Weapons[0].Ammo = 10
	o.Self.Pos = wire.Vec3{0, 19.9, 0}
	o.Self.Aim = wire.Vec2{math.Atan2(.6, 19), math.Atan2(-2.5, math.Hypot(.6, 19))}
	o.Mechs = []wire.Actor{{ID: 7, Kind: "mech", HP: 500, Pos: wire.Vec3{19, 20.2, 0}}}
	blocked := c.Decide(o)
	if blocked.Fire[0] || blocked.Reload {
		t.Errorf("blocked player with 10/12 rounds: fire=%v reload=%v", blocked.Fire[0], blocked.Reload)
	}

	// Apply the game's reload lockout only if the first command started it.
	o.Tick += 2
	o.T += .1
	if blocked.Reload {
		o.Self.Weapons[0].Reload = 2.3
	}
	o.Mechs[0].Pos[1] = 20.5
	if next := c.Decide(o); !next.Fire[0] || next.Reload {
		t.Errorf("aligned player cleared wall next observation: fire=%v reload=%v", next.Fire[0], next.Reload)
	}
}

func TestPartialReloadBudgetAndActorEligibility(t *testing.T) {
	live := wire.Actor{ID: 1, Kind: "mech", HP: 500, Pos: wire.Vec3{25, 0, 0}}
	dead := live
	dead.HP = 0
	npc := live
	npc.ID, npc.Kind = 2, "npc"
	players := []wire.Actor{live}
	cases := []struct {
		name       string
		ammo       int
		actors     []wire.Actor
		stats      map[string]float64
		remaining  float64
		wantReload bool
	}{
		{"below", 9, players, nil, 0, true},
		{"equal", 10, players, nil, 0, false},
		{"above", 11, players, nil, 0, false},
		{"NPC only", 10, []wire.Actor{npc}, nil, 0, true},
		{"dead player", 10, []wire.Actor{dead}, nil, 0, true},
		{"NPC then live player", 10, []wire.Actor{npc, live}, nil, 0, false},
		{"no actor", 10, nil, nil, 0, true},
		{"empty", 0, players, nil, 0, true},
		{"full", 12, nil, nil, 0, false},
		{"in progress", 9, players, nil, 1.2, false},
		{"upgraded cooldown below budget", 10, players, map[string]float64{"cooldown": .20}, 0, true},
		{"upgraded reload at budget", 8, players, map[string]float64{"reload": 1.92}, 0, false},
		{"upgraded mag can top off", 12, nil, map[string]float64{"mag": 15}, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, o := sample(t, true)
			c.cfg.Weapons["gun"]["reload"] = 2.4
			o.Mechs = tc.actors
			w := o.Self.Weapons[0]
			w.Ammo, w.Stats, w.Reload = tc.ammo, tc.stats, tc.remaining
			a := c.Decide(o)
			if a.Reload != tc.wantReload || a.Fire[0] {
				t.Fatalf("got reload=%v fire=%v, want reload=%v and no fire", a.Reload, a.Fire[0], tc.wantReload)
			}
		})
	}
}

func TestPartialReloadWaitsWhileAimingAtCrate(t *testing.T) {
	c, o := sample(t, false)
	c.cfg.Weapons["gun"]["reload"] = 2.4
	c.cfg.Crates = wire.Numbers{"height": 4, "radius": 2}
	o.Self.Weapons[0].Ammo = 9
	o.Self.Aim[0] = .45
	o.Crates = []wire.Crate{{ID: 8, Pos: wire.Vec3{30, 0, 0}, HP: 1}}
	a := c.Decide(o)
	if c.target != (goal{"crate", 8}) || a.Fire[0] || a.Reload || a.Aim[0] != 0 {
		t.Fatalf("expected to aim at crate without reloading, got target=%+v action=%+v", c.target, a)
	}
}
