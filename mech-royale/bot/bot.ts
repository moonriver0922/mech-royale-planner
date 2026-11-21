import { type ActorObs, type Hello, type Observation, type BotAction, draw, log, run } from "./mechfight.ts"
import { WorldMap } from "./map.ts"

const TAU = Math.PI * 2
const wrap = (a: number) => {
  a = (a + Math.PI) % TAU
  if (a < 0) a += TAU
  return a - Math.PI
}
const hyp2 = (ax: number, ay: number, bx: number, by: number) => Math.hypot(bx - ax, by - ay)

type Track = { id: number; pos: number[]; vel: number[]; hp: number; maxHp: number; t: number; weapons: string[]; charging: boolean; aim: number[]; cubes: number }
type FarmTarget = { pos: number[]; height: number; vel?: number[] }
type CombatResource = { goal: [number, number]; value: number; farm?: FarmTarget }

const WEAPON_PREF: Record<string, number> = { laser: 3.2, lightning: 2.5, mortar: 1, gun: 0 }
const UPGRADE_PREF = [
  "plating", "armor", "nanites", "lightning_efficiency", "laser_damage", "laser_charge", "gun_damage", "gun_rate",
  "lightning_range", "lightning_chain", "gyros", "servos", "gun_mag", "capacitor", "reactor", "jets", "gun_velocity",
  "sensors", "mortar_reload", "mortar_velocity", "mortar_splash", "laser_pierce",
]

class Brain {
  name = "TTUN-mech"
  cfg: any
  map: WorldMap | null = null
  tracks = new Map<number, Track>()
  strafeDir = 1
  strafeUntil = 0
  lastPos: number[] = [0, 0, 0]
  stuckT = 0
  combatStep: { move: [number, number]; until: number } | null = null
  lastDamageT = -99
  lastDamageBearing = 0
  threatBearing: number | null = null
  threatT = -99
  rng: () => number
  lastHp = 0
  scanPhase = 0
  laserHold = false
  attackers = new Map<number, number>() // id -> last time they hit us
  hideUntil = 0
  mechHitT = -99
  targetId = -1
  targetT = -99
  mechHitBearing = 0
  npcMem = new Map<number, { pos: number[]; vel: number[]; t: number; hp: number }>()
  cubeMem = new Map<number, { pos: number[]; t: number }>()
  pickMem = new Map<number, { pos: number[]; kind: string; t: number }>()

  constructor(hello: Hello) {
    this.cfg = hello.config
    let s = (hello.seed >>> 0) || 1
    this.rng = () => {
      s ^= s << 13
      s >>>= 0
      s ^= s >>> 17
      s ^= s << 5
      s >>>= 0
      return s / 4294967296
    }
    try {
      const t0 = Date.now()
      this.map = new WorldMap(hello.map.dir)
      // warm the JIT on the path search
      this.map.field(0, 0)
      this.map.pump(1e9)
      this.map.field(200, -100)
      this.map.pump(1e9)
      log(`map loaded in ${Date.now() - t0} ms`)
    } catch (e) {
      log("map load failed", e)
    }
  }

