# Ownbot S00

r1ver's independent Go entry for Builders Mech Royale. The default entry point reads the competition protocol, loads the mounted arena, and runs S00. The repository requires only the Go standard library and the game's supplied map and config.

```sh
go test ./...
CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=false -o /tmp/ownbot-s00 ./cmd/ownbot
python3 /path/to/official-kit/check/check.py -- /tmp/ownbot-s00
```

The included packaging manifest points to the same executable. The map and test stream are supplied by the [official kit](https://www.joinbuilders.org/buildathon/tracks/builders/files/mechfight-kit.tar.gz).

[Decision design](docs/DESIGN.md) · [Implementation sources](docs/SOURCES.md)
