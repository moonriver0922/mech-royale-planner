# Local free-for-all comparison

Run from the project root with Python 3.11+ and the extracted official bot kit. Two versions of your bot compete against each other and fourteen independent stock bots. Each seed runs twice, with the two contestant slots swapped. This accounts for the advantage of one spawn over the other.

```sh
python3 tools/ffa/league.py --kit ../mechfight-kit \
  --bot bot.py --baseline git:97e0e58 --seed 601 --pairs 8 \
  --output ffa-results.json
```

Both contestant arguments accept a Python, JavaScript or TypeScript entry point, or a local Git revision such as `git:97e0e58`. Use `git:REV:path` when the entry point has a different filename. JavaScript and TypeScript require a compatible Node runtime. All contestant processes receive the game's JSON-lines messages. The referee uses fixed terrain and rules independent of both contestants. A slot keeps its own bot RNG seed across scenarios. The map seed stays inside the referee.

The report contains each starting slot, placing, placement points and survival time. Each seed contributes one mean point difference after swapping slots. A positive mean difference favors the candidate. A useful control is to run the same committed bot in both slots. Timing-sensitive bots can still make different decisions across runs.

This field approximates a free-for-all using scripted opponents. The model includes shrinking zones, crates, drones, power cubes and supply drops, with simplified movement, visibility and inventory mechanics. Only the plating, armor and nanites upgrades have their full or partial modeled effects. The approximate referee omits mortar simulation and rooftop crate spawns. Use the official conformance check for timing and official practice submissions for practice scores.

The local free-for-all referee adapts the MIT-licensed LanceHuang245/mech-royale implementation. This version uses the project's shared practice engine and fixed world geometry, removes the experimental capability grants, adjusts placement ties, and runs contestant programs in separate processes. The original copyright notice is in [LICENSE](LICENSE). [Upstream referee at the pinned revision](https://github.com/LanceHuang245/mech-royale/blob/6bdf03b2c7031f333a6d37836ecee1b94d3f9127/tools/arena.py)