  act(o: Observation): BotAction {
    const t0 = performance.now()
    const me = o.self
    const [x, y, z] = me.pos
    const t = o.t
    const map = this.map
    const act: BotAction = {}
    const scale = 1 + (this.cfg.cubes?.size_per_cube ?? 0.05) * me.cubes
    const muzzleZ = z + (me.stats.muzzle_height ?? 7)
    const eyeZ = z + (me.stats.eye_height ?? 8)

    // ---------- memory
    for (const m of o.mechs) {
      if (m.kind !== "mech") continue
      this.tracks.set(m.id, {
        id: m.id, pos: m.pos, vel: m.vel, hp: m.hp ?? 1400, maxHp: m.max_hp ?? 1400, t, weapons: m.weapons ?? [],
        charging: !!m.charging, aim: m.aim, cubes: m.cubes ?? 0,
      })
    }
    for (const [id, tr] of this.tracks) if (t - tr.t > 12) this.tracks.delete(id)
    for (const e of o.events) {
      if (e.type === "damaged" && e.by != null && e.weapon !== "npc") {
        this.attackers.set(e.by, t)
        if (e.by < 16 && e.bearing != null) {
          this.mechHitT = t
          this.mechHitBearing = e.bearing
        }
      }
      if (e.type === "damaged" && e.weapon !== "zone" && e.bearing != null) {
        this.lastDamageT = t
        this.lastDamageBearing = e.bearing
        this.threatBearing = e.bearing
        this.threatT = t
      }
      if (e.type === "kill") this.tracks.delete(e.target)
    }
    for (const s of o.sounds) {
      if (["gun", "laser", "laser_charge", "lightning", "mortar", "boost", "step", "jet"].includes(s.kind) && s.dist !== "far") {
        if (t - this.threatT > 1.5) {
          this.threatBearing = s.bearing
          this.threatT = t
        }
      }
    }

    // ---------- upgrades
    if (o.offer) {
      let bi = 0, bs = 1e9
      o.offer.choices.forEach((c, i) => {
        const r = UPGRADE_PREF.indexOf(c.id)
        const s = r < 0 ? 50 : r
        if (s < bs) {
          bs = s
          bi = i
        }
      })
      act.pick = bi
    }

    // ---------- weapons held
    const w0 = me.weapons[0], w1 = me.weapons[1]
    const held = (k: string) => (w0?.kind === k ? 0 : w1?.kind === k ? 1 : -1)
    const gunS = held("gun"), laserS = held("laser"), lightS = held("lightning"), mortarS = held("mortar")

    // ---------- loot: which pickups we want. -1 = goes into an empty slot, 0/1 = slot to swap out, null = not wanted.
    // The gun is never swapped out while held.
    const wantKind = (k: string): number | null => {
      if (held(k) >= 0) return null
      if (!w0 || !w1) return -1
      let worst: number | null = null, wv = 1e9
      ;[w0, w1].forEach((w, i) => {
        if (w.kind === "gun" || (w.kind === "laser" && (w.charge ?? 0) > 0)) return
        const v = WEAPON_PREF[w.kind] ?? 0
        if (v < wv) {
          wv = v
          worst = i
        }
      })
      return worst !== null && (WEAPON_PREF[k] ?? 0) > wv + 0.5 ? worst : null
    }
    const onlyGun = !(w0 && w1)
    for (const p of o.pickups) {
      if (Math.abs(p.pos[2] - this.mapGround(p.pos[0], p.pos[1])) > 2) continue
      this.pickMem.set(p.id, { pos: p.pos, kind: p.kind, t })
    }
    for (const e of o.events) if ((e.type === "took" || e.type === "take_failed") && e.id != null) this.pickMem.delete(e.id)
    for (const [id, p] of this.pickMem) {
      if (p.t === t) continue
      if (t - p.t > 60 || hyp2(x, y, p.pos[0], p.pos[1]) < 18) this.pickMem.delete(id)
    }
    let nearPick: number[] | null = null, nearPickD = 1e9
    for (const p of this.pickMem.values()) {
      if (wantKind(p.kind) === null) continue
      const d = hyp2(x, y, p.pos[0], p.pos[1]) - (onlyGun ? 0 : (WEAPON_PREF[p.kind] ?? 0) * 5)
      if (d < nearPickD) {
        nearPickD = d
        nearPick = p.pos
      }
    }
    const reach = this.cfg.loot.pickup_radius - 0.3
    for (const p of o.pickups) {
      if (hyp2(x, y, p.pos[0], p.pos[1]) > reach || Math.abs(p.pos[2] - z) > 3.5) continue
      const slot = wantKind(p.kind)
      if (slot === null) continue
      act.take = p.id
      if (slot >= 0) act.drop = slot
      break
    }

    // ---------- choose target
    const visible = o.mechs.filter((m) => m.kind === "mech")
    let target: Track | null = null
    let tScore = 1e9
    for (const tr of this.tracks.values()) {
      const d = hyp2(x, y, tr.pos[0], tr.pos[1])
      const age = t - tr.t
      if (age > 4) continue
      const hitUs = t - (this.attackers.get(tr.id) ?? -99) < 3 ? 50 : 0
      const stick = tr.id === this.targetId && t - this.targetT < 2 ? 40 : 0
      const s = d + (tr.hp / 1400) * 90 + age * 30 - hitUs - stick
      if (s < tScore) {
        tScore = s
        target = tr
      }
    }
    if (target) {
      this.targetId = target.id
      this.targetT = t
    }
    const npcs = o.mechs.filter((m) => m.kind === "npc")
    for (const n of npcs) this.npcMem.set(n.id, { pos: n.pos, vel: n.vel, t, hp: n.hp ?? 350 })
    for (const e of o.events) if (e.type === "hit" && e.target >= 100 && e.target < 1000) {
      const nm = this.npcMem.get(e.target)
      if (nm) nm.hp -= e.amount
    }
    for (const [id, nm] of this.npcMem) if (t - nm.t > 25 || nm.hp <= 0) this.npcMem.delete(id)
    for (const c of o.cubes) {
      if (Math.abs(c.pos[2] - this.mapGround(c.pos[0], c.pos[1])) > 2) continue
      this.cubeMem.set(c.id, { pos: c.pos, t })
    }
    for (const [id, c] of this.cubeMem) {
      if (c.t === t) continue
      if (t - c.t > 40 || hyp2(x, y, c.pos[0], c.pos[1]) < 18) this.cubeMem.delete(id)
    }
    let nearCube: number[] | null = null, nearCubeD = 1e9
    for (const c of this.cubeMem.values()) {
      const d = hyp2(x, y, c.pos[0], c.pos[1])
      if (d < nearCubeD) {
        nearCubeD = d
        nearCube = c.pos
      }
    }
    let threats = 0
    for (const tr of this.tracks.values()) if (t - tr.t < 3 && hyp2(x, y, tr.pos[0], tr.pos[1]) < 160) threats++

    // ---------- zone goal
    const zn = o.zone
    const curR = zn.radius
    const shrinking = t >= zn.shrink_start
    const timeToShrink = zn.shrink_start - t
    const dNext = hyp2(x, y, zn.next_center[0], zn.next_center[1])
    const dCur = hyp2(x, y, zn.center[0], zn.center[1])
    const speed = me.stats.walk_speed ?? 11.7
    // where must we be
    let zoneGoal: [number, number] | null = null
    const needNext = dNext > zn.next_radius * 0.8 && (shrinking || timeToShrink < (dNext - zn.next_radius * 0.6) / speed + 12)
    if (!me.in_zone || dCur > curR * 0.9 || needNext) {
      const ang = Math.atan2(y - zn.next_center[1], x - zn.next_center[0])
      const r = zn.next_radius * 0.45
      zoneGoal = [zn.next_center[0] + Math.cos(ang) * r, zn.next_center[1] + Math.sin(ang) * r]
    }

    // ---------- decide
    let moveGoal: [number, number] | null = null
    let aimYaw = me.aim[0], aimPitch = 0
    let wantAim = false
    let mode = "roam"
    const fire: [boolean, boolean] = [false, false]
    const hpFrac = me.hp / me.max_hp
    let boost = false
    let jump = false
    let strafe = 0
    let canReposition = false
    let laserCharging = laserS >= 0 && (me.weapons[laserS]?.charge ?? 0) > 0

    if (target) {
      const tp = target.pos
      const d = hyp2(x, y, tp[0], tp[1])
      const tScale = 1 + 0.05 * target.cubes
      const tcz = tp[2] + 4.5 * tScale
      const seen = t - target.t < 0.11
      const hasLos = map ? map.los(x, y, muzzleZ, tp[0], tp[1], tcz) : true
      mode = "fight"

      // aim solution
      const aimFor = (spd: number): [number, number] => {
        let px = tp[0], py = tp[1], pz = tcz
        const age = t - target!.t
        px += target!.vel[0] * age
        py += target!.vel[1] * age
        for (let it = 0; it < 3; it++) {
          const dd = Math.hypot(px - x, py - y, pz - muzzleZ)
          const tt = dd / spd
          px = tp[0] + target!.vel[0] * (tt + age)
          py = tp[1] + target!.vel[1] * (tt + age)
          pz = tcz + (target!.vel[2] ?? 0) * Math.min(tt, 0.3)
        }
        return [Math.atan2(py - y, px - x), Math.atan2(pz - muzzleZ, Math.hypot(px - x, py - y))]
      }
      const instant = (): [number, number] => [Math.atan2(tp[1] - y, tp[0] - x), Math.atan2(tcz - muzzleZ, d)]

      // pick weapon emphasis
      let a: [number, number]
      const gunSpeed = gunS >= 0 ? me.weapons[gunS]!.stats.speed : 252
      if (lightS >= 0 && d < (me.weapons[lightS]!.stats.range ?? 45) * 1.1) a = instant()
      else if (laserS >= 0 && (laserCharging || (me.weapons[laserS]!.cooldown ?? 0) < 0.2)) a = instant()
      else a = aimFor(gunSpeed)
      aimYaw = a[0]
      aimPitch = a[1]
      wantAim = true
      const yawErr = Math.abs(wrap(me.aim[0] - aimYaw))
      const pitchErr = Math.abs(me.aim[1] - aimPitch)
      const tol = Math.atan2(2.2 * tScale, Math.max(d, 1))

      if (seen && hasLos) {
        // gun
        if (gunS >= 0) {
          const g = me.weapons[gunS]!
          const ga = aimFor(g.stats.speed)
          const gErr = Math.abs(wrap(me.aim[0] - ga[0]))
          if (d < g.stats.range && gErr < tol * 1.2 && pitchErr < tol * 2 + 0.03 && (g.reload ?? 0) === 0) fire[gunS] = true
        }
        // lightning
        if (lightS >= 0) {
          const L = me.weapons[lightS]!
          if (d < L.stats.range - 1 && yawErr < ((L.stats.cone_deg * Math.PI) / 180) * 0.45 && me.energy > L.stats.energy + 2) fire[lightS] = true
        }
      }
      // laser: charge while target is around, release when full and aligned
      if (laserS >= 0) {
        const Lw = me.weapons[laserS]!
        const full = Lw.stats.charge
        const ch = Lw.charge ?? 0
        if (ch > 0) {
          const ready = ch >= full - 0.05
          const aligned = seen && hasLos && yawErr < tol * 0.9 && pitchErr < tol * 1.5 + 0.02
          if (ready && aligned) fire[laserS] = false
          else fire[laserS] = true
        } else if (Lw.cooldown <= 0.05 && seen && d < Lw.stats.range && (hasLos || d < 60)) {
          fire[laserS] = true
        }
      }
      // mortar
      if (mortarS >= 0) {
        const M = me.weapons[mortarS]!
        if (M.cooldown <= 0 && d > 18 && d < 220) {
          const v = M.stats.speed, g = M.stats.gravity
          const h = tp[2] - (z + 9.5 * scale)
          let tof = d / (v * 0.7)
          let px = tp[0] + target.vel[0] * tof, py = tp[1] + target.vel[1] * tof
          const dd = Math.hypot(px - x, py - y)
          const disc = v ** 4 - g * (g * dd * dd + 2 * h * v * v)
          if (disc > 0) {
            const low = Math.atan((v * v - Math.sqrt(disc)) / (g * dd))
            const high = Math.atan((v * v + Math.sqrt(disc)) / (g * dd))
            const myMortarYaw = Math.atan2(py - y, px - x)
            let pitch = hasLos ? low : high
            if (pitch > (me.stats.max_pitch ?? 1.3)) pitch = low
            // only use mortar when not using other weapons' aim this tick, or if it's the best
            if (!fire[0] && !fire[1] && !(laserS >= 0 && laserCharging)) {
              aimYaw = myMortarYaw
              aimPitch = pitch
              if (Math.abs(wrap(me.aim[0] - myMortarYaw)) < 0.03 && Math.abs(me.aim[1] - pitch) < 0.03) fire[mortarS] = true
            }
          }
        }
      }

      // movement in fight
      const myDps = (gunS >= 0 ? 150 : 0) + (lightS >= 0 && d < 45 ? 200 : 0) + (laserS >= 0 ? 120 : 0)
      const theirHp = target.hp
      const theirFrac = theirHp / target.maxHp
      const finishing = theirFrac < 0.2 && hpFrac > 0.2 && threats <= 2
      const losing = !finishing && ((hpFrac < 0.35 && theirFrac > hpFrac + 0.1) || (threats >= 2 && hpFrac < 0.55) || (threats >= 3 && hpFrac < 0.8))
      canReposition = target.t === t && !finishing && !target.charging
      if (losing) this.hideUntil = t + 6
      const prefer = lightS >= 0 ? 28 : laserS >= 0 ? 110 : 70
      if (losing && map) {
        mode = "retreat"
        const danger = this.retreatAttacker(t, x, y, target)
        const [ex, ey, ez] = danger.pos
        moveGoal = map.coverFrom(x, y, ex, ey, ez + 8, 50) ?? [2 * x - ex, 2 * y - ey]
        boost = me.energy > 15
      } else {
        const dirx = (tp[0] - x) / Math.max(d, 1), diry = (tp[1] - y) / Math.max(d, 1)
        let radial = 0
        if (d > prefer + 15) {
          radial = 1
          if (lightS >= 0 && d < 90 && me.energy > 70) boost = true
        }
        else if (d < prefer - 15) radial = -0.7
        if (!hasLos && seen === false) radial = 1
        if (t > this.strafeUntil) {
          this.strafeDir = this.rng() < 0.5 ? -1 : 1
          this.strafeUntil = t + 0.35 + this.rng() * 0.7
        }
        strafe = this.strafeDir
        // dodge incoming laser: hard strafe
        if (target.charging) {
          const theirYaw = target.aim[0]
          const toMe = Math.atan2(y - tp[1], x - tp[0])
          if (Math.abs(wrap(theirYaw - toMe)) < 0.25) boost = me.energy > (lightS >= 0 ? 45 : 10)
        }
        const mx = dirx * radial + -diry * strafe * 0.9
        const my = diry * radial + dirx * strafe * 0.9
        if (map && radial > 0 && !hasLos) {
          map.field(tp[0], tp[1])
          const st = map.fieldFor(tp[0], tp[1], 30) ? map.steer(x, y) : null
          if (st) moveGoal = [x + st[0] * 10 + -diry * strafe * 3, y + st[1] * 10 + dirx * strafe * 3]
          else moveGoal = [x + mx * 10, y + my * 10]
        } else moveGoal = [x + mx * 10, y + my * 10]
      }
      if (nearCube && nearCubeD < (losing ? 12 : hpFrac > 0.5 ? 70 : 40)) {
        moveGoal = [nearCube[0], nearCube[1]]
        mode += "+cube"
      } else if (nearPick && !losing && nearPickD < (onlyGun ? 60 : 35)) {
        moveGoal = [nearPick[0], nearPick[1]]
        mode += "+weapon"
      }
      // Movement can secure an upgrade while aim remains on a real contact.
      if (!losing && !finishing && map && !zoneGoal && !laserCharging && hpFrac >= 0.55 &&
          o.alive > 4 && (me.cubes < 6 || onlyGun)) {
        const betterShot = seen && hasLos && (d < 105 || fire.some(Boolean))
        const resource = this.combatResource(o, map, target, onlyGun, wantKind, gunS >= 0 && !betterShot)
        if (resource) {
          moveGoal = resource.goal
          boost = false
          mode = "fight+resource"
          if (resource.farm && gunS >= 0 && !fire[0] && !fire[1]) {
            mode += "+shoot"
            const [cx, cy, cz] = resource.farm.pos
            const distance = hyp2(x, y, cx, cy)
            const travel = distance / (me.weapons[gunS]?.stats.speed ?? 252)
            const px = cx + (resource.farm.vel?.[0] ?? 0) * travel
            const py = cy + (resource.farm.vel?.[1] ?? 0) * travel
            aimYaw = Math.atan2(py - y, px - x)
            aimPitch = Math.atan2(cz + resource.farm.height - muzzleZ, hyp2(x, y, px, py))
            const tolerance = Math.atan2(1.2, Math.max(1, distance))
            fire[gunS] = (me.weapons[gunS]?.reload ?? 0) === 0 &&
              Math.abs(wrap(me.aim[0] - aimYaw)) < tolerance &&
              Math.abs(me.aim[1] - aimPitch) < tolerance * 2 + 0.02
          }
        }
      }
      if (zoneGoal && (!me.in_zone || shrinking)) {
        // zone overrides when outside
        if (!me.in_zone) moveGoal = null
      }
      void myDps
    }

    // heal: hold still (silent) out of sight until HP is back
    const healing = !moveGoal && !target && !zoneGoal && nearCubeD > 40 && (hpFrac < 0.55 || (t < this.hideUntil && hpFrac < 0.9))
    let slow = false
    if (healing) {
      mode = "heal"
      // creep (silently) into cover from the most recent threat
      let last: Track | null = null
      for (const tr of this.tracks.values()) if (!last || tr.t > last.t) last = tr
      if (last && map) {
        const exposed = map.los(last.pos[0], last.pos[1], last.pos[2] + 8, x, y, z + 6)
        if (exposed) {
          const cov = map.coverFrom(x, y, last.pos[0], last.pos[1], last.pos[2] + 8, 36)
          if (cov) {
            moveGoal = cov
            slow = true
          }
        }
      }
    }
    if (!moveGoal && !healing) {
      if (zoneGoal) {
        mode = mode === "fight" ? "fight-zone" : "zone"
        moveGoal = zoneGoal
        boost = !me.in_zone && me.energy > 20
      } else if (mode !== "fight") {
        // loot: pickups worth taking, then crates/cubes, then NPCs
        let best: [number, number] | null = null, bs = 1e9
        for (const p of this.pickMem.values()) {
          if (wantKind(p.kind) === null) continue
          const d = hyp2(x, y, p.pos[0], p.pos[1])
          const s = d - (WEAPON_PREF[p.kind] ?? 0) * 20 - (onlyGun ? 50 : 0)
          if (s < bs) {
            bs = s
            best = [p.pos[0], p.pos[1]]
          }
        }
        for (const c of this.cubeMem.values()) {
          const d = hyp2(x, y, c.pos[0], c.pos[1]) - 30
          if (d < bs) {
            bs = d
            best = [c.pos[0], c.pos[1]]
          }
        }
        // supply drops: capture when nobody else is on it
        for (const dr of o.drops ?? []) {
          const d = hyp2(x, y, dr.pos[0], dr.pos[1])
          if (dr.holder != null && dr.holder !== me.id) continue
          if (d > 220 || dr.lands_in > d / speed + 3) continue
          const zr = hyp2(dr.pos[0], dr.pos[1], zn.next_center[0], zn.next_center[1])
          if (zr > zn.next_radius && timeToShrink < 15) continue
          const sc = d - 80
          if (sc < bs) {
            bs = sc
            best = d < dr.radius - 3 ? [x, y] : [dr.pos[0], dr.pos[1]]
          }
        }
        // NPC drones: walk into gun range of the nearest remembered one
        if (hpFrac > 0.5) {
          for (const nm of this.npcMem.values()) {
            const d = hyp2(x, y, nm.pos[0], nm.pos[1])
            if (d > 170) continue
            const sc = d - 20
            if (sc < bs) {
              bs = sc
              best = d > 38 ? [nm.pos[0], nm.pos[1]] : [x, y]
            }
          }
        }
        // crates on the ground: walk into range and break them
        if (hpFrac > 0.45) {
          for (const c of o.crates) {
            if (Math.abs(c.pos[2] - this.mapGround(c.pos[0], c.pos[1])) > 2) continue
            const d = hyp2(x, y, c.pos[0], c.pos[1])
            if (d > 150) continue
            const sc = d + 5
            if (sc < bs) {
              bs = sc
              best = d > 30 ? [c.pos[0], c.pos[1]] : [x, y]
            }
          }
        }
        if (best) {
          mode = "loot"
          moveGoal = best
        } else {
          // wander toward the zone's next center slowly while scanning
          mode = "roam"
          const ang = Math.atan2(y - zn.next_center[1], x - zn.next_center[0])
          const r = Math.min(zn.next_radius * 0.6, Math.max(20, dNext - 30))
          moveGoal = [zn.next_center[0] + Math.cos(ang) * r, zn.next_center[1] + Math.sin(ang) * r]
        }
        // shoot crates / NPCs when nothing better
        const crate = o.crates
          .filter((c) => hyp2(x, y, c.pos[0], c.pos[1]) < 120 && map?.los(x, y, muzzleZ, c.pos[0], c.pos[1], c.pos[2] + 1.2) !== false)
          .sort((a, b) => hyp2(x, y, a.pos[0], a.pos[1]) - hyp2(x, y, b.pos[0], b.pos[1]))[0]
        const npc = npcs
          .filter((n) => hyp2(x, y, n.pos[0], n.pos[1]) < 110 && map?.los(x, y, muzzleZ, n.pos[0], n.pos[1], n.pos[2] + 1.75) !== false)
          .sort((a, b) => hyp2(x, y, a.pos[0], a.pos[1]) - hyp2(x, y, b.pos[0], b.pos[1]))[0]
        const tgt = npc ? { pos: npc.pos, vel: npc.vel, h: 1.75 } : crate ? { pos: crate.pos, vel: [0, 0, 0], h: 1.2 } : null
        const unseenAttacker = t - this.mechHitT < 1.5
        if (tgt && hpFrac > 0.4 && !unseenAttacker) {
          const dd = hyp2(x, y, tgt.pos[0], tgt.pos[1])
          const tt = dd / 252
          const px = tgt.pos[0] + tgt.vel[0] * tt, py = tgt.pos[1] + tgt.vel[1] * tt
          aimYaw = Math.atan2(py - y, px - x)
          aimPitch = Math.atan2(tgt.pos[2] + tgt.h - muzzleZ, dd)
          wantAim = true
          const tol = Math.atan2(1.2, Math.max(dd, 1))
          if (Math.abs(wrap(me.aim[0] - aimYaw)) < tol && Math.abs(me.aim[1] - aimPitch) < tol * 2 + 0.02) {
            if (gunS >= 0) fire[gunS] = true
            if (lightS >= 0 && dd < 40) fire[lightS] = true
          }
          if (!npc && crate && dd < 25) moveGoal = [crate.pos[0], crate.pos[1]]
          mode += "+shoot"
        }
      }
    }

    // ---------- movement execution
    let mv: [number, number] = [0, 0]
    if (moveGoal && map) {
      const gd = hyp2(x, y, moveGoal[0], moveGoal[1])
      const direct = map.los(x, y, z + 2, moveGoal[0], moveGoal[1], map.groundAt(moveGoal[0], moveGoal[1]) + 2)
      if (gd > 3) {
        const navigating = mode === "zone" || mode === "roam" || mode === "loot" || mode === "heal" ||
          mode.startsWith("fight+resource")
        const checkGround = navigating && me.grounded && Math.abs(z - map.groundAt(x, y)) < 2
        if (direct && gd < 60 && (!checkGround || map.walkSegment(x, y, moveGoal[0], moveGoal[1]))) {
          mv = [(moveGoal[0] - x) / gd, (moveGoal[1] - y) / gd]
        }
        else {
          map.field(moveGoal[0], moveGoal[1])
          const st = map.fieldFor(moveGoal[0], moveGoal[1], 25) ? map.steer(x, y) : null
          mv = st ?? [(moveGoal[0] - x) / gd, (moveGoal[1] - y) / gd]
        }
      }
    } else if (moveGoal) {
      const gd = hyp2(x, y, moveGoal[0], moveGoal[1])
      if (gd > 2) mv = [(moveGoal[0] - x) / gd, (moveGoal[1] - y) / gd]
    }
    if (slow) mv = [mv[0] * 0.4, mv[1] * 0.4]
    // dodge incoming bullets and shells
    {
      let dx = 0, dy = 0, urgent = false
      const cz = z + 4.5 * scale
      const mg = this.cfg.weapons.mortar.gravity ?? 22
      for (const p of o.projectiles) {
        const [px, py, pz] = p.pos
        const [vx, vy, vz] = p.vel
        if (p.kind === "mortar") {
          // time until it falls to our height, then landing point
          const a = -0.5 * mg, b = vz, c = pz - z
          const disc = b * b - 4 * a * c
          if (disc < 0) continue
          const tl = (-b - Math.sqrt(disc)) / (2 * a)
          if (tl < 0 || tl > 3) continue
          const lx = px + vx * tl, ly = py + vy * tl
          const dd = hyp2(x + me.vel[0] * tl, y + me.vel[1] * tl, lx, ly)
          if (dd < 11) {
            const l = Math.max(0.5, hyp2(x, y, lx, ly))
            dx += (x - lx) / l
            dy += (y - ly) / l
            urgent = true
          }
          continue
        }
        const rx = x - px, ry = y - py, rz = cz - pz
        const vv = vx * vx + vy * vy + vz * vz
        if (vv < 1) continue
        const tc = (rx * vx + ry * vy + rz * vz) / vv
        if (tc < 0 || tc > 0.9) continue
        const mx = px + vx * tc - x, my = py + vy * tc - y, mz = pz + vz * tc - cz
        const miss = Math.hypot(mx, my, mz * 0.5)
        if (miss > 3.6 * scale) continue
        // move perpendicular to the bullet path, away from its closest point
        const hv = Math.hypot(vx, vy) || 1
        let nx = -vy / hv, ny = vx / hv
        if (nx * -mx + ny * -my < 0) {
          nx = -nx
          ny = -ny
        }
        dx += nx
        dy += ny
        urgent = true
      }
      if (urgent) {
        const l = Math.hypot(dx, dy)
        if (l > 0.01) {
          mv = [mv[0] * 0.3 + (dx / l) * 0.95, mv[1] * 0.3 + (dy / l) * 0.95]
          const ml = Math.hypot(mv[0], mv[1])
          if (ml > 1) mv = [mv[0] / ml, mv[1] / ml]
          if (me.energy > (lightS >= 0 && target ? 55 : 25)) boost = true
          mode += "+dodge"
        }
      }
    }
    // stuck handling: jump + jet
    const moved = hyp2(x, y, this.lastPos[0], this.lastPos[1])
    if ((mv[0] || mv[1]) && moved < 0.25 && me.grounded) this.stuckT += 0.1
    else if (moved > 0.6) this.stuckT = 0
    // Clear the wall beside a stalled combat path before using the jump fallback.
    if (map && mode === "retreat" && me.grounded &&
        dCur < curR - 8 && Math.abs(z - map.groundAt(x, y)) < 0.6) {
      if (this.combatStep && (t >= this.combatStep.until ||
          !map.groundStepClear(x, y, z, ...this.combatStep.move))) this.combatStep = null
      const stride = Math.hypot(...mv)
      if (!this.combatStep && this.stuckT >= 0.3 && moved < 0.25 && stride >= 0.75 &&
          !map.groundStepClear(x, y, z, mv[0] / stride, mv[1] / stride)) {
        const heading = Math.atan2(mv[1], mv[0])
        for (const turn of [60, -60, 90, -90, 120, -120, 150, -150]) {
          const angle = heading + turn * this.strafeDir * Math.PI / 180
          const next: [number, number] = [Math.cos(angle), Math.sin(angle)]
          if (!map.groundStepClear(x, y, z, ...next)) continue
          this.combatStep = { move: next, until: t + 0.5 }
          break
        }
      }
    } else {
      this.combatStep = null
    }
    if (this.combatStep) {
      mv = this.combatStep.move
      this.stuckT = 0
    }
    if (this.stuckT > 0.6) {
      jump = true
      boost = me.energy > 10
      if (this.stuckT > 2.5) {
        // try a perpendicular direction
        mv = [-mv[1] * (this.strafeDir), mv[0] * this.strafeDir]
      }
      if (this.stuckT > 4) this.stuckT = 0
    }
    if (!me.grounded && this.stuckT > 0) boost = me.energy > 5
    const laserHeld = laserS >= 0 && fire[laserS]
    const reposition = canReposition && mode === "fight" && target && map && !zoneGoal && !boost && !jump &&
      act.take === undefined ? this.fightPosition(o, map, target, visible, mv, laserCharging, laserHeld) : null
    if (reposition) mv = reposition
    const rotation = target && map && zoneGoal && shrinking && me.in_zone && !jump && !this.combatStep &&
      !mode.endsWith("+dodge") ? this.safeRotation(o, map, visible, mv, boost, laserCharging, moveGoal, mode, act.take) : null
    if (rotation) mv = rotation
    this.lastPos = [x, y, z]

    // ---------- aim when idle: look along move or toward threats
    if (!wantAim) {
      if (t < 3) aimYaw = me.aim[0] + 1.6
      else if (t - this.mechHitT < 2) aimYaw = this.mechHitBearing
      else if (this.threatBearing !== null && t - this.threatT < 3) aimYaw = this.threatBearing
      else if (mv[0] || mv[1]) {
        this.scanPhase += 0.1
        aimYaw = Math.atan2(mv[1], mv[0]) + Math.sin(this.scanPhase * 1.3) * 0.9
      } else {
        this.scanPhase += 0.1
        aimYaw = me.aim[0] + 0.6
      }
      aimPitch = 0
    }
    // keep laser charging when target briefly lost
    if (laserS >= 0 && laserCharging && !fire[laserS]) {
      const Lw = me.weapons[laserS]!
      if ((Lw.charge ?? 0) < Lw.stats.charge - 0.05) fire[laserS] = true
    }
    // reload when safe
    if (gunS >= 0 && mode !== "fight") {
      const g = me.weapons[gunS]!
      if ((g.ammo ?? 12) < g.stats.mag * 0.5 && (g.reload ?? 0) === 0 && !fire[gunS]) act.reload = true
    }

    const pmin = me.stats.min_pitch ?? -0.7, pmax = me.stats.max_pitch ?? 1.3
    act.move = [Math.round(mv[0] * 1000) / 1000, Math.round(mv[1] * 1000) / 1000]
    act.aim = [Math.round(wrap(aimYaw) * 10000) / 10000, Math.round(Math.max(pmin, Math.min(pmax, aimPitch)) * 10000) / 10000]
    act.fire = fire
    act.boost = boost
    act.jump = jump
    draw.label(`${mode}${rotation ? "+rotate" : ""}${this.combatStep ? "+sidestep" : ""}${reposition ? "+position" : ""} hp${me.hp | 0} ${target ? "T" + target.id : ""}`)
    this.lastHp = me.hp
    if (map) map.pump(Math.max(0, 20 - (performance.now() - t0)), 70000)
    return act
  }

