package decision

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"

	"ownbot/internal/arena"
	"ownbot/internal/wire"
)

func damageFrom(id int, amount float64) wire.Event {
	return wire.Event{Type: "damaged", By: &id, Amount: amount}
}

func crossfireScene(t *testing.T) (*Controller, wire.Observation) {
	t.Helper()
	c, o := growthScene(t, true)
	c.cfg.Mech["rear_mult"] = 1.5
	c.cfg.Weapons["gun"]["damage"], c.cfg.Weapons["gun"]["reload"] = 45, 2.4
	c.cfg.Weapons["laser"] = map[string]float64{"damage": 315, "range": 280, "charge_turn_mult": 1}
	o.Self.HP = 600
	o.Self.Pos = wire.Vec3{-12, 22, 0}
	o.Self.Weapons[0].Cooldown = .15
	o.Self.Aim = wire.Vec2{0, -.047}
	o.Mechs = []wire.Actor{{ID: 1, Kind: "mech", HP: 1400, Pos: wire.Vec3{40, 22, 0}}, {ID: 15, Kind: "mech", HP: 1400, Pos: wire.Vec3{45, 24, 0}}}
	o.Events = []wire.Event{damageFrom(1, 200), damageFrom(15, 200)}
	return c, o
}

func TestCrossfireRedirectsOnlyMove(t *testing.T) {
	c, o := crossfireScene(t)
	baseline, plain := crossfireScene(t)
	plain.Events = nil
	want := baseline.Decide(plain)
	got := c.Decide(o)
	if got.Move == want.Move || got.Move[1] >= 0 {
		t.Fatalf("expected walk behind south side of wall, got %v, baseline %v", got.Move, want.Move)
	}
	got.Move = want.Move
	if !reflect.DeepEqual(got, want) || c.target != baseline.target || c.recovering != baseline.recovering || c.destination != baseline.destination || !reflect.DeepEqual(c.route, baseline.route) {
		t.Fatalf("baseline action or route changed: %+v %+v", got, want)
	}
}

func replayCrossfire(t *testing.T, env string, check func(*Controller, *Controller, wire.Observation, wire.Action, wire.Action, wire.Action) bool) {
	t.Helper()
	path := os.Getenv(env)
	if path == "" {
		t.Skipf("set %s to the archived seed 45002 JSONL", env)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 65536), 2<<20)
	var full, noBeam *Controller
	for sc.Scan() {
		var row struct {
			Input  json.RawMessage
			Output wire.Action
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		var tag struct{ Type string }
		if err := json.Unmarshal(row.Input, &tag); err != nil {
			t.Fatal(err)
		}
		if tag.Type == "hello" {
			var h wire.Hello
			if err := json.Unmarshal(row.Input, &h); err != nil {
				t.Fatal(err)
			}
			dir := os.Getenv("S14_ARENA")
			if dir == "" {
				dir = h.Map.Dir
			}
			m, err := arena.LoadMap(dir)
			if err != nil {
				t.Fatal(err)
			}
			full, noBeam = NewController(h.Config, m), NewController(h.Config, m)
			continue
		}
		if tag.Type != "obs" {
			continue
		}
		var o wire.Observation
		if err := json.Unmarshal(row.Input, &o); err != nil {
			t.Fatal(err)
		}
		got := full.Decide(o)
		// The held gun-only controller has no other use for opponents' charging flags.
		for i := range o.Mechs {
			o.Mechs[i].Charging = false
		}
		ablation := noBeam.Decide(o)
		if check(full, noBeam, o, got, ablation, row.Output) {
			return
		}
	}
	t.Fatalf("witness absent: %v", sc.Err())
}

func TestCrossfireRecordedS09(t *testing.T) {
	replayCrossfire(t, "S14_S09_TRACE", func(full, noBeam *Controller, o wire.Observation, got, ablation, recorded wire.Action) bool {
		if !reflect.DeepEqual(ablation, recorded) {
			t.Fatalf("B=0 differs from recorded S09 at tick %d: %+v, want %+v", o.Tick, ablation, recorded)
		}
		if o.Tick == 434 {
			if !full.crossfire.active || full.crossfire.requiredLaser != 14 || math.Abs(got.Move[0]-.981) > .001 || math.Abs(got.Move[1]+.192) > .001 ||
				math.Abs(full.crossfire.dest[0]+107.62) > .01 || math.Abs(full.crossfire.dest[1]-78.10) > .01 {
				t.Fatalf("unexpected witness move %v, destination %v", got.Move, full.crossfire.dest)
			}
			got.Move = recorded.Move
		}
		if !reflect.DeepEqual(got, recorded) {
			t.Fatalf("unexpected action change at tick %d: %+v", o.Tick, got)
		}
		return o.Tick == 434
	})
}

