package decision

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"ownbot/internal/arena"
	"ownbot/internal/wire"
)

// Check the emitted vector at each physics tick, including the tick that
// finishes a held decision. Carry velocity even when input is zero.
func inspectHeld(c *Controller, o wire.Observation, move wire.Vec2) (bodyMotion, int) {
	body := bodyMotion{o.Self.Pos, wire.Vec2{o.Self.Vel[0], o.Self.Vel[1]}}
	badTick := 0
	for i := 1; i <= int(c.cfg.Match["decide_every"]); i++ {
		next := c.walkTick(body, move, c.stat(o.Self, "walk_speed"), c.stat(o.Self, "ground_accel"), 1/c.cfg.Match["tick_hz"])
		if badTick == 0 && !c.walk(o.Self, body.pos, next.pos) {
			badTick = i
		}
		body = next
	}
	return body, badTick
}

func TestInertiaRecordedWitnesses(t *testing.T) {
	dir := os.Getenv("S19_RECORDINGS")
	if dir == "" {
		t.Skip("set S19_RECORDINGS to the directory of the three own S09 recordings")
	}
	for _, w := range []struct {
		file   string
		tick   int
		nextXY wire.Vec2
	}{
		{"48009-s09-for-s14-vs-s09-observations.jsonl", 1516, wire.Vec2{29.012142888667665, 138.7998622574736}},
		{"45027-s09-timing-observations.jsonl", 1132, wire.Vec2{-480.2415380846894, -89.26049005934664}},
		{"49018-s09-navigation-observations.jsonl", 948, wire.Vec2{-253.91176723586162, 46.742940997043405}},
	} {
		t.Run(w.file[:5], func(t *testing.T) {
			f, err := os.Open(filepath.Join(dir, w.file))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			scan := bufio.NewScanner(f)
			scan.Buffer(make([]byte, 65536), 1048576)
			var c *Controller
			matched := 0
			for scan.Scan() {
				var row struct{ Input, Output json.RawMessage }
				if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
					t.Fatal(err)
				}
				if c == nil {
					var hello wire.Hello
					if err := json.Unmarshal(row.Input, &hello); err != nil {
						t.Fatal(err)
					}
					dir := hello.Map.Dir
					if path := os.Getenv("S19_ARENA"); path != "" {
						dir = path
					}
					world, err := arena.LoadMap(dir)
					if err != nil {
						t.Fatal(err)
					}
					c = NewController(hello.Config, world)
					continue
				}
				var o wire.Observation
				var recorded wire.Action
				if err := json.Unmarshal(row.Input, &o); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(row.Output, &recorded); err != nil {
					t.Fatal(err)
				}
				action := c.Decide(o)
				if o.Tick < w.tick {
					if !reflect.DeepEqual(action, recorded) {
						t.Fatalf("prefix diverged at tick %d", o.Tick)
					}
					matched++
					continue
				}
				if o.Tick != w.tick {
					t.Fatalf("missed tick %d", w.tick)
				}
				base, bad := inspectHeld(c, o, recorded.Move)
				if !c.walk(o.Self, o.Self.Pos, o.Self.Pos) || bad == 0 || math.Hypot(base.pos[0]-w.nextXY[0], base.pos[1]-w.nextXY[1]) > 1e-9 {
					t.Fatalf("wrong witness: original bad tick %d, predicted next %v", bad, base.pos)
				}
				next, changedBad := inspectHeld(c, o, action.Move)
				t.Logf("%d prefix matches, tick %d, original predicted first invalid substep %d; candidate move %v, next center %v", matched, w.tick, bad, action.Move, next.pos)
				if changedBad != 0 || action.Move == (wire.Vec2{}) || math.Abs(math.Hypot(action.Move[0], action.Move[1])-1) > 1e-9 {
					t.Fatalf("need a nonzero, clear replacement, got %v, bad tick %d", action.Move, changedBad)
				}
				recorded.Move = action.Move
				if !reflect.DeepEqual(action, recorded) {
					t.Fatal("changed another action field")
				}
				return
			}
			t.Fatalf("incomplete recording, scan error %v", scan.Err())
		})
	}
}

// The resource lies parallel to a wall. This observed approach velocity
// crosses the wall's expanded footprint on the second tick of a full move.
func wallApproach(t *testing.T, offset float64) (*Controller, wire.Observation) {
	t.Helper()
	c, o := sample(t, true)
	o.Self.Pos, o.Self.Vel = wire.Vec3{2.26, 0, 0}, wire.Vec3{4, 0, 0}
	o.Cubes = []wire.Cube{{ID: 7, Pos: wire.Vec3{2.26, offset, 0}}}
	return c, o
}

