"""Local 16-player free-for-all comparison with swapped starting slots."""
import argparse
from contextlib import ExitStack
import gzip
import json
from pathlib import Path
import random
from statistics import mean
import sys
from tempfile import TemporaryDirectory

TOOLS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TOOLS))
from simulate import BotProcess, resolve_bot
from practice_engine import MOVEMENT_MODEL
from sim_world import World
from arena import HunterBot, League


def play(seed, slots, scripts, greeting, world):
    with ExitStack() as cleanup:
        processes = {}

        def factory(label):
            def start(hello, _world):
                process = BotProcess(scripts[label])
                cleanup.callback(process.close)
                ready = process.exchange(hello, hello["config"]["bot"]["hello_budget_ms"] / 1000)
                if ready.get("type") != "ready":
                    raise ValueError(f"{label} failed the ready handshake")
                processes[label] = process
                return process
            return start

        field = [("hunter", HunterBot)] * 16
        for slot, label in zip(slots, ("candidate", "baseline")):
            field[slot] = (label, factory(label))
        league = League(greeting["config"], world, seed, field, greeting)
        result = {}
        for mech in league.run():
            label = mech["label"]
            if label in processes:
                processes[label].send({"type": "end", "place": mech["place"], "kills": mech["stats"].get("kills", 0)})
                proc = processes[label].proc
                proc.stdin.close()
                if proc.wait(timeout=3):
                    raise RuntimeError(f"{label} failed to finish cleanly")
                result[label] = {key: mech[key] for key in ("id", "place", "points", "alive_s")}
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kit", required=True, type=Path)
    parser.add_argument("--bot", default=str(TOOLS.parent / "bot.py"))
    parser.add_argument("--baseline", required=True, help="Bot script, git:REV, or git:REV:path")
    parser.add_argument("--seed", type=int, default=1, help="first scenario seed")
    parser.add_argument("--pairs", type=int, default=1, help="two full games per seed")
    parser.add_argument("--output", type=Path, help="save JSON report")
    args = parser.parse_args()
    if args.pairs < 1:
        parser.error("--pairs must be positive")

    kit = args.kit.resolve()
    with gzip.open(kit / "check" / "stream.jsonl.gz", "rt") as stream:
        greeting = json.loads(stream.readline())
    greeting["map"]["dir"] = str(kit / "arena")
    world = World(greeting["map"]["dir"])
    if not world.ok:
        parser.error(f"Cannot load map from {greeting['map']['dir']}")

    with TemporaryDirectory(prefix="mech-ffa-") as temp:
        versions = {"candidate": args.bot, "baseline": args.baseline}
        resolved = {k: resolve_bot(v, Path(temp) / k) for k, v in versions.items()}
        scripts = {k: x[0] for k, x in resolved.items()}
        report = {"model": "local-ffa-14-hunters", "bots": {k: x[1] for k, x in resolved.items()}, "pairs": []}
        report["movement_model"] = MOVEMENT_MODEL
        for seed in range(args.seed, args.seed + args.pairs):
            slots = random.Random(seed).sample(range(16), 2)
            games = [play(seed, order, scripts, greeting, world) for order in (slots, slots[::-1])]
            delta = mean(g["candidate"]["points"] - g["baseline"]["points"] for g in games)
            pair = {"seed": seed, "games": games, "mean_delta": delta}
            report["pairs"].append(pair)
            print(json.dumps(pair), flush=True)
    pairs = report["pairs"]
    summary = {"pairs": len(pairs), "games": 2 * len(pairs),
               "mean_points": {k: mean(g[k]["points"] for p in pairs for g in p["games"]) for k in versions},
               "mean_delta": mean(p["mean_delta"] for p in pairs)}
    report["summary"] = summary
    if args.output:
        args.output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(summary))


if __name__ == "__main__":
    main()
