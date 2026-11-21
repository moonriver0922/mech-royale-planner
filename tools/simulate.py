"""Run one bot, or compare two, against a fixed approximate practice referee."""

import argparse
import gzip
import hashlib
import io
import json
import os
import platform
from pathlib import Path
from queue import Empty, Queue
import statistics
from shutil import which
import subprocess
import sys
from tempfile import TemporaryDirectory
from threading import Thread
from zipfile import ZipFile

from practice_engine import Sim
from sim_world import World


ROOT = Path(__file__).resolve().parents[1]


class BotProcess:
    def __init__(self, script):
        if script.suffix == ".py":
            command = [sys.executable, "-B", "-u", str(script)]
        elif script.suffix in {".js", ".ts", ".mjs", ".cjs", ".mts", ".cts"}:
            node = which("node")
            if not node:
                raise ValueError("Install Node.js to run a JavaScript or TypeScript bot")
            command = [node, str(script)]
        else:
            raise ValueError(f"Unsupported bot script extension: {script.suffix}")
        self.proc = subprocess.Popen(
            command, cwd=script.parent,
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True,
            encoding="utf-8", env={**os.environ, "PYTHONHASHSEED": "0"},
        )
        self.lines = Queue()
        self.reader = Thread(target=self._read, daemon=True)
        self.reader.start()

    def _read(self):
        try:
            for line in self.proc.stdout:
                self.lines.put(line)
        finally:
            self.lines.put(None)

    def send(self, msg):
        self.proc.stdin.write(json.dumps(msg, allow_nan=False, separators=(",", ":")) + "\n")
        self.proc.stdin.flush()

    def exchange(self, msg, timeout):
        self.send(msg)
        try:
            line = self.lines.get(timeout=timeout)
        except Empty as error:
            raise RuntimeError(f"Bot stalled during {msg['type']}") from error
        if line is None:
            raise RuntimeError(f"Bot exited during {msg['type']}")
        return json.loads(line)

    def act(self, obs):
        act = self.exchange(obs, 2.0)
        if not isinstance(act, dict) or act.get("tick") != obs["tick"]:
            raise ValueError(f"Expected action for tick {obs['tick']}, got {act!r}")
        return act

    def close(self):
        try:
            self.proc.stdin.close()
        except BrokenPipeError:
            pass
        if self.proc.poll() is None:
            self.proc.kill()
        self.proc.wait()
        self.reader.join()
        self.proc.stdout.close()


def resolve_bot(value, destination):
    if value.startswith("git:"):
        ref, _, filename = value[4:].partition(":")
        entry = Path(filename or "bot.py")
        if entry.is_absolute() or ".." in entry.parts:
            raise ValueError(f"Git bot entry point must be inside the snapshot: {entry}")
        git = ["git", "-C", str(ROOT)]
        sha = subprocess.check_output(
            [*git, "rev-parse", "--verify", "--end-of-options", ref + "^{commit}"], text=True
        ).strip()
        archive = subprocess.check_output([*git, "archive", "--format=zip", sha])
        with ZipFile(io.BytesIO(archive)) as snapshot:
            snapshot.extractall(destination)
        path = destination / entry
        source = f"git:{sha}:{entry}"
    else:
        path = Path(value).resolve()
        source = str(path)
    if not path.is_file():
        raise ValueError(f"Bot entry point does not exist: {path}")
    return path, {"source": source, "bot_sha256": hashlib.sha256(path.read_bytes()).hexdigest()}


def play(script, hello, world, seed, roof_locations=None):
    bot = BotProcess(script)
    try:
        budget = hello["config"]["bot"]["hello_budget_ms"] / 1000
        if bot.exchange(hello, budget).get("type") != "ready":
            raise ValueError("Expected a ready message")
        result = Sim(hello["config"], world, seed, bot, roof_locations=roof_locations).run(hello["config"]["match"]["duration_s"])
        bot.send({"type": "end", "place": result["place"], "kills": result["kills"]})
        bot.proc.stdin.close()
        if bot.proc.wait(timeout=3) != 0:
            raise RuntimeError(f"Bot exited with status {bot.proc.returncode}")
        return result
    finally:
        bot.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kit", type=Path, required=True, help="extracted official mechfight-kit directory")
    parser.add_argument("--bot", default=str(ROOT / "bot.py"), help="bot script path or git:REV[:path]")
    parser.add_argument("--baseline", help="optional second bot script path or git:REV[:path]")
    parser.add_argument("--roof-loot", action="store_true", help="place the kit's rooftop fraction on mapped building tops")
    parser.add_argument("--seed", type=int, default=1, help="first world seed")
    parser.add_argument("--matches", type=int, default=8, help="number of consecutive world seeds")
    parser.add_argument("--output", type=Path, help="write full JSON report to this path")
    args = parser.parse_args()
    if args.matches < 1:
        parser.error("--matches must be positive")
    kit = args.kit.resolve()
    recording = kit / "check" / "stream.jsonl.gz"
    with gzip.open(recording, "rt") as stream:
        hello = json.loads(stream.readline())
    # The protocol's hello.seed is a per-slot bot RNG seed, independent of the world.
    # Keep the recorded value fixed and expose no scenario seed to either bot.
    hello["slot"] = 0
    hello["map"]["dir"] = str(kit / "arena")
    world = World(hello["map"]["dir"])
    if not world.ok:
        raise ValueError(f"Failed to load the arena in {kit}")

    roof_locations = None
    if args.roof_loot:
        zone = hello["config"]["zone"]
        roof_locations = world.roof_locations(zone["start_center"], zone["start_radius"] * 0.9)
        if len(roof_locations) < hello["config"]["loot"]["floor_count"]:
            raise ValueError("Too few flat building-top cells found for roof-loot trials")
    with TemporaryDirectory(prefix="mech-bots-") as temp:
        sources = {"candidate": args.bot}
        if args.baseline:
            sources["baseline"] = args.baseline
        bots = {name: resolve_bot(source, Path(temp) / name) for name, source in sources.items()}
        report = {
            "model": "approximate-practice-roof-loot" if args.roof_loot else "approximate-practice", "first_seed": args.seed, "matches": args.matches,
            "bot_rng_seed": hello["seed"], "python": platform.python_version(),
            "MR_FLAGS": os.environ.get("MR_FLAGS"),
            "inputs_sha256": {
                str(p.relative_to(kit)): hashlib.sha256(p.read_bytes()).hexdigest()
                for p in [recording, *(kit / "arena" / f for f in hello["map"]["files"])]
            },
            "bots": {k: v[1] for k, v in bots.items()}, "games": [],
        }
        for seed in range(args.seed, args.seed + args.matches):
            row = {"seed": seed}
            for name, (path, _) in bots.items():
                try:
                    row[name] = play(path, hello, world, seed, roof_locations)
                except Exception as error:
                    raise RuntimeError(f"{name}, seed {seed}: {error}") from error
            if args.baseline:
                row["delta"] = row["candidate"]["points"] - row["baseline"]["points"]
            report["games"].append(row)
            print(json.dumps(row, separators=(",", ":")), flush=True)

    games = report["games"]
    summary = {"mean_points": {k: statistics.mean(g[k]["points"] for g in games) for k in bots}}
    if args.baseline:
        deltas = [g["delta"] for g in games]
        summary.update(mean_delta=statistics.mean(deltas), better=sum(x > 0 for x in deltas),
                       tied=deltas.count(0), worse=sum(x < 0 for x in deltas))
    report["summary"] = summary
    if args.output:
        args.output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(summary))


if __name__ == "__main__":
    main()
