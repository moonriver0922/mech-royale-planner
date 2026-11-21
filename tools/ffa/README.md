# Local free-for-all comparison

Run from the project root with Python 3.11+ and the extracted bot kit. Two versions of your bot compete against each other and fourteen independent scripted bots. Each seed runs twice with the two bot slots swapped to account for differences between starting positions.

```sh
python3 tools/ffa/league.py --kit ../mechfight-kit \
  --bot bot.py --baseline git:HEAD --seed 601 --pairs 8 \
  --output ffa-results.json
```

Both bot arguments accept a Python, JavaScript or TypeScript entry point, or a local Git revision such as `git:HEAD`. Use `git:REV:path` when the entry point has a different filename. JavaScript and TypeScript require a compatible Node runtime. Both bot processes receive the game's JSON-lines messages. The referee uses fixed terrain and rules. A slot keeps its own bot RNG seed across scenarios. The map seed stays inside the referee.

The report contains each starting slot, placing, placement points and survival time. Each seed contributes one mean point difference after swapping slots. A positive mean difference favors the candidate. A useful control is to run the same committed bot in both slots. Timing-sensitive bots can still make different decisions across runs.

The model includes shrinking zones, crates, drones, power cubes and supply drops, with simplified movement, visibility and inventory mechanics. Only the plating, armor and nanites upgrades have their full or partial modeled effects. The referee omits mortar simulation and rooftop crate spawns. Use the kit's conformance checker to verify response times.

The local free-for-all referee adapts the MIT-licensed LanceHuang245/mech-royale implementation. This version uses the project's shared practice engine and fixed world geometry, removes the experimental capability grants, adjusts placement ties, and runs contestant programs in separate processes. The original copyright notice is in [LICENSE](LICENSE). [Upstream referee at the pinned revision](https://github.com/LanceHuang245/mech-royale/blob/6bdf03b2c7031f333a6d37836ecee1b94d3f9127/tools/arena.py)