func TestInertiaResourcePreservesMagnitudeAndRoute(t *testing.T) {
	for _, distance := range []float64{7, 1.8} {
		c, o := wallApproach(t, distance)
		scale := math.Min(1, distance/2.4)
		// Adjust remaining clearance for the shorter move's faster braking.
		if scale < 1 {
			o.Self.Pos[0] = 2.29
			o.Cubes[0].Pos[0] = o.Self.Pos[0]
		}
		parentMove := wire.Vec2{math.Cos(math.Pi/2) * scale, scale}
		_, bad := inspectHeld(c, o, parentMove)
		if bad != 2 {
			t.Fatalf("fixture needs a second-tick crossing, distance %g, first bad tick %d", distance, bad)
		}
		o.Self.Weapons[0].Ammo = 3
		got := c.Decide(o)
		next, bad := inspectHeld(c, o, got.Move)
		if got.Move[0] >= 0 || bad != 0 || math.Abs(math.Hypot(got.Move[0], got.Move[1])-scale) > 1e-10 || !got.Reload {
			t.Fatalf("distance %g: move %v, next %v, first collision %d, reload %t", distance, got.Move, next.pos, bad, got.Reload)
		}
		if c.target != (goal{"cube", 7}) || c.destination != o.Cubes[0].Pos || len(c.route) != 1 || c.route[0] != c.destination {
			t.Fatalf("changed resource route %v %v", c.target, c.route)
		}
	}
}

func TestInertiaKeepsEligibleClearInputsExactly(t *testing.T) {
	c, o := wallApproach(t, 7)
	o.Self.Vel = wire.Vec3{-2, 1, 0}
	for _, move := range []wire.Vec2{{0, 1}, {-.123456789, .345678901}, {}, {-1e-12, 2e-12}} {
		a := wire.Action{Tick: 40, Move: move, Aim: wire.Vec2{1, -.2}, Reload: true}
		if _, bad := inspectHeld(c, o, move); bad != 0 {
			t.Fatalf("bad clear fixture %v", move)
		}
		if got := c.navigationMove(o, a); got != move {
			t.Errorf("legal input %v changed to %v", move, got)
		}
	}
}

func TestInertiaSkipsPriorityActionsAndUnsupportedBodies(t *testing.T) {
	mutations := map[string]func(*Controller, *wire.Observation, *wire.Action){
		"boost":      func(_ *Controller, _ *wire.Observation, a *wire.Action) { a.Boost = true },
		"jump":       func(_ *Controller, _ *wire.Observation, a *wire.Action) { a.Jump = true },
		"gun firing": func(_ *Controller, _ *wire.Observation, a *wire.Action) { a.Fire[0] = true },
		"laser rising trigger": func(_ *Controller, o *wire.Observation, a *wire.Action) {
			o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"}
			a.Fire[1] = true
		},
		"take":                  func(_ *Controller, _ *wire.Observation, a *wire.Action) { a.Take = new(int) },
		"upgrade":               func(_ *Controller, _ *wire.Observation, a *wire.Action) { a.Pick = new(int) },
		"airborne":              func(_ *Controller, o *wire.Observation, _ *wire.Action) { o.Self.Grounded = false },
		"elevated support":      func(_ *Controller, o *wire.Observation, _ *wire.Action) { o.Self.Pos[2] = .11 },
		"outside":               func(_ *Controller, o *wire.Observation, _ *wire.Action) { o.Self.InZone = false },
		"overlapping footprint": func(_ *Controller, o *wire.Observation, _ *wire.Action) { o.Self.Pos[0] = 2.51 },
		"observed charge": func(_ *Controller, o *wire.Observation, _ *wire.Action) {
			o.Self.Weapons[1] = &wire.Weapon{Kind: "laser", Charge: .25}
		},
		"held charge": func(c *Controller, o *wire.Observation, _ *wire.Action) {
			o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"}
			c.laser = laserState{present: true, slot: 1, held: true}
		},
		"pending release": func(c *Controller, o *wire.Observation, _ *wire.Action) {
			o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"}
			c.laser = laserState{present: true, slot: 1, pending: true}
		},
	}
	for label, mutate := range mutations {
		t.Run(label, func(t *testing.T) {
			c, o := wallApproach(t, 7)
			a := wire.Action{Tick: o.Tick, Move: wire.Vec2{0, 1}}
			if c.navigationMove(o, a) == a.Move {
				t.Fatal("control must exercise a replacement")
			}
			mutate(c, &o, &a)
			if c.navigationMove(o, a) != a.Move {
				t.Fatal("changed excluded input")
			}
		})
	}
}

