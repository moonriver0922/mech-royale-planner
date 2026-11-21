"""Practice-only referee adapted from LanceHuang245/mech-royale at 171bc06 (MIT)."""
import copy
import math
import random
import time

DT = 0.1  # one decision per 0.1 s
MOVEMENT_MODEL = "horizontal-vector-subticks-v1"
NOISE_MULT = 5.0  # house-bot aim noise multiplier: with HUNT 1.0 and 3 lasers the kit example scores ~1.2 (real 1.15)
LASER_BOTS = 3
HUNT_MULT = 1.0  # scales how far house bots chase us


def wrap(a):
    return (a + math.pi) % (2 * math.pi) - math.pi


def horizontal_motion(velocity, target, acceleration, ticks, tick_dt):
    """Approach the requested planar velocity and integrate travel at the game tick rate."""
    vx, vy = velocity[:2]
    x = y = 0.0
    for _ in range(ticks):
        dx, dy = target[0] - vx, target[1] - vy
        change = math.hypot(dx, dy)
        if change > acceleration * tick_dt:
            scale = acceleration * tick_dt / change
            vx += dx * scale
            vy += dy * scale
        else:
            vx, vy = target
        x += vx * tick_dt
        y += vy * tick_dt
    return vx, vy, x, y


class Mech:
    def __init__(self, mid, pos, skill, weapons):
        self.id, self.pos, self.vel = mid, list(pos), [0.0, 0.0, 0.0]
        self.aim = [0.0, 0.0]
        self.hp = self.max_hp = 1400.0
        self.energy = 100.0
        self.skill = skill
        self.weapons = weapons  # list of dict or None
        self.alive = True
        self.last_dmg = -99.0
        self.last_spend = -99.0
        self.grounded = True
        self.charge = 0.0
        self.seen = {}  # id -> last time seen (for memory)
        self.strafe = 1
        self.strafe_t = 0.0


def weapon(kind, cfg):
    w = {"kind": kind, "cooldown": 0.0, "stats": dict(cfg["weapons"][kind])}
    if kind == "gun":
        w["ammo"], w["reload"] = w["stats"]["mag"], 0.0
    if kind == "laser":
        w["charge"] = 0.0
    return w


