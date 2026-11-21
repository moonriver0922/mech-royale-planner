# Mech Royale practice entry

This entry adapts the MIT-licensed [LanceHuang245/mech-royale bot at commit 171bc06](https://github.com/LanceHuang245/mech-royale/tree/171bc0675b04c4095f5a3619cc63664b8a338922). Its Python protocol wrapper and original starting bot came from the [Builders Mech Royale kit](https://www.joinbuilders.org/buildathon/tracks/builders). The runtime uses Python's standard library and the map supplied by the scorer. The image enables the upstream `sneak` configuration.

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
