"""Check horizontal acceleration against the opening straight walk in the official recording."""

import argparse
import gzip
import json
import math
from pathlib import Path

from practice_engine import Sim
from sim_world import World


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kit", type=Path, required=True)
    kit = parser.parse_args().kit.resolve()
    with gzip.open(kit / "check/stream.jsonl.gz", "rt") as stream:
        cfg = json.loads(next(stream))["config"]
        frames = [o for line in stream if (o := json.loads(line))["type"] == "obs" and o["t"] <= 2.0]

    # Actions aren't in the recording. Infer the heading from steady motion late
    # in the segment, then replay the acceleration from the recorded resting pose.
    initial = frames[0]["self"]
    steady = frames[-1]["self"]["vel"][:2]
    walk_speed = cfg["mech"]["walk_speed"]
    assert math.isclose(math.hypot(*steady), walk_speed, abs_tol=0.01)
    assert all(o["self"]["vel"][:2] == steady for o in frames[2:])
    move = [v / math.hypot(*steady) for v in steady]
    sim = Sim(cfg, World(str(kit / "arena")), 1, None)
    mech = sim.me
    mech.pos, mech.vel = list(initial["pos"]), list(initial["vel"])
    mech.grounded = initial["grounded"]
    pos_errors, vel_errors = [], []
    for prev, obs in zip(frames, frames[1:]):
        sim.t = prev["t"]
        sim.apply(mech, {"move": move}, [])
        recorded = obs["self"]
        pos_errors.append(math.dist(mech.pos[:2], recorded["pos"][:2]))
        vel_errors.append(math.dist(mech.vel[:2], recorded["vel"][:2]))
        if len(pos_errors) == 1:
            print(f"0.1s: local XY {mech.pos[:2]}, recorded {recorded['pos'][:2]}")
            print(f"      local velocity {mech.vel[:2]}, recorded {recorded['vel'][:2]}")

    print(f"{len(pos_errors)} frames; maximum XY error {max(pos_errors):.4f}m; "
          f"maximum velocity error {max(vel_errors):.4f}m/s")
    if max(pos_errors) > 0.02 or max(vel_errors) > 0.01:
        raise SystemExit("movement exceeds tolerance for rounded recorded observations")


if __name__ == "__main__":
    main()
