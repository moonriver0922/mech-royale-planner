"""Exercise supply capture decisions with a quiet, protocol-shaped scenario from the bot kit."""

import argparse
import copy
import gzip
import importlib.util
import json
import math
from pathlib import Path
import sys

from practice_engine import horizontal_motion


def load_bot(path):
    sys.path.insert(0, str(path.parent))
    spec = importlib.util.spec_from_file_location("capture_policy", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.Bot


def fixture(kit):
    with gzip.open(kit / "check/stream.jsonl.gz", "rt") as stream:
        hello = json.loads(next(stream))
        obs = next(msg for line in stream if (msg := json.loads(line))["type"] == "obs")
    hello["map"]["dir"] = str(kit / "arena")
    me = obs["self"]
    x, y, z = me["pos"]
    cfg = hello["config"]["drops"]
    radius, capture_s = cfg["radius"], cfg["capture_s"]
    progress = capture_s - 0.9
    start = cfg["times"][0] + cfg["fall_s"] + progress
    me.update(hp=min(1000, me["max_hp"]), vel=[0, 0, 0], grounded=True, in_zone=True)
    obs.update(t=start, tick=round(start * 20), mechs=[], projectiles=[], sounds=[],
               beams=[], events=[], pickups=[], crates=[], offer=None)
    obs["zone"] = dict(center=[x, y], radius=380, next_center=[x, y], next_radius=240,
                       shrink_start=start + 60, shrink_end=start + 100, dps=35)
    obs["cubes"] = [dict(id=5001, pos=[x + 18, y, z])]
    obs["drops"] = [dict(id=20000, pos=[x - radius + 1, y, z], radius=radius,
                         lands_in=0, capture_s=capture_s, progress=progress, holder=me["id"])]
    return hello, obs


def stays_until_capture(bot_type, hello, initial):
    """Advance only ground movement in a clear lane during the last 0.9 seconds."""
    obs = copy.deepcopy(initial)
    pilot = bot_type(hello)
    me, drop = obs["self"], obs["drops"][0]
    dt = 0.1
    cfg = hello["config"]["mech"]
    for _ in range(9):
        action = pilot.act(obs)
        move = action.get("move", [0, 0])
        length = max(1.0, math.hypot(*move))
        speed = cfg["boost_speed"] if action.get("boost") else me["stats"]["walk_speed"]
        target = (move[0] * speed / length, move[1] * speed / length)
        match = hello["config"]["match"]
        me["vel"][0], me["vel"][1], dx, dy = horizontal_motion(
            me["vel"], target, cfg["ground_accel"], match["decide_every"], 1 / match["tick_hz"])
        me["pos"][0] += dx
        me["pos"][1] += dy
        if math.dist(me["pos"][:2], drop["pos"][:2]) > drop["radius"]:
            return False
        drop["progress"] += dt
        obs["t"] += dt
        obs["tick"] += 2
    return True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kit", type=Path, required=True, help="extracted official starter kit")
    parser.add_argument("--bot", type=Path, default=Path(__file__).resolve().parents[1] / "bot.py",
                        help="Python bot file exposing Bot(hello) and act(obs), default: repo bot.py")
    args = parser.parse_args()
    bot_type = load_bot(args.bot.resolve())
    hello, obs = fixture(args.kit.resolve())
    checks = {"stay in the ring for the remaining 0.9 s": stays_until_capture(bot_type, hello, obs)}

    # East leads toward both the outside cube and the emergency safe circle.
    # Chasing the active drop instead would move west toward its center.
    for case in ("foreign holder", "early capture", "urgent zone"):
        state = copy.deepcopy(obs)
        drop = state["drops"][0]
        if case == "foreign holder":
            drop["holder"] = (state["self"]["id"] + 1) % 16
        elif case == "early capture":
            drop["progress"] = 0.2
        else:
            x, y, _ = state["self"]["pos"]
            state["self"]["in_zone"] = False
            state["zone"].update(center=[x + 100, y], radius=40,
                                 next_center=[x + 100, y], next_radius=24)
        control = copy.deepcopy(state)
        control["drops"] = []
        normal = bot_type(hello).act(control)["move"]
        actual = bot_type(hello).act(state)["move"]
        checks[case + " retains ordinary movement"] = normal[0] > 0 and math.dist(actual, normal) < 1e-6

    for case, passed in checks.items():
        print(("PASS" if passed else "FAIL"), case)
    return 0 if all(checks.values()) else 1


if __name__ == "__main__":
    raise SystemExit(main())