func TestCrossfireRecordedS11(t *testing.T) {
	replayCrossfire(t, "S14_S11_TRACE", func(full, noBeam *Controller, o wire.Observation, got, ablation, recorded wire.Action) bool {
		// Recorded S11 used another policy. Only its observations feed the two S09-derived controllers.
		if o.Tick < 458 {
			if full.crossfire.active || noBeam.crossfire.active {
				t.Fatalf("early break at tick %d", o.Tick)
			}
			return false
		}
		if o.Tick != 458 || !reflect.DeepEqual(got, ablation) || !noBeam.crossfire.active || full.crossfire.requiredLaser != -1 ||
			math.Abs(full.crossfire.dest[0]+118.69) > .01 || math.Abs(full.crossfire.dest[1]-86.82) > .01 {
			t.Fatalf("unexpected actual-hit witness tick %d, move %v, destination %v", o.Tick, got.Move, full.crossfire.dest)
		}
		return true
	})
}

func TestCrossfireDamageWindowAndBound(t *testing.T) {
	c, o := crossfireScene(t)
	o.Self.ID = 3
	o.Events = []wire.Event{damageFrom(9, 7), damageFrom(9, 8), damageFrom(15, 0), damageFrom(-1, 99), damageFrom(3, 99), damageFrom(105, 99), {Type: "damaged", Amount: 100}, {Type: "hit", By: new(int), Amount: 100}}
	c.recordIncoming(o)
	if d, tick := c.crossfire.damageBy(9); d != 15 || tick != 40 {
		t.Fatalf("initial evidence %v @ %d", d, tick)
	}
	if d, _ := c.crossfire.damageBy(3); d != 0 {
		t.Fatal("self counted")
	}
	o.T, o.Tick, o.Events = 4.3, 86, []wire.Event{damageFrom(9, 4), damageFrom(15, 6)}
	c.recordIncoming(o)
	if d, tick := c.crossfire.damageBy(9); d != 19 || tick != 86 {
		t.Fatalf("inside window %v @ %d", d, tick)
	}
	o.T, o.Tick, o.Events = 4.4, 88, nil
	c.recordIncoming(o)
	if d, _ := c.crossfire.damageBy(9); d != 4 {
		t.Fatalf("open left boundary retained old hit: %v", d)
	}
	o.Events = []wire.Event{damageFrom(15, 1)}
	for k := 0; k < 100; k++ {
		o.T = 10 + float64(k)/10
		o.Tick = 200 + k*2
		c.recordIncoming(o)
	}
	if n := len(c.crossfire.history); n != 24 || c.crossfire.count != 24 {
		t.Fatalf("history capacity=%d count=%d", n, c.crossfire.count)
	}
	if d, _ := c.crossfire.damageBy(15); d != 24 {
		t.Fatalf("wrapped window %v", d)
	}
	o.T += 3
	o.Events = nil
	c.recordIncoming(o)
	if c.crossfire.count != 0 {
		t.Fatal("stale evidence survived skipped observations")
	}
}

func TestCrossfireGunFinishBound(t *testing.T) {
	c, o := crossfireScene(t)
	c.cfg.Cubes.DamagePerCube = .04
	o.Self.Cubes = 20 // capped at ten, observed body geometry remains enlarged independently
	w := o.Self.Weapons[0]
	w.Stats = map[string]float64{"damage": 50, "mag": 3, "cooldown": .2, "reload": 1.5, "speed": 100}
	target := shot{distance: 105.5, radius: 2.5}
	for _, tc := range []struct {
		name             string
		shots, ammo      int
		cd, reload, want float64
	}{
		{"unspent magazine", 3, 3, .15, 0, 1.55},
		{"two magazine transitions", 7, 1, .15, 0, 4.95},
		{"empty", 4, 0, .15, 0, 4.4},
		{"reloading", 4, 0, .15, .7, 3.6},
		{"cooldown exceeds remaining reload", 2, 0, .8, .1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w.Ammo, w.Cooldown, w.Reload = tc.ammo, tc.cd, tc.reload
			target.hp = float64(tc.shots)*105 - 1
			got, ok := c.gunFinish(o, target)
			if !ok || math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("finish %v want %v", got, tc.want)
			}
		})
	}
	target.hp = 105
	if _, ok := c.gunFinish(o, target); ok {
		t.Fatal("single round must win")
	}
	target.hp = 1e18
	if got, ok := c.gunFinish(o, target); !ok || math.IsInf(got, 0) || got <= 0 {
		t.Fatalf("large HP: %v", got)
	}
}