  mapGround(x: number, y: number) {
    return this.map ? this.map.groundAt(x, y) : 0
  }

  fightPosition(o: Observation, map: WorldMap, target: Track, visible: ActorObs[],
      move: [number, number], charging: boolean, laserHeld: boolean): [number, number] | null {
    const me = o.self
    const [x, y, z] = me.pos
    if (!me.grounded || !map.walk[map.cellOf(x, y)] || Math.abs(z - map.groundAt(x, y)) > 1 ||
        Math.hypot(...move) < 0.9) return null

    const step = 0.2
    const horizon = 2 * step
    const speed = (me.stats.walk_speed ?? 11.7) * (charging || laserHeld ? (me.stats.charge_move_mult ?? 0.5) : 1)
    const accel = (me.stats.ground_accel ?? 58.5) * step
    const muzzleHeight = me.stats.muzzle_height ?? 7
    const chestHeight = (me.stats.height ?? 9) / 2
    const targetChest = target.pos[2] + 4.5 * (1 + 0.05 * target.cubes)
    const contacts = visible.flatMap(actor => {
      const [ax, ay, az] = actor.pos
      const distance = hyp2(x, y, ax, ay)
      const attacked = o.t - (this.attackers.get(actor.id) ?? -99) < 1.5
      const facing = Math.abs(wrap(actor.aim[0] - Math.atan2(y - ay, x - ax)))
      if (!attacked && facing > 0.3) return []
      const weapons = actor.weapons ?? ["gun"]
      const range = Math.max(0, ...weapons.map(kind => kind === "mortar" ? 0 : (this.cfg.weapons[kind]?.range ?? 0)))
      if (range <= 0 || distance > range + horizon * (speed + Math.hypot(actor.vel[0], actor.vel[1]))) return []
      const muzzle = az + (this.cfg.mech.muzzle_height ?? 7) * (1 + 0.05 * (actor.cubes ?? 0))
      if (!attacked && (distance > range || !map.los(ax, ay, muzzle, x, y, z + chestHeight))) return []
      return [{ actor, range, muzzle, weight: actor.id === target.id ? 1 : 2, priority: distance - (attacked ? 80 : 0) }]
    }).sort((a, b) => a.priority - b.priority).slice(0, 3)
    if (!contacts.length) return null
    const otherAttackers = contacts.reduce((mask, contact, index) =>
      contact.actor.id === target.id ? mask : mask | (1 << index), 0)

    // A movement option is useful only if a direct-fire weapon can actually be ready at that point.
    const firingRange = (seconds: number) => {
      let range = 0
      for (const weapon of me.weapons) {
        if (!weapon || weapon.kind === "mortar" || weapon.cooldown > seconds) continue
        if (weapon.kind === "gun" && ((weapon.reload ?? 0) > seconds ||
            ((weapon.ammo ?? 1) <= 0 && !weapon.reload))) continue
        if (weapon.kind === "laser" && ((!charging && !laserHeld) ||
            weapon.cooldown + weapon.stats.charge - (weapon.charge ?? 0) - 0.05 > seconds)) continue
        if (weapon.kind === "lightning" && me.energy <= weapon.stats.energy + 2) continue
        range = Math.max(range, weapon.stats.range - (weapon.kind === "lightning" ? 1 : 0))
      }
      return range
    }
    const samples = [step, horizon].map(seconds => ({
      seconds,
      tx: target.pos[0] + target.vel[0] * seconds,
      ty: target.pos[1] + target.vel[1] * seconds,
      tz: targetChest + target.vel[2] * seconds,
      range: firingRange(seconds),
    }))
    const project = (heading: [number, number]) => {
      let px = x, py = y
      let [vx, vy] = me.vel
      const exposure = []
      let shots = 0, pressure = 0
      for (const sample of samples) {
        const dx = heading[0] * speed - vx, dy = heading[1] * speed - vy
        const ramp = Math.min(1, accel / (Math.hypot(dx, dy) || 1))
        const nx = px + (vx + dx * ramp / 2) * step, ny = py + (vy + dy * ramp / 2) * step
        if (!map.walkSegment(px, py, nx, ny) || hyp2(nx, ny, ...o.zone.center) > o.zone.radius - 2) return null
        px = nx
        py = ny
        vx += dx * ramp
        vy += dy * ramp
        const ground = map.groundAt(px, py)
        const distance = hyp2(px, py, sample.tx, sample.ty)
        const yaw = Math.atan2(sample.ty - py, sample.tx - px)
        const pitch = Math.atan2(sample.tz - ground - muzzleHeight, distance)
        shots <<= 1
        if (distance < sample.range && pitch >= (me.stats.min_pitch ?? -0.7) && pitch <= (me.stats.max_pitch ?? 1.3) &&
            Math.abs(wrap(yaw - me.aim[0])) <= (me.stats.turn_rate ?? 4) * sample.seconds + 0.05 &&
            Math.abs(pitch - me.aim[1]) <= (me.stats.pitch_rate ?? 3) * sample.seconds + 0.05 &&
            map.los(px, py, ground + muzzleHeight, sample.tx, sample.ty, sample.tz)) shots |= 1
        let shooters = 0
        for (const [index, { actor, range, muzzle, weight }] of contacts.entries()) {
          const ax = actor.pos[0] + actor.vel[0] * sample.seconds
          const ay = actor.pos[1] + actor.vel[1] * sample.seconds
          if (hyp2(px, py, ax, ay) <= range &&
              map.los(ax, ay, muzzle + actor.vel[2] * sample.seconds, px, py, ground + chestHeight)) {
            shooters |= 1 << index
            pressure += weight
          }
        }
        exposure.push(shooters)
      }
      return { exposure, shots, pressure }
    }

    const current = project(move)
    if (!current) return null
    const angle = Math.atan2(move[1], move[0])
    let best = current
    let chosen: [number, number] | null = null
    // Only turn to break an established line of fire. During a shooting window, screen the
    // other attackers while preserving the shot at our current target.
    for (const turn of [30, -30, 60, -60]) {
      const bearing = angle + turn * Math.PI / 180
      const heading: [number, number] = [Math.cos(bearing), Math.sin(bearing)]
      const route = project(heading)
      if (!route || (route.shots & current.shots) !== current.shots) continue
      const [near, far] = route.exposure
      const screened = current.exposure[1] & ~far
      if (!screened || (current.shots && !(screened & otherAttackers)) ||
          (near & ~current.exposure[0]) || (far & ~current.exposure[1])) continue
      if (route.pressure < best.pressure || (route.pressure === best.pressure && route.shots > best.shots)) {
        best = route
        chosen = heading
      }
    }
    return chosen
  }

