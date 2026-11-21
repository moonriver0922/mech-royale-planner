# Mech Royale practice entry

This entry adapts the MIT-licensed [LanceHuang245/mech-royale bot at commit 171bc06](https://github.com/LanceHuang245/mech-royale/tree/171bc0675b04c4095f5a3619cc63664b8a338922). Its Python protocol wrapper and original starting bot came from the [Builders Mech Royale kit](https://www.joinbuilders.org/buildathon/tracks/builders). The runtime uses Python's standard library and the map supplied by the scorer. This practice build uses normal walking speed with `MR_FLAGS` set to the empty string. It retains r1ver's safe-circle and supply-drop decisions. The comparison was motivated by [BackwardsDeployeds' normal-speed practice probe](https://github.com/brycehcmcgrath/mechfight-bot/commit/dd1e809f6ce769b2690ba25773c96bb7ebeff81b).

## Practice score and local development

The first scored entry, [97e0e58](https://github.com/moonriver0922/mech-royale-planner/tree/97e0e588ac9fb49533602a738344cb3fdc8b1551), earned **4.54** over 48 official practice matches (receipt `BB-407987E8`). The development version also finishes an uncontested supply drop when at most one second remains, provided no threat or circle decision takes priority.

Use the [practice simulator](tools/README.md) for paired runs against house-style bots and the [free-for-all simulator](tools/ffa/README.md) to compare two entrants in the same match with swapped spawns. Both run locally with the extracted organizer kit. The [experiment notes](experiments/README.md) include the public competitors studied, immutable source versions and measured results.

```sh
python3 -B tools/simulate.py --kit ../mechfight-kit --bot bot.py --baseline git:97e0e58 --seed 300 --matches 8
python3 -B tools/ffa/league.py --kit ../mechfight-kit --bot bot.py --baseline git:97e0e58 --seed 601 --pairs 4
python3 -B tools/check_capture.py --kit ../mechfight-kit
```

## Safe-circle change

The base policy estimates time remaining until a shrink starts. At the start of that shrink it changes the deadline to the end of the shrink. This can cancel a crossing in progress while the mech is still short of the next circle. This version remembers the circle that triggered travel until the mech reaches its inner margin. The commitment expires when the next circle changes. The existing danger and cover decisions still take priority where applicable.

## Run the official check

Run from this repository with the extracted starter kit available at `../mechfight-kit`.

```sh
python3 ../mechfight-kit/check/check.py -- python3 -u bot.py
docker build --platform linux/amd64 -t mech-royale-v1 .
python3 ../mechfight-kit/check/check.py --map-dir /arena -- docker run --rm -i --platform linux/amd64 --network none --cpus 1 --memory 512m --read-only --tmpfs /tmp:rw,size=64m --user 65534:65534 -v "$PWD/../mechfight-kit/arena:/arena:ro" mech-royale-v1
```

The official scorer determines placement points over 48 practice games. See [LICENSE](LICENSE) for the MIT terms and upstream notice.
