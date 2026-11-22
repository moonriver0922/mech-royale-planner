// Package arena models static geometry from the v2 competition map.
package arena

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"ownbot/internal/wire"
	"path/filepath"
)

type bounds struct{ MinX, MaxX, MinY, MaxY float64 }
type terrain struct {
	File                    string
	MinX, MinY, Step, Scale float64
	Nx, Ny                  int
}
type prism struct {
	Base, Top float64
	Ring      []float64
	Holes     [][]float64
	box       bounds
}
type Map struct {
	Bounds  bounds
	Terrain terrain
	Rules   struct{ MaxStep, MaxSlopeDeg float64 }
	Prisms  []prism
	grid    []int16
	bins    map[[2]int][]int
}

func LoadMap(dir string) (*Map, error) {
	path := filepath.Join(dir, "v2", "world.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		path = filepath.Join(dir, "world.json")
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	m := new(Map)
	if err = json.Unmarshal(data, m); err != nil {
		return nil, err
	}
	t := m.Terrain
	if t.Nx < 2 || t.Ny < 2 || t.Nx > 10000 || t.Ny > 10000 || t.Step <= 0 || t.Scale <= 0 {
		return nil, fmt.Errorf("invalid terrain metadata")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), t.File))
	if err != nil {
		return nil, err
	}
	if len(raw) != 2*t.Nx*t.Ny {
		return nil, fmt.Errorf("terrain length %d", len(raw))
	}
	m.grid = make([]int16, len(raw)/2)
	for i := range m.grid {
		m.grid[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	m.bins = make(map[[2]int][]int)
	for i := range m.Prisms {
		p := &m.Prisms[i]
		if len(p.Ring) < 6 || len(p.Ring)%2 != 0 {
			return nil, fmt.Errorf("invalid prism %d", i)
		}
		for _, hole := range p.Holes {
			if len(hole) < 6 || len(hole)%2 != 0 {
				return nil, fmt.Errorf("invalid prism hole %d", i)
			}
		}
		b := bounds{p.Ring[0], p.Ring[0], p.Ring[1], p.Ring[1]}
		for j := 2; j < len(p.Ring); j += 2 {
			b.MinX = math.Min(b.MinX, p.Ring[j])
			b.MaxX = math.Max(b.MaxX, p.Ring[j])
			b.MinY = math.Min(b.MinY, p.Ring[j+1])
			b.MaxY = math.Max(b.MaxY, p.Ring[j+1])
		}
		p.box = b
		for x := bin(b.MinX); x <= bin(b.MaxX); x++ {
			for y := bin(b.MinY); y <= bin(b.MaxY); y++ {
				k := [2]int{x, y}
				m.bins[k] = append(m.bins[k], i)
			}
		}
	}
	return m, nil
}
func bin(x float64) int               { return int(math.Floor(x / 16)) }
func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(v, hi)) }
func (m *Map) GroundHeight(x, y float64) float64 {
	t := m.Terrain
	u := clamp((x-t.MinX)/t.Step, 0, float64(t.Nx-1))
	v := clamp((y-t.MinY)/t.Step, 0, float64(t.Ny-1))
	i := min(int(u), t.Nx-2)
	j := min(int(v), t.Ny-2)
	a := u - float64(i)
	b := v - float64(j)
	k := j*t.Nx + i
	return ((1-b)*((1-a)*float64(m.grid[k])+a*float64(m.grid[k+1])) + b*((1-a)*float64(m.grid[k+t.Nx])+a*float64(m.grid[k+t.Nx+1]))) * t.Scale
}
func (m *Map) within(p wire.Vec3, r float64) bool {
	b := m.Bounds
	return p[0] >= b.MinX+r && p[0] <= b.MaxX-r && p[1] >= b.MinY+r && p[1] <= b.MaxY-r && !math.IsNaN(p[2]) && !math.IsInf(p[2], 0)
}
func mix(a, b wire.Vec3, t float64) wire.Vec3 {
	return wire.Vec3{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
}
func inside(r []float64, x, y float64) bool {
	v := false
	for i, j := 0, len(r)-2; i < len(r); j, i = i, i+2 {
		a, b, c, d := r[j], r[j+1], r[i], r[i+1]
		if (b > y) != (d > y) && x < (c-a)*(y-b)/(d-b)+a {
			v = !v
		}
	}
	return v
}
func (p *prism) contains(x, y float64) bool {
	if !inside(p.Ring, x, y) {
		return false
	}
	for _, h := range p.Holes {
		if inside(h, x, y) {
			return false
		}
	}
	return true
}
func dist2(x, y, a, b, c, d float64) float64 {
	u, v := c-a, d-b
	t := 0.
	if n := u*u + v*v; n > 0 {
		t = clamp(((x-a)*u+(y-b)*v)/n, 0, 1)
	}
	u = x - a - t*u
	v = y - b - t*v
	return u*u + v*v
}
func edgeHit(ring []float64, a, b wire.Vec3, r float64) bool {
	x, y, u, v := a[0], a[1], b[0], b[1]
	rr := r*r + 1e-13
	for i, j := 0, len(ring)-2; i < len(ring); j, i = i, i+2 {
		c, d, e, f := ring[j], ring[j+1], ring[i], ring[i+1]
		if math.Max(c, e) < math.Min(x, u)-r || math.Min(c, e) > math.Max(x, u)+r || math.Max(d, f) < math.Min(y, v)-r || math.Min(d, f) > math.Max(y, v)+r {
			continue
		}
		s := (u-x)*(d-y) - (v-y)*(c-x)
		t := (u-x)*(f-y) - (v-y)*(e-x)
		g := (e-c)*(y-d) - (f-d)*(x-c)
		h := (e-c)*(v-d) - (f-d)*(u-c)
		if (s*t < 0 && g*h < 0) || dist2(x, y, c, d, e, f) <= rr || dist2(u, v, c, d, e, f) <= rr || dist2(c, d, x, y, u, v) <= rr || dist2(e, f, x, y, u, v) <= rr {
			return true
		}
	}
	return false
}
func (p *prism) hits(a, b wire.Vec3, r float64) bool {
	if math.Max(a[0], b[0])+r < p.box.MinX || math.Min(a[0], b[0])-r > p.box.MaxX || math.Max(a[1], b[1])+r < p.box.MinY || math.Min(a[1], b[1])-r > p.box.MaxY {
		return false
	}
	if p.contains(a[0], a[1]) || edgeHit(p.Ring, a, b, r) {
		return true
	}
	for _, h := range p.Holes {
		if edgeHit(h, a, b, r) {
			return true
		}
	}
	return false
}

// cells splits a segment wherever it crosses a terrain grid line. Within each
// resulting interval, bilinear elevation along the segment is quadratic.
func (m *Map) cells(a, b wire.Vec3, fn func(wire.Vec3, wire.Vec3) bool) bool {
	next := [2]float64{math.Inf(1), math.Inf(1)}
	delta := next
	for axis, o := range []float64{m.Terrain.MinX, m.Terrain.MinY} {
		d := b[axis] - a[axis]
		if d == 0 {
			continue
		}
		u := (a[axis] - o) / m.Terrain.Step
		n := math.Floor(u) + 1
		if d < 0 {
			n = math.Ceil(u) - 1
		}
		next[axis] = (n - u) * m.Terrain.Step / d
		delta[axis] = m.Terrain.Step / math.Abs(d)
	}
	prev := a
	for {
		t := math.Min(1, math.Min(next[0], next[1]))
		end := mix(a, b, t)
		if !fn(prev, end) {
			return false
		}
		if t >= 1 {
			return true
		}
		for i := range next {
			if next[i] <= t+1e-12 {
				next[i] += delta[i]
			}
		}
		prev = end
	}
}
func (m *Map) nearby(a, b wire.Vec3, r float64, fn func(*prism) bool) bool {
	for x := bin(math.Min(a[0], b[0]) - r); x <= bin(math.Max(a[0], b[0])+r); x++ {
		for y := bin(math.Min(a[1], b[1]) - r); y <= bin(math.Max(a[1], b[1])+r); y++ {
			for _, i := range m.bins[[2]int{x, y}] {
				if !fn(&m.Prisms[i]) {
					return false
				}
			}
		}
	}
	return true
}
func (m *Map) ClearShot(a, b wire.Vec3) bool {
	if !m.within(a, 0) || !m.within(b, 0) {
		return false
	}
	return m.cells(a, b, func(l, h wire.Vec3) bool {
		mid := mix(l, h, .5)
		v0 := l[2] - m.GroundHeight(l[0], l[1])
		v1 := h[2] - m.GroundHeight(h[0], h[1])
		vm := mid[2] - m.GroundHeight(mid[0], mid[1])
		if math.Min(v0, v1) <= 0 || vm <= 0 {
			return false
		}
		q := 2 * (v0 + v1 - 2*vm)
		s := v1 - v0 - q
		if q > 0 {
			u := -s / (2 * q)
			if u > 0 && u < 1 && v0+u*(s+u*q) <= 0 {
				return false
			}
		}
		return m.nearby(l, h, 0, func(p *prism) bool {
			if math.Max(l[2], h[2]) < p.Base || math.Min(l[2], h[2]) > p.Top {
				return true
			}
			lo, hi := 0., 1.
			if dz := h[2] - l[2]; dz != 0 {
				u, v := (p.Base-l[2])/dz, (p.Top-l[2])/dz
				lo = clamp(math.Min(u, v), 0, 1)
				hi = clamp(math.Max(u, v), 0, 1)
			}
			return !p.hits(mix(l, h, lo), mix(l, h, hi), 0)
		})
	})
}
func (m *Map) Segment(a, b wire.Vec3, r, height, step, slope float64) bool {
	if r < 0 || height <= 0 || step < 0 || !m.within(a, r) || !m.within(b, r) {
		return false
	}
	feet := a[2]
	maxGrade := math.Tan(slope * math.Pi / 180)
	return m.cells(a, b, func(l, h wire.Vec3) bool {
		z0 := m.GroundHeight(l[0], l[1])
		z1 := m.GroundHeight(h[0], h[1])
		mid := mix(l, h, .5)
		zm := m.GroundHeight(mid[0], mid[1])
		q := 2 * (z0 + z1 - 2*zm)
		maxRise := z1 - z0 + math.Abs(q)
		length := math.Hypot(h[0]-l[0], h[1]-l[1])
		if maxRise > maxGrade*length+1e-6 && z0 >= feet-.05 {
			return false
		}
		floor := z1
		ok := m.nearby(l, h, r, func(p *prism) bool {
			if p.Top < math.Min(z0, z1)-.01 || p.Base >= math.Max(feet, math.Max(z0, z1))+height || !p.hits(l, h, r) {
				return true
			}
			if p.Top > math.Max(feet, z0)+step+.001 {
				return false
			}
			if p.hits(h, h, r) {
				floor = math.Max(floor, p.Top)
			}
			return true
		})
		feet = floor
		return ok
	})
}