  safeRotation(o: Observation, map: WorldMap, visible: ActorObs[], move: [number, number],
      boost: boolean, charging: boolean, goal: [number, number] | null, mode: string,
      taking: BotAction["take"]): [number, number] | null {
    const me = o.self
    const zone = o.zone
    const [x, y, z] = me.pos
    const remaining = zone.shrink_end - o.t
    const nextDistance = hyp2(x, y, ...zone.next_center)
    if (remaining <= 0 || !me.grounded || Math.abs(z - map.groundAt(x, y)) >= 2 ||
        !map.walk[map.cellOf(x, y)] || nextDistance <= zone.next_radius - 2) return null

    const walk = me.stats.walk_speed ?? 11.7
    const run = me.stats.boost_speed ?? 19.5
    const drain = me.stats.boost_drain ?? 17
    const chargeScale = charging ? (me.stats.charge_move_mult ?? 0.5) : 1
    const step = 0.3
    const maxVelocityChange = (me.stats.ground_accel ?? 58.5) * step
    const approachingWeapon = mode.endsWith("+weapon")
    const resource = approachingWeapon ? o.pickups : mode.endsWith("+cube") ? o.cubes : []
    const pickup = resource.find(item => goal && item.pos[0] === goal[0] && item.pos[1] === goal[1])
    // A take command for this goal completes the approach this tick, before the first simulated step.
    if (approachingWeapon && pickup && pickup.id === taking) return null
    const radius = approachingWeapon ? this.cfg.loot.pickup_radius - 0.3 : this.cfg.cubes.pickup_radius
    const distanceFromEdge = (px: number, py: number, seconds: number) => {
      const fraction = Math.min(1, seconds / remaining)
      const cx = zone.center[0] + (zone.next_center[0] - zone.center[0]) * fraction
      const cy = zone.center[1] + (zone.next_center[1] - zone.center[1]) * fraction
      const radius = zone.radius + (zone.next_radius - zone.radius) * fraction
      return radius - hyp2(px, py, cx, cy)
    }
    const simulate = (heading: [number, number], collect = false) => {
      let [px, py] = me.pos
      let [vx, vy] = me.vel
      let margin = Infinity
      const points: [number, number][] = []
      for (let i = 1; i <= 4; i++) {
        const speed = (boost && me.energy >= i * step * drain ? run : walk) * chargeScale
        const dx = heading[0] * speed - vx, dy = heading[1] * speed - vy
        const fraction = Math.min(1, maxVelocityChange / (Math.hypot(dx, dy) || 1))
        // Average the starting and ending velocities over the substep.
        const nx = px + (vx + dx * fraction / 2) * step
        const ny = py + (vy + dy * fraction / 2) * step
        if (!map.walkSegment(px, py, nx, ny)) return null
        vx += dx * fraction
        vy += dy * fraction
        px = nx
        py = ny
        points.push([px, py])
        margin = Math.min(margin, distanceFromEdge(px, py, i * step))
        // The resource movement ends at pickup range. Subsequent frames will reassess the route.
        if (collect && pickup && hyp2(px, py, pickup.pos[0], pickup.pos[1]) <= radius &&
            Math.abs(map.groundAt(px, py) - pickup.pos[2]) <= 3.5) break
      }
      return { points, margin, progress: nextDistance - hyp2(px, py, ...zone.next_center) }
    }

    // Only reconsider when the actual combat movement would cross the shrinking edge soon.
    const current = simulate(move, true)
    if (!current || current.margin >= 0) return null

    const threats = visible.flatMap(actor => {
      const [ax, ay, az] = actor.pos
      const bearing = Math.atan2(y - ay, x - ax)
      const recentlyHit = o.t - (this.attackers.get(actor.id) ?? -99) < 2
      if (!recentlyHit && Math.abs(wrap(actor.aim[0] - bearing)) > 0.45) return []
      const range = Math.max(...(actor.weapons ?? ["gun"]).map(kind =>
        kind === "mortar" ? 0 : (this.cfg.weapons[kind]?.range ?? 0)))
      const distance = hyp2(x, y, ax, ay)
      if (distance > range + 24) return []
      const muzzle = az + (this.cfg.mech?.muzzle_height ?? 7) * (1 + 0.05 * (actor.cubes ?? 0))
      return [{ x: ax, y: ay, muzzle, range, distance }]
    }).sort((a, b) => a.distance - b.distance).slice(0, 3)
    const bodyHeight = (me.stats.height ?? 9) / 2
    const inward = Math.atan2(zone.next_center[1] - y, zone.next_center[0] - x)
    let best: [number, number] | null = null
    let bestExposure = Infinity, bestProgress = -Infinity
    for (const offset of [0, Math.PI / 4, -Math.PI / 4, Math.PI * 5 / 12, -Math.PI * 5 / 12]) {
      const heading: [number, number] = [Math.cos(inward + offset), Math.sin(inward + offset)]
      const path = simulate(heading)
      if (!path || path.margin < 0 || path.progress < 0.5) continue
      let exposure = 0
      for (const [px, py] of path.points) {
        const height = map.groundAt(px, py) + bodyHeight
        for (const threat of threats) {
          if (hyp2(px, py, threat.x, threat.y) <= threat.range &&
              map.los(threat.x, threat.y, threat.muzzle, px, py, height)) exposure++
        }
      }
      if (exposure < bestExposure || (exposure === bestExposure && path.progress > bestProgress)) {
        best = heading
        bestExposure = exposure
        bestProgress = path.progress
      }
    }
    return best
  }