class Sim:
    roof_loot = False

    def __init__(self, cfg, world, seed, bot, roof_locations=None):
        self.cfg, self.w, self.rng = cfg, world, random.Random(seed)
        self.bot = bot
        self.t = 0.0
        self.zone_phase = 0
        z = cfg["zone"]
        self.z_center, self.z_radius = list(z["start_center"]), z["start_radius"]
        self.z_next = self._next_circle()
        self.z_phase_start = 0.0
        self.mechs = []
        skills = [0.0] + [4.0] * 4 + [2.5] * 4 + [1.3] * 4 + [0.7] * 3
        spawns = self._spawns(16)
        for i in range(16):
            ws = [weapon("gun", cfg), None]
            if i >= 16 - LASER_BOTS:  # the strongest carry lasers
                ws[1] = weapon("laser", cfg)
            self.mechs.append(Mech(i, spawns[i], skills[i], ws))
        self.me = self.mechs[0]
        self.me.weapons = [weapon("gun", cfg), None]
        self.pickups = []
        for k in range(cfg["loot"]["floor_count"]):
            p = self._open_point(self.z_center, self.z_radius * 0.9)
            self.pickups.append({"id": 8000 + k, "kind": self.rng.choice(["mortar", "laser", "lightning"]), "pos": p})
        self.roof_loot = roof_locations is not None
        self.roof_ids = set()
        self.roof_taken = set()
        if self.roof_loot:
            count = round(len(self.pickups) * cfg["loot"]["floor_roof_fraction"])
            # Move existing item positions; keep ground spawns, item kinds and other RNG draws paired.
            roof_rng = random.Random(seed ^ 0x42BC912)
            for item, position in zip(roof_rng.sample(self.pickups, count), roof_rng.sample(roof_locations, count)):
                item["pos"] = list(position)
                self.roof_ids.add(item["id"])
        self.bullets = []
        self.cubes = []  # {"id","pos"}
        self.me.cubes = 0
        self.deaths = []  # (t, id)

    def _next_circle(self):
        z = self.cfg["zone"]
        if self.zone_phase >= len(z["phases"]):
            return (list(self.z_center), 0.0)
        r = z["phases"][self.zone_phase]["radius"]
        room = self.z_radius - r
        a = self.rng.random() * math.tau
        d = self.rng.random() * room * 0.9
        return ([self.z_center[0] + math.cos(a) * d, self.z_center[1] + math.sin(a) * d], r)

    def _open_point(self, c, r):
        for _ in range(500):
            a, d = self.rng.random() * math.tau, math.sqrt(self.rng.random()) * r
            x, y = c[0] + math.cos(a) * d, c[1] + math.sin(a) * d
            g = self.w.ground(x, y)
            if not self.w.blocked(x, y, g) and self.w.top[self.w.cell(x, y)] < g + 1:
                return [x, y, g]
        return [c[0], c[1], self.w.ground(*c)]

    def _spawns(self, n):
        pts = []
        for _ in range(n):
            for _ in range(200):
                p = self._open_point(self.z_center, self.z_radius * 0.8)
                if all(math.dist(p[:2], q[:2]) > 130 for q in pts):
                    break
            pts.append(p)
        return pts

    # ------------------------------------------------------------------ zone
    def zone_info(self):
        z = self.cfg["zone"]
        ph = z["phases"][min(self.zone_phase, len(z["phases"]) - 1)]
        ss = self.z_phase_start + ph["hold_s"]
        se = ss + ph["shrink_s"]
        return ss, se, ph["dps"]

    def zone_now(self):
        ss, se, dps = self.zone_info()
        c, r = self.z_center, self.z_radius
        nc, nr = self.z_next
        if self.t >= ss:
            f = min(1.0, (self.t - ss) / (se - ss))
            c = [c[0] + (nc[0] - c[0]) * f, c[1] + (nc[1] - c[1]) * f]
            r = r + (nr - r) * f
        return c, r, dps

    def step_zone(self):
        ss, se, _ = self.zone_info()
        if self.t >= se and self.zone_phase < len(self.cfg["zone"]["phases"]):
            self.z_center, self.z_radius = self.z_next
            self.zone_phase += 1
            self.z_phase_start = self.t
            self.z_next = self._next_circle()

    # ------------------------------------------------------------------ vision
    def can_see(self, a, b):
        ex = (a.pos[0], a.pos[1], a.pos[2] + 8)
        tx = (b.pos[0], b.pos[1], b.pos[2] + 8)
        d = math.dist(ex[:2], tx[:2])
        if d > 200:
            return False
        if d > 45:
            ang = math.atan2(tx[1] - ex[1], tx[0] - ex[0])
            if abs(wrap(ang - a.aim[0])) > math.radians(60):
                return False
        return self.w.los(ex, tx)

    # ------------------------------------------------------------------ obs for our bot
    def obs(self, events):
        me = self.me
        mechs = []
        for m in self.mechs:
            if m is me or not m.alive:
                continue
            if self.can_see(me, m):
                me.seen[m.id] = self.t
            if self.t - me.seen.get(m.id, -99) <= 5:
                mechs.append({"id": m.id, "kind": "mech", "pos": list(m.pos), "vel": list(m.vel), "aim": list(m.aim),
                              "hp": m.hp, "max_hp": m.max_hp, "cubes": 0,
                              "weapons": [w["kind"] for w in m.weapons if w], "charging": m.charge > 0})
        def pick_visible(p):
            d = math.dist(p["pos"][:2], me.pos[:2])
            if d > 200:
                return False
            if d > 45 and abs(wrap(math.atan2(p["pos"][1] - me.pos[1], p["pos"][0] - me.pos[0]) - me.aim[0])) > math.radians(60):
                return False
            return self.w.los((me.pos[0], me.pos[1], me.pos[2] + 8), (p["pos"][0], p["pos"][1], p["pos"][2] + 1))
        pick = [p for p in self.pickups if pick_visible(p)]
        c, r, dps = self.zone_now()
        ss, se, _ = self.zone_info()
        st = dict(self.cfg["mech"])
        eye = (me.pos[0], me.pos[1], me.pos[2] + 8)
        projs = []
        for b in self.bullets:
            if b["by"] != 0 and math.dist(b["pos"][:2], me.pos[:2]) < 150 and self.w.los(eye, b["pos"], 5.0):
                projs.append({"id": 1, "kind": "gun", "pos": [round(v, 2) for v in b["pos"]], "vel": [round(v, 2) for v in b["vel"]]})
        cubes = [dict(c) for c in self.cubes if math.dist(c["pos"][:2], me.pos[:2]) < 100 and self.w.los(eye, (c["pos"][0], c["pos"][1], c["pos"][2] + 1), 5.0)]
        return {"type": "obs", "tick": int(round(self.t * 20)), "t": round(self.t, 2),
                "alive": sum(m.alive for m in self.mechs),
                "self": {"id": 0, "pos": list(me.pos), "vel": list(me.vel), "aim": list(me.aim), "hp": me.hp,
                         "max_hp": round(me.max_hp, 1), "energy": me.energy, "max_energy": 100, "grounded": me.grounded,
                         "cubes": me.cubes, "upgrades": [], "in_zone": math.dist(me.pos[:2], c) <= r,
                         "weapons": copy.deepcopy(me.weapons), "stats": st},
                "mechs": mechs, "projectiles": projs, "crates": [], "cubes": cubes, "pickups": pick,
                "sounds": [], "beams": [], "events": events, "drops": [],
                "zone": {"center": c, "radius": r, "next_center": self.z_next[0], "next_radius": self.z_next[1],
                         "shrink_start": ss, "shrink_end": se, "dps": dps},
                "offer": None}

    # ------------------------------------------------------------------ house AI
    def house_action(self, m):
        me = self.me
        if not me.alive:
            return {}
        tgt = me
        d = math.dist(m.pos[:2], tgt.pos[:2])
        ang = math.atan2(tgt.pos[1] - m.pos[1], tgt.pos[0] - m.pos[0])
        c, r, _ = self.zone_now()
        nc = self.z_next[0]
        if self.t > m.strafe_t:
            m.strafe = -m.strafe
            m.strafe_t = self.t + 1 + self.rng.random() * 2
        tier = {4.0: (150, 110), 2.5: (260, 140), 1.3: (420, 170), 0.7: (1e9, 200)}.get(m.skill, (300, 150))
        hunt_r, fire_r = tier[0] * HUNT_MULT, tier[1]
        if math.dist(m.pos[:2], c) > r - 10 or d > hunt_r:
            mv = math.atan2(nc[1] - m.pos[1], nc[0] - m.pos[0])
            if math.dist(m.pos[:2], nc) < 40:
                mv += math.pi / 2
            move = [math.cos(mv), math.sin(mv)]
        elif d > 90:
            move = [math.cos(ang), math.sin(ang)]
        else:
            move = [math.cos(ang + math.pi / 2) * m.strafe, math.sin(ang + math.pi / 2) * m.strafe]
        # greedy slide around walls
        if self.w.blocked(m.pos[0] + move[0] * 4, m.pos[1] + move[1] * 4, m.pos[2], 4):
            for da in (0.8, -0.8, 1.6, -1.6, 2.4, -2.4):
                a2 = math.atan2(move[1], move[0]) + da
                if not self.w.blocked(m.pos[0] + math.cos(a2) * 4, m.pos[1] + math.sin(a2) * 4, m.pos[2], 4):
                    move = [math.cos(a2), math.sin(a2)]
                    break
        sees = d < fire_r and self.can_see(m, tgt) and \
            self.w.los((m.pos[0], m.pos[1], m.pos[2] + 7), (tgt.pos[0], tgt.pos[1], tgt.pos[2] + 4.5))
        noise = math.radians(m.skill) * NOISE_MULT
        tt = d / 252
        px, py = tgt.pos[0] + tgt.vel[0] * tt, tgt.pos[1] + tgt.vel[1] * tt
        yaw = math.atan2(py - m.pos[1], px - m.pos[0]) + self.rng.gauss(0, noise)
        pitch = math.atan2(tgt.pos[2] + 4.5 - m.pos[2] - 7, d) + self.rng.gauss(0, noise * 0.5)
        fire = [False, False]
        if sees and d < 220:
            fire[0] = abs(wrap(yaw - m.aim[0])) < 0.05
            if m.weapons[1] and m.weapons[1]["kind"] == "laser":
                fire[1] = not (m.charge >= m.weapons[1]["stats"]["charge"] and abs(wrap(yaw - m.aim[0])) < 0.03)
        return {"move": move, "aim": [yaw, pitch], "fire": fire, "boost": False, "jump": False}

    # ------------------------------------------------------------------ physics & weapons
    def apply(self, m, a, events_for):
        cfg = self.cfg["mech"]
        if not m.alive:
            return
        # aim slew
        ty, tp = a.get("aim", m.aim)
        dy = wrap(ty - m.aim[0])
        lim = cfg["turn_rate"] * DT
        m.aim[0] = wrap(m.aim[0] + max(-lim, min(lim, dy)))
        lp = cfg["pitch_rate"] * DT
        m.aim[1] = m.aim[1] + max(-lp, min(lp, tp - m.aim[1]))
        # movement
        mv = a.get("move", [0, 0])
        l = math.hypot(*mv)
        if l > 1:
            mv = [mv[0] / l, mv[1] / l]
        boost = a.get("boost", False) and m.energy > 0
        speed = cfg["boost_speed"] if boost and m.grounded else cfg["walk_speed"]
        if m.charge > 0:
            speed *= 0.5
        acc = cfg["ground_accel"] if m.grounded else cfg["air_accel"]
        match = self.cfg["match"]
        target = (mv[0] * speed, mv[1] * speed)
        m.vel[0], m.vel[1], dx, dy = horizontal_motion(
            m.vel, target, acc, match["decide_every"], 1 / match["tick_hz"])
        if boost:
            m.energy -= (cfg["boost_drain"] if m.grounded else cfg["jet_drain"]) * DT
            m.last_spend = self.t
        if a.get("jump") and m.grounded:
            m.vel[2] = cfg["jump_speed"]
            m.grounded = False
        if not m.grounded:
            if boost:
                m.vel[2] = min(cfg["max_rise_speed"], m.vel[2] + cfg["jet_accel"] * DT)
            m.vel[2] -= cfg["gravity"] * DT
        nx, ny = m.pos[0] + dx, m.pos[1] + dy
        nz = m.pos[2] + m.vel[2] * DT
        if self.w.blocked(nx, ny, max(nz, m.pos[2])):
            if not self.w.blocked(nx, m.pos[1], m.pos[2]):
                ny, m.vel[1] = m.pos[1], 0.0
            elif not self.w.blocked(m.pos[0], ny, m.pos[2]):
                nx, m.vel[0] = m.pos[0], 0.0
            else:
                nx, ny, m.vel[0], m.vel[1] = m.pos[0], m.pos[1], 0.0, 0.0
        floor = self.w.ground(nx, ny)
        c = self.w.cell(nx, ny)
        if c >= 0 and self.w.top[c] <= max(nz, m.pos[2]) + 1.3 and self.w.top[c] > floor:
            floor = self.w.top[c]
        if nz <= floor or (m.grounded and nz - floor < 1.5):
            nz, m.vel[2], m.grounded = floor, 0.0, True
        else:
            m.grounded = False
        m.pos = [nx, ny, nz]
        if self.t - m.last_spend > cfg["energy_regen_delay"]:
            m.energy = min(100.0, m.energy + cfg["energy_regen"] * DT)
        # take
        if a.get("take") is not None:
            for p in self.pickups:
                if (p["id"] == a["take"]
                        and math.dist(p["pos"][:2], m.pos[:2]) <= self.cfg["loot"]["pickup_radius"]
                        and (not self.roof_loot or abs(p["pos"][2] - m.pos[2]) <= 4)):
                    slot = a.get("drop")
                    if slot is None:
                        slot = 1 if m.weapons[1] is None else (0 if m.weapons[0] is None else None)
                    if slot is not None:
                        old = m.weapons[slot]
                        m.weapons[slot] = weapon(p["kind"], self.cfg)
                        m.weapons[slot]["cooldown"] = 1.0
                        self.pickups.remove(p)
                        if self.roof_loot and m is self.me and p["id"] in self.roof_ids:
                            self.roof_taken.add(p["id"])
                        if old:
                            self.pickups.append({"id": 9000 + len(self.pickups) + int(self.t * 10), "kind": old["kind"], "pos": list(m.pos)})
                    break
        # weapons
        fire = a.get("fire", [False, False])
        for i, w in enumerate(m.weapons):
            if not w:
                continue
            w["cooldown"] = max(0.0, w["cooldown"] - DT)
            k, s = w["kind"], w["stats"]
            if k == "gun":
                if w["reload"] > 0:
                    w["reload"] = max(0.0, w["reload"] - DT)
                    if w["reload"] == 0:
                        w["ammo"] = s["mag"]
                elif a.get("reload") and w["ammo"] < s["mag"]:
                    w["reload"] = s["reload"]
                elif fire[i]:
                    n = 0
                    while w["cooldown"] <= 0 and w["ammo"] > 0 and n < 2:
                        self.shoot_bullet(m, s)
                        w["ammo"] -= 1
                        w["cooldown"] += s["cooldown"]
                        n += 1
                    if w["ammo"] == 0:
                        w["reload"] = s["reload"]
            elif k == "laser":
                if fire[i] and w["cooldown"] <= 0:
                    m.charge += DT
                    w["charge"] = m.charge
                    if m.charge >= s["charge"] + s["hold_s"]:
                        self.fire_laser(m, s, events_for)
                elif m.charge > 0:
                    if m.charge >= s["min_charge"]:
                        self.fire_laser(m, s, events_for)
            elif k == "lightning":
                if fire[i] and w["cooldown"] <= 0 and m.energy >= s["energy"]:
                    w["cooldown"] = s["cooldown"]
                    m.energy -= s["energy"]
                    m.last_spend = self.t
                    self.fire_lightning(m, s, events_for)

    def shoot_bullet(self, m, s):
        sp = math.radians(s["spread_deg"])
        yaw = m.aim[0] + self.rng.gauss(0, sp)
        pitch = m.aim[1] + self.rng.gauss(0, sp)
        d = (math.cos(pitch) * math.cos(yaw), math.cos(pitch) * math.sin(yaw), math.sin(pitch))
        o = [m.pos[0] + d[0] * 3, m.pos[1] + d[1] * 3, m.pos[2] + 7 + d[2] * 3]
        self.bullets.append({"by": m.id, "pos": o, "vel": [d[0] * s["speed"], d[1] * s["speed"], d[2] * s["speed"]],
                             "left": s["range"], "dmg": s["damage"]})

    def fire_laser(self, m, s, ev):
        frac = s["min_frac"] + (1 - s["min_frac"]) * min(1.0, m.charge / s["charge"])
        m.charge = 0.0
        w = [x for x in m.weapons if x and x["kind"] == "laser"][0]
        w["charge"], w["cooldown"] = 0.0, s["cooldown"]
        yaw, pitch = m.aim
        d = (math.cos(pitch) * math.cos(yaw), math.cos(pitch) * math.sin(yaw), math.sin(pitch))
        o = (m.pos[0], m.pos[1], m.pos[2] + 7)
        best = None
        for t in self.targets(m):
            rel = [t.pos[0] - o[0], t.pos[1] - o[1]]
            along = rel[0] * d[0] + rel[1] * d[1]
            if along <= 0 or along > s["range"]:
                continue
            perp = abs(rel[0] * d[1] - rel[1] * d[0])
            hz = o[2] + d[2] * along / max(1e-6, math.hypot(d[0], d[1]))
            if perp < 2.5 and t.pos[2] - 0.5 <= hz <= t.pos[2] + 9.5:
                if self.w.los(o, (t.pos[0], t.pos[1], hz)) and (best is None or along < best[0]):
                    best = (along, t)
        if best:
            self.damage(best[1], m, s["damage"] * frac, ev)

    def fire_lightning(self, m, s, ev):
        o = (m.pos[0], m.pos[1], m.pos[2] + 7)
        best = None
        for t in self.targets(m):
            d = math.dist(t.pos[:2], m.pos[:2])
            if d > s["range"]:
                continue
            ang = math.atan2(t.pos[1] - m.pos[1], t.pos[0] - m.pos[0])
            if abs(wrap(ang - m.aim[0])) > math.radians(s["cone_deg"] / 2):
                continue
            if self.w.los(o, (t.pos[0], t.pos[1], t.pos[2] + 4.5)) and (best is None or d < best[0]):
                best = (d, t)
        if best:
            self.damage(best[1], m, s["damage"], ev)

    def targets(self, m):
        if m is self.me:
            return [x for x in self.mechs if x.alive and x is not m]
        return [self.me] if self.me.alive else []

    def damage(self, t, by, amt, ev):
        if by is self.me:
            amt *= 1 + 0.04 * min(self.me.cubes, 10)
        to_by = math.atan2(by.pos[1] - t.pos[1], by.pos[0] - t.pos[0])
        rear = abs(wrap(to_by - t.aim[0])) > math.pi / 2
        if rear:
            amt *= 1.5
        t.hp -= amt
        t.last_dmg = self.t
        by.last_dmg = self.t
        if t is self.me:
            ev.append({"type": "damaged", "amount": amt, "weapon": "gun", "bearing": to_by, "by": by.id, "rear": rear})
        if by is self.me:
            ev.append({"type": "hit", "target": t.id, "amount": amt, "weapon": "gun"})
        if t.hp <= 0 and t.alive:
            t.alive = False
            self.deaths.append((self.t, t.id))
            for k in range(2):
                self.cubes.append({"id": 5000 + len(self.cubes) + k + int(self.t * 10), "pos": [t.pos[0] + k * 2 - 1, t.pos[1], t.pos[2]]})
            if by is self.me:
                ev.append({"type": "kill", "target": t.id})
            for w in t.weapons:
                if w and w["kind"] != "gun":
                    self.pickups.append({"id": 9500 + t.id, "kind": w["kind"], "pos": list(t.pos)})

    def step_bullets(self, ev):
        keep = []
        for b in self.bullets:
            sub = 4
            alive = True
            for _ in range(sub):
                o = list(b["pos"])
                for i in range(3):
                    b["pos"][i] += b["vel"][i] * DT / sub
                step = math.dist(o, b["pos"])
                b["left"] -= step
                if b["left"] <= 0 or not self.w.los(o, b["pos"], 2.0):
                    alive = False
                    break
                shooter = self.mechs[b["by"]]
                hit = None
                for t in self.targets(shooter):
                    # closest approach of segment o->pos to the cylinder axis
                    px, py = t.pos[0], t.pos[1]
                    sx, sy = b["pos"][0] - o[0], b["pos"][1] - o[1]
                    L = sx * sx + sy * sy or 1e-9
                    u = max(0.0, min(1.0, ((px - o[0]) * sx + (py - o[1]) * sy) / L))
                    cx, cy, cz = o[0] + sx * u, o[1] + sy * u, o[2] + (b["pos"][2] - o[2]) * u
                    if math.hypot(cx - px, cy - py) < 2.8 and t.pos[2] - 0.3 <= cz <= t.pos[2] + 9.3:
                        hit = t
                        break
                if hit:
                    self.damage(hit, shooter, b["dmg"], ev)
                    alive = False
                    break
            if alive:
                keep.append(b)
        self.bullets = keep

    def run(self, max_t=480):
        ev = []
        times = []
        seen_roof = set()
        zone_ticks = 0
        kills = 0
        cfg = self.cfg["mech"]
        while self.t < max_t:
            alive = [m for m in self.mechs if m.alive]
            if len(alive) <= 1 or not self.me.alive:
                break
            o = self.obs(ev)
            ev = []
            t0 = time.perf_counter()
            a = self.bot.act(o)
            times.append((time.perf_counter() - t0) * 1000)
            zone_ticks += not o["self"]["in_zone"]
            if self.roof_loot:
                seen_roof.update(p["id"] for p in o["pickups"] if p["id"] in self.roof_ids)
            acts = {0: a}
            for m in alive:
                if m is not self.me:
                    acts[m.id] = self.house_action(m)
            for m in alive:
                self.apply(m, acts.get(m.id, {}), ev)
            self.step_bullets(ev)
            for cb in list(self.cubes):
                if self.me.alive and math.dist(cb["pos"][:2], self.me.pos[:2]) < 3.5 and abs(cb["pos"][2] - self.me.pos[2]) < 4:
                    self.cubes.remove(cb)
                    self.me.cubes += 1
                    cubes = self.cfg["cubes"]
                    new_max = cfg["hp"] * (1 + cubes["hp_per_cube"] * min(self.me.cubes, cubes["stat_cap"]))
                    self.me.hp += new_max - self.me.max_hp
                    self.me.max_hp = new_max
                    ev.append({"type": "pickup", "cubes": self.me.cubes})
            c, r, dps = self.zone_now()
            for m in self.mechs:
                if not m.alive:
                    continue
                if math.dist(m.pos[:2], c) > r:
                    m.hp -= dps * DT
                    m.last_dmg = self.t
                    if m is self.me:
                        ev.append({"type": "damaged", "amount": dps * DT, "weapon": "zone", "bearing": None, "by": None})
                    if m.hp <= 0:
                        m.alive = False
                        self.deaths.append((self.t, m.id))
                elif self.t - m.last_dmg > cfg["regen_delay"]:
                    m.hp = min(m.max_hp, m.hp + cfg["regen_per_s"] * DT)
            kills += sum(event["type"] == "kill" for event in ev)
            self.step_zone()
            self.t += DT
        if self.me.alive:
            place = 1 + sum(m.alive and m.hp > self.me.hp for m in self.mechs)
        else:
            # Other eliminations on the same tick share this placement.
            place = 1 + sum(m.alive for m in self.mechs)
        return {"place": place, "points": 16 - place, "alive_s": round(self.t, 1),
                "outside_s": round(zone_ticks * DT, 1), "kills": kills,
                "roof_items_seen": len(seen_roof), "roof_items_taken": len(self.roof_taken),
                "act_ms_max": round(max(times), 2) if times else 0,
                "act_ms_p99": round(sorted(times)[int(len(times) * 0.99)], 2) if times else 0}
