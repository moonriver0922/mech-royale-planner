package mech

import (
	"math"
	"testing"
)

func TestTrackedChargeBehindCover(t *testing.T) {
	w := FlatWorld()
	b := Brain{W: w, strafe: 1}
	b.Cfg.Mech = MechStats{MuzzleHeight: 7}
	o := Obs{Self: Self{Pos: Vec3{0, 0, 0}, Stats: MechStats{Height: 9, Radius: 2.5}}}
	charging := MechSeen{Kind: "mech", Pos: Vec3{40, 0, 0}, Aim: [2]float64{math.Pi, 0}, Charging: true}
	wall := Prism{
		Kind: "building", MinX: 18, MaxX: 20, MinY: -6, MaxY: 6,
		Edges: []float64{18, -6, 20, -6, 20, -6, 20, 6, 20, 6, 18, 6, 18, 6, 18, -6},
	}
	for _, tc := range []struct {
		name   string
		height float64
		want   bool
	}{{"open", 0, true}, {"full cover", 14, false}, {"partly exposed", 7, true}} {
		t.Run(tc.name, func(t *testing.T) {
			wall.Top = tc.height
			var solids []Prism
			if tc.height > 0 {
				solids = []Prism{wall}
			}
			w.initGeo(solids, nil, Rules{})
			o.Mechs = []MechSeen{charging}
			x, y, dodge := b.dodge(&o)
			if dodge != tc.want || ((math.Hypot(x, y) > 0) != tc.want) {
				t.Fatalf("reaction %v %v %v", x, y, dodge)
			}
			if !tc.want {
				second := charging
				second.Pos, second.Aim = Vec3{0, 35, 0}, [2]float64{-math.Pi / 2, 0}
				o.Mechs = append(o.Mechs, second)
				if _, _, dodge = b.dodge(&o); !dodge {
					t.Fatal("lost the second, exposed opponent")
				}
				o.Mechs = o.Mechs[:1]
				o.Projectiles = []Proj{{Kind: "gun", Pos: Vec3{0, 12, 5}, Vel: Vec3{0, -100, 0}}}
				x, y, dodge = b.dodge(&o)
				if dodge || math.Hypot(x, y) == 0 {
					t.Fatal("lost independent projectile reaction")
				}
				o.Projectiles = nil
			}
		})
	}
}