  retreatAttacker(t: number, x: number, y: number, target: Track): Track {
    if (t - (this.attackers.get(target.id) ?? -99) < 3) return target
    let danger = target
    let score = Infinity
    for (const tr of this.tracks.values()) {
      const hitAge = t - (this.attackers.get(tr.id) ?? -99)
      const seenAge = t - tr.t
      if (hitAge > 3 || seenAge > 2) continue
      const distance = hyp2(x, y, tr.pos[0], tr.pos[1])
      if (distance > 190) continue
      const cost = distance + 25 * hitAge + 20 * seenAge
      if (cost < score) {
        score = cost
        danger = tr
      }
    }
    return danger
  }

  combatResource(o: Observation, map: WorldMap, target: Track, onlyGun: boolean,
      wantKind: (kind: string) => number | null, canFarm: boolean): CombatResource | null {
    const me = o.self
    const [x, y, z] = me.pos
    const t = o.t
    if (!me.grounded || t - this.mechHitT < 0.9 || Math.abs(z - map.groundAt(x, y)) > 2) return null
    for (const actor of o.mechs) {
      if (actor.kind !== "mech") continue
      const [ax, ay, az] = actor.pos
      const distance = hyp2(x, y, ax, ay)
      if (distance < 37) return null
      if (distance < 105 && Math.abs(wrap(actor.aim[0] - Math.atan2(y - ay, x - ax))) < 0.34 &&
          map.los(ax, ay, az + 7, x, y, z + 5)) return null
    }

    const [tx, ty] = target.pos
    const threatDistance = hyp2(x, y, tx, ty)
    let best: CombatResource | null = null
    const consider = (pos: number[], radius: number, value: number, farm?: FarmTarget) => {
      const [px, py, pz] = pos
      const distance = hyp2(x, y, px, py)
      if (distance > radius || hyp2(px, py, ...o.zone.center) > o.zone.radius - 5) return
      if (Math.abs(pz - map.groundAt(px, py)) > 2) return
      const closing = threatDistance - hyp2(px, py, tx, ty)
      if (threatDistance < 120 && closing > 15) return
      const score = value - distance - 0.7 * Math.max(0, closing)
      if (best && best.value >= score) return
      if (!map.walkSegment(x, y, px, py)) return
      best = { goal: [px, py], value: score, farm }
    }
    for (const item of o.pickups) {
      if (onlyGun && wantKind(item.kind) !== null) consider(item.pos, 53, 60 + 3 * (WEAPON_PREF[item.kind] ?? 0))
    }
    for (const cube of o.cubes) {
      if (me.cubes < 6) consider(cube.pos, 48, 57)
    }
    if (!best && canFarm && me.cubes < 6) {
      const muzzle = z + (me.stats.muzzle_height ?? 7)
      for (const crate of o.crates) {
        const [cx, cy, cz] = crate.pos
        const distance = hyp2(x, y, cx, cy)
        if (distance > 70 || !map.los(x, y, muzzle, cx, cy, cz + 1.2)) continue
        consider(crate.pos, 70, 25, { pos: crate.pos, height: 1.2 })
      }
      for (const actor of o.mechs) {
        if (actor.kind !== "npc") continue
        const [nx, ny, nz] = actor.pos
        const distance = hyp2(x, y, nx, ny)
        if (distance > 55 || !map.los(x, y, muzzle, nx, ny, nz + 1.75)) continue
        const groundPos = [nx, ny, map.groundAt(nx, ny)]
        consider(groundPos, 55, 38, { pos: actor.pos, height: 1.75, vel: actor.vel })
      }
      if (best) {
        const resource: CombatResource = best
        const distance = hyp2(x, y, ...resource.goal)
        const approach = Math.max(0, 1 - 25 / Math.max(1, distance))
        resource.goal = [x + (resource.goal[0] - x) * approach, y + (resource.goal[1] - y) * approach]
      }
    }
    return best
  }
}

run((hello) => new Brain(hello))
