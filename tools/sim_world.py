"""Fixed map queries from LanceHuang245/mech-royale at 171bc06 (MIT)."""
import math
import os
import sys
from array import array


def log(*args):
    print(*args, file=sys.stderr)


class World:
    RES = 2.0

    def __init__(self, map_dir):
        self.ok = False
        self.min_x, self.min_y, self.nx, self.ny = -513.0, -334.0, 1, 1
        self.t_nx = self.t_ny = 1
        self.terrain = array("h", [0])
        self.top = array("f", [-1e9])
        self.base = array("f", [1e9])
        try:
            self._load(map_dir)
            self.ok = True
        except Exception as e:  # play blind rather than crash
            log("map load failed:", repr(e))

    def _load(self, map_dir):
        import json
        with open(os.path.join(map_dir, "v2", "world.json")) as f:
            w = json.load(f)
        t = w["terrain"]
        self.t_min_x, self.t_min_y, self.t_step = t["minX"], t["minY"], t["step"]
        self.t_nx, self.t_ny = t["nx"], t["ny"]
        terr = array("h")
        with open(os.path.join(map_dir, "v2", "terrain.i16"), "rb") as f:
            terr.frombytes(f.read())
        if sys.byteorder != "little":
            terr.byteswap()
        self.terrain = terr
        b = w["bounds"]
        self.bx0, self.bx1, self.by0, self.by1 = b["minX"], b["maxX"], b["minY"], b["maxY"]
        r = self.RES
        self.min_x, self.min_y = self.t_min_x, self.t_min_y
        self.nx = int((self.t_nx * self.t_step) / r) + 1
        self.ny = int((self.t_ny * self.t_step) / r) + 1
        n = self.nx * self.ny
        top = array("f", [-1e9]) * n
        base = array("f", [1e9]) * n
        self.building_surface = bytearray(n)
        for p in w["prisms"]:
            polys = [p["ring"]] + [h for h in p.get("holes") or [] if h]
            edges = []
            ys = p["ring"][1::2]
            for poly in polys:
                m = len(poly) // 2
                for k in range(m):
                    x1, y1 = poly[2 * k], poly[2 * k + 1]
                    x2, y2 = poly[(2 * k + 2) % (2 * m)], poly[(2 * k + 3) % (2 * m)]
                    if y1 != y2:
                        edges.append((x1, y1, x2, y2))
            j0 = max(0, int((min(ys) - self.min_y) / r))
            j1 = min(self.ny - 1, int((max(ys) - self.min_y) / r) + 1)
            pt, pb = p["top"], p["base"]
            for j in range(j0, j1 + 1):
                yc = self.min_y + (j + 0.5) * r
                xs = []
                for x1, y1, x2, y2 in edges:
                    if (y1 <= yc < y2) or (y2 <= yc < y1):
                        xs.append(x1 + (yc - y1) * (x2 - x1) / (y2 - y1))
                if len(xs) < 2:
                    continue
                xs.sort()
                row = j * self.nx
                for k in range(0, len(xs) - 1, 2):
                    i0 = max(0, int(math.ceil((xs[k] - self.min_x) / r - 0.5)))
                    i1 = min(self.nx - 1, int(math.floor((xs[k + 1] - self.min_x) / r - 0.5)))
                    for i in range(i0, i1 + 1):
                        c = row + i
                        if pt > top[c]:
                            top[c] = pt
                            self.building_surface[c] = int(p["kind"] == "building")
                        if pb < base[c]:
                            base[c] = pb
        self.top, self.base = top, base

    # --- queries -------------------------------------------------------------------------------
    def roof_locations(self, center, radius):
        """Flat building tops with a mech-width margin inside the starting circle."""
        pad = math.ceil(2.5 / self.RES)
        result = []
        for cell, building in enumerate(self.building_surface):
            if not building:
                continue
            j, i = divmod(cell, self.nx)
            if i < pad or i >= self.nx - pad or j < pad or j >= self.ny - pad:
                continue
            x, y = self.min_x + (i + .5) * self.RES, self.min_y + (j + .5) * self.RES
            z = self.top[cell]
            if z <= self.ground(x, y) + 4 or math.hypot(x - center[0], y - center[1]) > radius:
                continue
            if all(self.building_surface[cell + dy * self.nx + dx] and
                   self.top[cell + dy * self.nx + dx] == z
                   for dy in range(-pad, pad + 1) for dx in range(-pad, pad + 1)):
                result.append((x, y, z))
        return result

    def ground(self, x, y):
        if not self.ok:
            return -1e9
        i = int((x - self.t_min_x) / self.t_step + 0.5)
        j = int((y - self.t_min_y) / self.t_step + 0.5)
        if i < 0:
            i = 0
        elif i >= self.t_nx:
            i = self.t_nx - 1
        if j < 0:
            j = 0
        elif j >= self.t_ny:
            j = self.t_ny - 1
        return self.terrain[j * self.t_nx + i] * 0.01

    def cell(self, x, y):
        i = int((x - self.min_x) / self.RES)
        j = int((y - self.min_y) / self.RES)
        if i < 0 or j < 0 or i >= self.nx or j >= self.ny:
            return -1
        return j * self.nx + i

    def los(self, a, b, step=2.5):
        """True if the segment a->b (3D points) is not blocked by buildings or terrain."""
        if not self.ok:
            return True
        ax, ay, az = a
        dx, dy, dz = b[0] - ax, b[1] - ay, b[2] - az
        d = math.sqrt(dx * dx + dy * dy + dz * dz)
        n = int(d / step)
        if n < 2:
            return True
        top, base, nx, ny, r = self.top, self.base, self.nx, self.ny, self.RES
        mx, my = self.min_x, self.min_y
        terr, tnx, tny, tmx, tmy = self.terrain, self.t_nx, self.t_ny, self.t_min_x, self.t_min_y
        inv = 1.0 / n
        for k in range(1, n):
            f = k * inv
            x = ax + dx * f
            y = ay + dy * f
            z = az + dz * f
            i = int((x - mx) / r)
            j = int((y - my) / r)
            if 0 <= i < nx and 0 <= j < ny:
                c = j * nx + i
                if base[c] <= z <= top[c]:
                    return False
            ti = int(x - tmx + 0.5)
            tj = int(y - tmy + 0.5)
            if 0 <= ti < tnx and 0 <= tj < tny and terr[tj * tnx + ti] * 0.01 > z + 0.5:
                return False
        return True

    def blocked(self, x, y, z, d=0.0):
        """Would a mech with feet at height z bump into something at (x, y)?"""
        if not self.ok:
            return False
        if x < self.bx0 + 4 or x > self.bx1 - 4 or y < self.by0 + 4 or y > self.by1 - 4:
            return True
        c = self.cell(x, y)
        if c < 0:
            return True
        if self.top[c] > z + 1.3 and self.base[c] < z + 9.0:
            return True
        g = self.ground(x, y)
        return g > z + 1.4 + 0.55 * d

    def obstacle_top(self, x, y):
        c = self.cell(x, y)
        if c < 0 or not self.ok:
            return -1e9
        return max(self.top[c], self.ground(x, y))
