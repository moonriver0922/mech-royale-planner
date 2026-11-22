package main

import (
	"fmt"
	"os"

	"ownbot/internal/arena"
	"ownbot/internal/decision"
	"ownbot/internal/wire"
)

func main() {
	err := wire.Serve(os.Stdin, os.Stdout, "Ownbot", func(h wire.Hello) (wire.Pilot, error) {
		world, err := arena.LoadMap(h.Map.Dir)
		if err != nil {
			return nil, fmt.Errorf("load arena: %w", err)
		}
		return decision.NewController(h.Config, world), nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