func TestCrossfireRequiresDistinctCurrentPlayerLanes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Controller, *wire.Observation)
	}{
		{"one source", func(c *Controller, o *wire.Observation) {
			o.Events = []wire.Event{damageFrom(1, 500), damageFrom(1, 500)}
		}},
		{"source no longer visible", func(c *Controller, o *wire.Observation) { o.Mechs = o.Mechs[:1] }},
		{"second ray blocked", func(c *Controller, o *wire.Observation) { o.Mechs[1].Pos = wire.Vec3{45, 0, 0} }},
		{"NPC", func(c *Controller, o *wire.Observation) { o.Mechs[1].Kind = "npc" }},
		{"one round", func(c *Controller, o *wire.Observation) { o.Mechs[0].HP = 67.5 }},
		{"unknown target HP", func(c *Controller, o *wire.Observation) {
			for i := range o.Mechs {
				o.Mechs[i].HP = 0
			}
		}},
		{"insufficient incoming", func(c *Controller, o *wire.Observation) { o.Events = []wire.Event{damageFrom(1, 1), damageFrom(15, 1)} }},
		{"no cover", func(c *Controller, o *wire.Observation) { flat, _ := sample(t, false); c.world = flat.world }},
		{"outside zone", func(c *Controller, o *wire.Observation) { o.Self.InZone = false }},
		{"airborne", func(c *Controller, o *wire.Observation) { o.Self.Grounded = false }},
		{"recovery", func(c *Controller, o *wire.Observation) { o.Self.HP = 449 }},
		{"empty gun slot", func(c *Controller, o *wire.Observation) { o.Self.Weapons[0] = nil }},
		{"second weapon", func(c *Controller, o *wire.Observation) { o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"} }},
		{"requested pickup", func(c *Controller, o *wire.Observation) {
			o.Pickups = []wire.Pickup{{ID: 8002, Kind: "laser", Pos: o.Self.Pos}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, o := crossfireScene(t)
			tc.change(c, &o)
			c.Decide(o)
			if c.crossfire.active {
				t.Fatal("started an ineligible break")
			}
		})
	}
}

func TestCrossfireLaserCredit(t *testing.T) {
	c, o := crossfireScene(t)
	// Exactly 120 damage/2.4 s = 50 HP/s. Without a beam our 600 HP
	// lasts 12 s, beyond the fixed 10 s horizon.
	o.Events = []wire.Event{damageFrom(1, 60), damageFrom(15, 60)}
	for i := range o.Mechs {
		o.Mechs[i].Charging = true
		o.Mechs[i].Weapons = []string{"gun", "laser"}
		o.Mechs[i].Aim = angles(sub(c.bodyCenter(o.Self, o.Self.Pos), c.actorMuzzle(o.Mechs[i])))
	}
	c.recordIncoming(o)
	c.cfg.Cubes.DamagePerCube = .04
	o.Mechs[1].Cubes = 20
	o.Self.Stats["armor"] = .2
	finish := 6.
	risk, ok := c.incomingRace(o, finish)
	// B=315*1.4*.8=352.8: lethal time is 4.944 s.
	if !ok || !risk.needsLaser || risk.laser != 15 {
		t.Fatalf("wrong laser credit %+v", risk)
	}
	if _, ok := c.incomingRace(o, 4.9); ok {
		t.Fatal("stacked beams, uncapped scaling or omitted armor")
	}
	o.Mechs[1].Aim[0] += 1.4
	if _, ok := c.incomingRace(o, finish); ok {
		t.Fatal("unreachable laser slew received credit")
	}
	o.Mechs[1].Aim[0] -= 1.4
	c.cfg.Weapons["laser"]["range"] = 10
	if _, ok := c.incomingRace(o, finish); ok {
		t.Fatal("out of range beam received credit")
	}
	c.cfg.Weapons["laser"]["range"] = 280
	o.Mechs[1].Charging = false
	o.Mechs[0].Weapons = []string{"gun"}
	if _, ok := c.incomingRace(o, 9); ok {
		t.Fatal("nonlaser charge or idle laser received credit")
	}
}

func TestCrossfireEndpointNoNewRayAndRequiredBeam(t *testing.T) {
	c, o := crossfireScene(t)
	o.Self.Aim[0] = 0
	a := wire.Action{}
	enemy, _ := c.bestEnemy(o)
	// A blocked-at-start player can currently see the proposed endpoint.
	risk := crossfireRisk{laser: -1, lanes: []fireLane{{source: 1, muzzle: c.actorMuzzle(o.Mechs[0]), confirmed: true}, {source: 8, muzzle: wire.Vec3{-12, 4, 7}, blocked: true}}}
	center := c.bodyCenter(o.Self, wire.Vec3{-12, 10, 0})
	if cut, _ := c.cutsAt(center, risk); cut != 0 {
		t.Fatal("opened a new visible player's ray")
	}
	risk.lanes = risk.lanes[:1]
	risk.lanes = append(risk.lanes, fireLane{source: 15, muzzle: wire.Vec3{-80, 22, 7}, confirmed: true})
	if _, _, ok := c.selectBreak(o, a, enemy, risk); !ok {
		t.Fatal("fixture lacks a partial-cover endpoint")
	}
	risk.needsLaser, risk.laser = true, 15
	if _, _, ok := c.selectBreak(o, a, enemy, risk); ok {
		t.Fatal("partial cover leaves the required beam open")
	}
}

func TestCrossfireActivePrioritiesAndRestart(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Controller, *wire.Observation)
		keep   bool
	}{
		{"scout after loss of visibility", func(c *Controller, o *wire.Observation) { o.Mechs = nil }, true},
		{"required shooter hidden", func(c *Controller, o *wire.Observation) { c.crossfire.requiredLaser = 15; o.Mechs = o.Mechs[:1] }, true},
		{"one round target", func(c *Controller, o *wire.Observation) { o.Mechs[0].HP = 1 }, false},
		{"urgent zone", func(c *Controller, o *wire.Observation) { o.Self.InZone = false }, false},
		{"recovery latch", func(c *Controller, o *wire.Observation) { c.recovering = true }, false},
		{"resource pursuit", func(c *Controller, o *wire.Observation) {
			o.Mechs = nil
			o.Cubes = []wire.Cube{{ID: 5001, Pos: wire.Vec3{-15, 22, 0}}}
		}, false},
		{"crate firing", func(c *Controller, o *wire.Observation) {
			o.Mechs = nil
			o.Crates = []wire.Crate{{ID: 1001, Pos: wire.Vec3{-16, 22, 0}, HP: 40}}
		}, false},
		{"take", func(c *Controller, o *wire.Observation) {
			o.Pickups = []wire.Pickup{{ID: 8002, Kind: "laser", Pos: o.Self.Pos}}
		}, false},
		{"secondary acquired", func(c *Controller, o *wire.Observation) { o.Self.Weapons[1] = &wire.Weapon{Kind: "laser"} }, false},
		{"arrived", func(c *Controller, o *wire.Observation) { o.Self.Pos = c.crossfire.dest; o.Self.Pos[0] += .99 }, false},
		{"deadline", func(c *Controller, o *wire.Observation) { o.T = c.crossfire.deadline }, false},
		{"lost walk", func(c *Controller, o *wire.Observation) { c.crossfire.dest = wire.Vec3{7, 5, 0} }, false},
		{"endpoint outside zone on arrival", func(c *Controller, o *wire.Observation) {
			o.Zone = wire.Zone{Center: wire.Vec2{o.Self.Pos[0], o.Self.Pos[1]}, Radius: 50, NextCenter: wire.Vec2{o.Self.Pos[0], o.Self.Pos[1]}, NextRadius: 5, ShrinkStart: o.T, ShrinkEnd: o.T + .5}
		}, false},
		{"required laser regained line", func(c *Controller, o *wire.Observation) {
			c.crossfire.requiredLaser = 15
			o.Mechs[1].Pos = c.crossfire.dest
			o.Mechs[1].Pos[0] -= 2
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, o := crossfireScene(t)
			c.Decide(o)
			if !c.crossfire.active {
				t.Fatal("fixture did not start")
			}
			o.Events = nil
			o.T += .1
			o.Tick += 2
			tc.change(c, &o)
			parent := *c
			parent.crossfire = crossfireState{}
			want := parent.Decide(o)
			a := c.Decide(o)
			originalFields := a
			if tc.keep {
				originalFields.Move = want.Move
			}
			if !reflect.DeepEqual(originalFields, want) || c.target != parent.target || c.recovering != parent.recovering {
				t.Fatalf("ordinary action or target changed: got %+v, baseline %+v", originalFields, want)
			}
			if c.crossfire.active != tc.keep {
				t.Fatalf("unexpected latch; action %+v target %+v", a, c.target)
			}
			if tc.keep && a.Move != c.breakSteer(o, c.crossfire.dest) {
				t.Fatalf("lost held endpoint during brief occlusion: %v", a.Move)
			}
		})
	}
	c, o := crossfireScene(t)
	c.Decide(o)
	o.Events = nil
	o.T += .1
	o.Tick += 2
	o.Self.Grounded = false
	c.Decide(o)
	o.Self.Grounded = true
	o.T += .1
	o.Tick += 2
	c.Decide(o)
	if c.crossfire.active {
		t.Fatal("restarted on the old damage evidence")
	}
	o.Events = []wire.Event{damageFrom(1, 10)}
	o.T += .1
	o.Tick += 2
	c.Decide(o)
	if !c.crossfire.active || c.crossfire.evidenceTick != o.Tick {
		t.Fatal("new damage failed to authorize a start")
	}
}

