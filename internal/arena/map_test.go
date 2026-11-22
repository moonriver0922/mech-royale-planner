package arena

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"ownbot/internal/wire"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) *Map {
	t.Helper()
	dir := t.TempDir()
	m := Map{Bounds: bounds{0, 60, 0, 60}, Terrain: terrain{File: "terrain.i16", Step: 1, Scale: .01, Nx: 61, Ny: 61},
		Prisms: []prism{{Base: 5, Top: 7, Ring: []float64{20, 10, 30, 10, 30, 40, 20, 40}, Holes: [][]float64{{22, 20, 28, 20, 28, 30, 22, 30}}}}}
	m.Rules.MaxStep = 1.5
	m.Rules.MaxSlopeDeg = 50
	b, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "world.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	raw := make([]byte, 61*61*2)
	binary.LittleEndian.PutUint16(raw[2*(61+1):], uint16(400))
	if e = os.WriteFile(filepath.Join(dir, "terrain.i16"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	got, e := LoadMap(dir)
	if e != nil {
		t.Fatal(e)
	}
	return got
}
func TestTerrainAndRay(t *testing.T) {
	m := fixture(t)
	if got := m.GroundHeight(.5, .5); got != 1 {
		t.Fatalf("height %g", got)
	}
	cases := []struct {
		a, b  wire.Vec3
		clear bool
	}{
		{wire.Vec3{10, 15, 4}, wire.Vec3{40, 15, 4}, true}, {wire.Vec3{10, 15, 6}, wire.Vec3{40, 15, 6}, false},
		{wire.Vec3{25, 25, 2}, wire.Vec3{25, 25, 10}, true}, {wire.Vec3{21, 25, 2}, wire.Vec3{21, 25, 10}, false},
		{wire.Vec3{0, 1, 3}, wire.Vec3{2, 1, 3}, false},
	}
	for _, c := range cases {
		if m.ClearShot(c.a, c.b) != c.clear {
			t.Errorf("ray %v %v want clear %t", c.a, c.b, c.clear)
		}
	}
	// Along this diagonal h(t)=4t(1-t), so a ray can graze away from both grid vertices.
	m.grid[1] = 400
	m.grid[62] = 0
	if m.ClearShot(wire.Vec3{0, 0, .24}, wire.Vec3{1, 1, 2.24}) {
		t.Fatal("missed interior terrain intersection")
	}
}
func TestGroundMotionAndDetour(t *testing.T) {
	m := fixture(t)
	a, b := wire.Vec3{10, 15, 0}, wire.Vec3{40, 15, 0}
	if !m.Segment(a, b, 1, 4, 1.5, 50) || m.Segment(a, b, 1, 9, 1.5, 50) {
		t.Fatal("bridge head clearance")
	}
	if m.Segment(wire.Vec3{19.5, 15, 0}, wire.Vec3{19.5, 35, 0}, 1, 9, 1.5, 50) {
		t.Fatal("radius clips bridge")
	}
	if m.Segment(wire.Vec3{.5, 15, 0}, a, 1, 4, 1.5, 50) {
		t.Fatal("body exceeds bounds")
	}
	if m.Segment(wire.Vec3{0, 1, 0}, wire.Vec3{1, 1, 4}, 0, 4, 10, 50) {
		t.Fatal("steep uphill")
	}
	path, ok := m.Route(a, b, 1, 9)
	if !ok || len(path) < 2 || path[len(path)-1] != b {
		t.Fatalf("detour %v %v", path, ok)
	}
	for _, p := range path {
		if !m.Segment(a, p, 1, 9, 1.5, 50) {
			t.Fatal("invalid route edge")
		}
		a = p
	}
}

func TestStepHeight(t *testing.T) {
	m := fixture(t)
	p := &m.Prisms[0]
	p.Base = 0
	a, b := wire.Vec3{18, 15, 0}, wire.Vec3{24, 15, 0}
	p.Top = 1
	if !m.Segment(a, b, 1, 9, 1.5, 50) {
		t.Fatal("legal step rejected")
	}
	p.Top = 2
	if m.Segment(a, b, 1, 9, 1.5, 50) {
		t.Fatal("high step accepted")
	}
}
