# Local simulator

One bot plays 15 cooperating scripted opponents. In a paired run, the baseline and candidate each play their own copy of every starting scenario. The referee uses the same map model and opponent implementation for both. Use a recent Python 3 with the standard library and Git for commit comparisons. JavaScript bots use Node.js. The TypeScript examples run directly with Node 22.18 or newer.

## Run from a clean checkout

Extract `mechfight-kit.tar.gz` next to the repository. Run these commands from the repository root.

```sh
tar -xzf ../mechfight-kit.tar.gz -C ..
python3 -B tools/simulate.py --kit ../mechfight-kit --seed 40 --matches 2
```

To compare the working tree with a Git revision, keep the seed range the same for both versions. The archived baseline runs with its own protocol wrapper and neighboring modules. Pass `git:HEAD` for the current commit or substitute a revision that exists in the local clone.

```sh
MR_FLAGS=sneak python3 -B tools/simulate.py --kit ../mechfight-kit \
  --bot bot.py --baseline git:HEAD \
  --seed 300 --matches 32 --output practice-300.json
```

Both `--bot` and `--baseline` accept a Python, JavaScript or TypeScript script path, or `git:REV[:path]`. A Git snapshot defaults to `bot.py` at its root. For example, `git:HEAD:src/index.ts` uses the tracked entry and its sibling modules. Native TypeScript execution requires Node-compatible erasable TypeScript and explicit module imports. For projects with their own build, pass the built JavaScript entry point. To compare identical copies, use the same revision for both options. To run the kit examples, use `--bot ../mechfight-kit/python/example_bot.py` or `--bot ../mechfight-kit/ts/example.ts`.

## Read the results

The CLI prints a JSON row per seed and a final summary. Each row records finishing place, placement points, survival time, seconds outside the circle, credited eliminations, and the maximum and p99 decision latency including local protocol I/O. The paired delta is candidate points minus baseline points. The summary counts improved, equal and worse outcomes. For a range beginning at 300 with 32 matches, each variant plays seeds 300 through 331.

The optional report retains all rows, resolved Git revisions or local entry-point hashes, kit file hashes, Python version, the per-bot RNG seed and `MR_FLAGS`. Local file hashes cover the entry point only. Use Git revisions to fix every dependency for a comparison. A fresh process runs each match in the entry point's directory so each bot loads its own modules. A slow machine, tight wall-clock planning budgets or a bot's own randomness can still change outcomes across reruns.

## Roof loot scenario

Add `--roof-loot` to either practice command to compare strategies that change how a bot values elevated pickups. The kit sets `floor_count` to 40 and `floor_roof_fraction` to 0.25. For each world seed the scenario relocates 10 of the 40 initial items to distinct, level building-top grid cells, preserving weapon kinds and the other 30 ground positions. It excludes bridge and stadium surfaces and keeps a footprint margin from roof edges. The report labels this scenario `approximate-practice-roof-loot` and counts distinct initial roof items seen and taken per bot.

This scenario uses the protocol's horizontal pickup radius and 4 m vertical tolerance. A bot must reach pickup height, using the existing jump and powered ascent where terrain requires it. A point can be on a flat roof without being reachable from the bot's current position or energy. Overhangs, rounded chassis contact and catching a roof edge remain approximate on the two-metre raster. Standard practice retains its earlier ground-only layout and horizontal pickup check for reproducible comparisons.

## Model and measurement limits

Both referees integrate horizontal velocity over the two physics ticks in each decision. They limit the magnitude of the change in velocity using the kit's ground or air acceleration. Reports identify this revision with `movement_model: horizontal-vector-subticks-v1`. To check the first two seconds of unobstructed walking against the kit recording, run `python3 -B tools/check_movement.py --kit ../mechfight-kit`. The recording rounds position and velocity to two decimal places. The model resolves walls and vertical movement once per decision.

The referee uses the kit's recorded configuration and terrain. It advances 0.1 simulated seconds per decision. Both policies receive the same recorded per-slot RNG seed and slot 0. The world seed stays inside the referee. Once actions diverge, the matches can evolve differently. Each observation exposes the currently interpolated circle and the next circle. A dead mech shares its place with others eliminated in that tick, immediately below the survivors. At time expiry surviving mechs are ordered by HP.

This model uses approximate opponents, movement and equipment rules. Crates, NPCs, sounds, beams, contested drops and upgrade offers are absent. The model runs actions to completion and aborts stalled processes. Use the kit's `check/check.py` to check protocol conformance and response time. For free-for-all comparisons see [the FFA runner](ffa/README.md).

The referee and fixed map queries adapt the upstream MIT [practice simulator](https://github.com/LanceHuang245/mech-royale/blob/171bc0675b04c4095f5a3619cc63664b8a338922/tools/sim.py) and [World class](https://github.com/LanceHuang245/mech-royale/blob/171bc0675b04c4095f5a3619cc63664b8a338922/bot.py) by LanceHuang245. The repository retains its [license and notice](../LICENSE). This adaptation adds the protocol process runner, independent map loading, paired reporting, kit fingerprints, and corrections to kill accounting and placement ties.

## Reproducing a timing-sensitive route decision

Time-limited map searches may finish different amounts of route planning under CPU contention. For a Node.js diagnostic, advance the bot's monotonic clock by a fixed 0.3 ms per call. Each planning time check then consumes the same logical budget. The Python referee still measures actual response times.

```sh
NODE_OPTIONS="--require=\"$PWD/tools/comparison-clock.cjs\"" python3 -B tools/simulate.py --kit ../mechfight-kit \
  --bot ../mechfight-kit/ts/example.ts --baseline ../mechfight-kit/ts/example.ts \
  --roof-loot --seed 301 --matches 2
```

The example compares identical entry points. Substitute the bot under test for both paths to isolate decision differences in bots that use the monotonic clock. Run the kit's conformance checker and repeat with the real clock to measure response time under normal execution.
