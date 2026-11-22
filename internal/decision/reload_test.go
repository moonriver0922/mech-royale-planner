package decision

import (
	"math"
	"testing"

	"ownbot/internal/wire"
)

func TestReloadKeepsLoadedGunThroughOcclusion(t *testing.T) {
	c, o := sample(t, true)
	o.Self.Weapons[0].Ammo = 11
	c.Decide(o)
	o.T += 10
	o.Mechs = []wire.Actor{{ID: 8, Kind: "player", Pos: wire.Vec3{30, 0, 0}}}
	if a := c.Decide(o); a.Reload || a.Fire[0] {
		t.Fatalf("tracked player behind prism: %+v", a)
	}
	o.T += .1
	o.Mechs[0].Pos = wire.Vec3{0, 30, 0}
	o.Self.Aim = wire.Vec2{math.Pi / 2, math.Atan2(-2.5, 30)}
	if a := c.Decide(o); !a.Fire[0] || a.Reload {
		t.Fatalf("player clear of prism on next decision: %+v", a)
	}
}

func TestReloadNeedsObservedQuiet(t *testing.T) {
	c, o := sample(t, false)
	o.Self.Weapons[0].Ammo = 9
	for _, step := range []struct {
		at               float64
		projectile, want bool
	}{
		{10, false, false}, {11.5, true, false}, {11.6, false, false},
		{13.9, false, false}, {14.1, false, true},
	} {
		o.T = step.at
		o.Projectiles = nil
		if step.projectile {
			o.Projectiles = []wire.Projectile{{ID: 17, Kind: "gun"}}
		}
		if a := c.Decide(o); a.Reload != step.want {
			t.Fatalf("at %.1f projectile=%v: reload=%v, want %v", o.T, step.projectile, a.Reload, step.want)
		}
	}
	o.Self.Weapons[0].Ammo = 12
	if a := c.Decide(o); a.Reload {
		t.Fatal("full magazine requested reload")
	}
}

func TestReloadDefersAfterDamage(t *testing.T) {
	for _, event := range []string{"hit", "damaged"} {
		t.Run(event, func(t *testing.T) {
			c, o := sample(t, false)
			o.Self.Weapons[0].Ammo = 9
			c.Decide(o)
			o.T = 10
			o.Events = []wire.Event{{Type: event}}
			if a := c.Decide(o); a.Reload {
				t.Fatal("requested reload on combat event")
			}
			o.Events = nil
			for _, at := range []float64{10.1, 14.9, 15.1} {
				o.T = at
				if a := c.Decide(o); a.Reload != (at == 15.1) {
					t.Fatalf("unexpected reload=%v at %.1f", a.Reload, at)
				}
			}
		})
	}
}

func TestReloadEmptyGunWhileTracked(t *testing.T) {
	c, o := sample(t, true)
	o.Mechs = []wire.Actor{{ID: 8, Pos: wire.Vec3{30, 0, 0}}}
	o.Self.Weapons[0].Ammo = 0
	if a := c.Decide(o); !a.Reload {
		t.Fatal("empty gun did not request reload")
	}
	o.Self.Weapons[0].Reload = 1.5
	if a := c.Decide(o); a.Reload {
		t.Fatal("requested another reload while one was active")
	}
}

func TestReloadDefersWhileLiningUpCrate(t *testing.T) {
	c, o := sample(t, false)
	o.Self.Weapons[0].Ammo = 9
	c.Decide(o)
	o.T += 10
	o.Crates = []wire.Crate{{ID: 81, Pos: wire.Vec3{30, 0, 0}, HP: 1}}
	o.Self.Aim[0] = .5
	if a := c.Decide(o); a.Fire[0] || a.Reload || a.Aim[0] != 0 || a.Move != (wire.Vec2{}) {
		t.Fatalf("lining up crate: %+v", a)
	}
}