func TestCrossfireContinuationChecksDelayedShot(t *testing.T) {
	c, o := crossfireScene(t)
	o.Self.Pos = wire.Vec3{-10, 20.4, 0}
	o.Self.Vel[1] = -1.5
	o.Mechs[0].Pos = wire.Vec3{40, 20.4, 0}
	o.Mechs = o.Mechs[:1]
	o.Self.Weapons[0].Cooldown = .08
	o.Self.Aim = angles(sub(c.bodyCenter(o.Self, o.Mechs[0].Pos), c.actorMuzzle(wire.Actor{Pos: o.Self.Pos})))
	baseline := *c
	want := baseline.Decide(o)
	c.crossfire = crossfireState{active: true, dest: wire.Vec3{-10, 5, 0}, deadline: o.T + 2, requiredLaser: -1}
	got := c.Decide(o)
	if !got.Fire[0] || c.crossfire.active || !reflect.DeepEqual(got, want) {
		t.Fatalf("move threatened a second-tick gun shot: %+v, baseline %+v", got, want)
	}
}

func TestCrossfireRaceHorizonAndSlack(t *testing.T) {
	c, o := crossfireScene(t)
	o.Events = []wire.Event{damageFrom(1, 72), damageFrom(15, 72)}
	c.recordIncoming(o)
	// 600 HP / (144 / 2.4) = 10 s, including the fixed forecast horizon.
	o.Self.Stats["regen_delay"] = 2
	if _, ok := c.incomingRace(o, 10+c.period); ok {
		t.Fatal("finish within timing slack cannot lose")
	}
	if _, ok := c.incomingRace(o, 10+c.period+.0001); !ok {
		t.Fatal("inclusive fixed horizon ignored")
	}
	o.Self.HP += .01
	if _, ok := c.incomingRace(o, 20); ok {
		t.Fatal("forecast exceeded the horizon")
	}
	o.Self.HP = .45 * o.Self.MaxHP
	o.Events = []wire.Event{damageFrom(1, 200), damageFrom(15, 200)}
	o.T += 3
	o.Tick += 60
	c.Decide(o)
	if !c.crossfire.active {
		t.Fatal("45% HP boundary incorrectly excluded")
	}
}

func TestCrossfireLegalSlewAndObstruction(t *testing.T) {
	c, o := crossfireScene(t)
	m := o.Mechs[0]
	m.Charging, m.Weapons = true, []string{"gun", "laser"}
	center := c.bodyCenter(o.Self, o.Self.Pos)
	src := c.actorMuzzle(m)
	m.Aim = angles(sub(center, src))
	m.Aim[0] += .37
	if !c.chargingBeam(o, m, src, center) {
		t.Fatal("beam can reach within a decision")
	}
	c.cfg.Weapons["laser"]["charge_turn_mult"] = .4
	if c.chargingBeam(o, m, src, center) {
		t.Fatal("beam exceeds reduced turn limits")
	}
	c.cfg.Weapons["laser"]["charge_turn_mult"] = 1
	m.Pos[1] = 0
	src = c.actorMuzzle(m)
	m.Aim = angles(sub(center, src))
	if c.chargingBeam(o, m, src, center) {
		t.Fatal("laser credited through blocking wall")
	}
}
