package arena

import (
	"container/heap"
	"math"
	"ownbot/internal/wire"
)

type key [2]int
type node struct {
	k     key
	g, f  float64
	order int
}
type frontier []node

func (h frontier) Len() int { return len(h) }
func (h frontier) Less(i, j int) bool {
	if h[i].f != h[j].f {
		return h[i].f < h[j].f
	}
	return h[i].order < h[j].order
}
func (h frontier) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *frontier) Push(v any)   { *h = append(*h, v.(node)) }
func (h *frontier) Pop() any     { n := len(*h) - 1; v := (*h)[n]; *h = (*h)[:n]; return v }

// Route searches a bounded eight-neighbor ground lattice after checking the
// direct path. Callers can retain the waypoints across decisions.
func (m *Map) Route(from, to wire.Vec3, radius, height float64) ([]wire.Vec3, bool) {
	if !m.within(from, radius) || !m.within(to, radius) {
		return nil, false
	}
	walk := func(a, b wire.Vec3) bool {
		return m.Segment(a, b, radius, height, m.Rules.MaxStep, m.Rules.MaxSlopeDeg)
	}
	if walk(from, to) {
		return []wire.Vec3{to}, true
	}
	const stride = 10.
	pos := func(k key) wire.Vec3 {
		if k == (key{}) {
			return from
		}
		x, y := from[0]+float64(k[0])*stride, from[1]+float64(k[1])*stride
		return wire.Vec3{x, y, m.GroundHeight(x, y)}
	}
	dist := func(p wire.Vec3) float64 { return math.Hypot(p[0]-to[0], p[1]-to[1]) }
	cost := map[key]float64{{}: 0}
	parent := map[key]key{}
	open := frontier{{f: dist(from)}}
	seq := 0
	for expanded := 0; open.Len() > 0 && expanded < 160; {
		n := heap.Pop(&open).(node)
		if cost[n.k] < n.g {
			continue
		}
		expanded++
		p := pos(n.k)
		if dist(p) < stride*2.25 && walk(p, to) {
			path := []wire.Vec3{to}
			for k := n.k; k != (key{}); k = parent[k] {
				path = append(path, pos(k))
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path, true
		}
		for _, d := range [...]key{{1, 0}, {0, 1}, {-1, 0}, {0, -1}, {1, 1}, {-1, 1}, {-1, -1}, {1, -1}} {
			k := key{n.k[0] + d[0], n.k[1] + d[1]}
			v := pos(k)
			g := n.g + math.Hypot(v[0]-p[0], v[1]-p[1])
			if old, ok := cost[k]; ok && g >= old {
				continue
			}
			if !walk(p, v) {
				continue
			}
			cost[k] = g
			parent[k] = n.k
			seq++
			heap.Push(&open, node{k, g, g + 1.2*dist(v), seq})
		}
	}
	return nil, false
}