func TestInertiaRetainsFullForwardClearance(t *testing.T) {
	c, o := wallApproach(t, 7)
	// The first clear short alternative would put the full lookahead beyond
	// the world boundary with a 2.5 m body. A later turn has enough room.
	c.world.Bounds.MaxY = 5.3
	a := wire.Action{Move: wire.Vec2{0, 1}}
	shortOnly := wire.Vec2{-math.Sin(.5), math.Cos(.5)}
	if _, bad := inspectHeld(c, o, shortOnly); bad != 0 {
		t.Fatal("first alternative must pass held motion")
	}
	look := func(m wire.Vec2) wire.Vec3 { return c.onGround(o.Self.Pos[0]+3.6*m[0], o.Self.Pos[1]+3.6*m[1]) }
	if c.walk(o.Self, o.Self.Pos, look(shortOnly)) {
		t.Fatal("boundary fixture must reject the lookahead")
	}
	got := c.navigationMove(o, a)
	if got == a.Move || got == (wire.Vec2{}) || !c.walk(o.Self, o.Self.Pos, look(got)) {
		t.Fatalf("invalid lookahead with move %v", got)
	}
}

func TestInertiaBrakesOnlyIfInertialBrakePathIsClear(t *testing.T) {
	c, o := wallApproach(t, 7)
	o.Zone = wire.Zone{Center: wire.Vec2{o.Self.Pos[0], o.Self.Pos[1]}, Radius: .75}
	a := wire.Action{Move: wire.Vec2{0, 1}}
	for _, tc := range []struct {
		vx         float64
		want       wire.Vec2
		brakeClear bool
	}{{4, wire.Vec2{}, true}, {18, a.Move, false}} {
		o.Self.Vel[0] = tc.vx
		_, bad := inspectHeld(c, o, wire.Vec2{})
		if tc.brakeClear != (bad == 0) {
			t.Fatal("brake fixture changed")
		}
		if got := c.navigationMove(o, a); got != tc.want {
			t.Errorf("vx %g: got %v want %v", tc.vx, got, tc.want)
		}
	}
}

func TestInertiaUsesConfiguredTicksAndEffectiveStats(t *testing.T) {
	c, o := wallApproach(t, 7)
	a := wire.Action{Move: wire.Vec2{0, 1}}
	c.cfg.Match["decide_every"] = 1
	if got := c.navigationMove(o, a); got != a.Move {
		t.Fatal("second tick is outside this hold")
	}
	c.cfg.Match["decide_every"] = 3
	o.Self.Pos[0] = 2.21
	if _, bad := inspectHeld(c, o, a.Move); bad != 3 {
		t.Fatalf("need first collision on third tick, got %d", bad)
	}
	if got := c.navigationMove(o, a); got == a.Move {
		t.Fatal("third held step was ignored")
	}
	o.Self.Stats["ground_accel"] = 160
	if got := c.navigationMove(o, a); got != a.Move {
		t.Fatal("higher effective acceleration clears the original path")
	}
	o.Self.Stats["ground_accel"] = 60
	o.Self.Stats["walk_speed"] = 3
	if got := c.navigationMove(o, a); got != a.Move {
		t.Fatal("lower effective speed clears the original path")
	}
}

func TestInertiaDoesNotChangeZonePriorityOrCombat(t *testing.T) {
	t.Run("zone", func(t *testing.T) {
		c, o := wallApproach(t, 7)
		o.Zone = wire.Zone{Center: wire.Vec2{2.26, 0}, Radius: 90, NextCenter: wire.Vec2{2.26, 50}, NextRadius: 10, ShrinkStart: o.T, ShrinkEnd: o.T + 2.5}
		d, urgent := c.zoneGoal(o)
		if !urgent {
			t.Fatal("need an in-zone urgent decision")
		}
		parent := c.follow(o, goal{kind: "zone"}, d)
		if got := c.navigationMove(o, wire.Action{Move: parent}); got == parent {
			t.Fatalf("need an otherwise avoidable collision, parent=%v", parent)
		}
		got := c.Decide(o)
		if c.target.kind != "zone" || got.Move != parent {
			t.Fatalf("changed urgent zone move from %v to %v", parent, got.Move)
		}
	})
	t.Run("combat", func(t *testing.T) {
		c, o := wallApproach(t, 7)
		o.Cubes = nil
		o.Self.ID = 1
		o.Mechs = []wire.Actor{{ID: 8, Kind: "mech", Pos: wire.Vec3{-38, 0, 0}}}
		enemy, ok := c.bestEnemy(o)
		if !ok {
			t.Fatal("need combat")
		}
		a := wire.Action{Tick: o.Tick, Aim: enemy.aim}
		c.engageWithGrowth(o, enemy, &a)
		if c.navigationMove(o, a) == a.Move {
			t.Fatalf("need an otherwise avoidable collision, parent=%+v", a)
		}
		got := c.Decide(o)
		if c.target.kind != "enemy" || got != a {
			t.Fatalf("changed combat action from %+v to %+v", a, got)
		}
	})
}
