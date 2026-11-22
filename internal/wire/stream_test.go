package wire

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type decideFunc func(Observation) Action

func (fn decideFunc) Decide(o Observation) Action { return fn(o) }

func TestBurstEchoesNewestTickAndPreservesZeroPick(t *testing.T) {
	in, err := os.CreateTemp(t.TempDir(), "records")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	input := strings.Join([]string{
		`{"type":"hello","protocol":"mechfight/1","config":{"loot":{"weights":{"mortar":1},"pickup_radius":4}}}`,
		`{"type":"obs","tick":10}`,
		`{"type":"obs","tick":12,"self":{"weapons":[null,{"kind":"laser"}]}}`,
		`{"type":"end"}`, ""}, "\n")
	if _, err = in.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if _, err = in.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = Serve(in, &output, "test", func(h Hello) (Pilot, error) {
		if h.Config.Loot["pickup_radius"] != 4 {
			t.Fatal("pickup radius lost")
		}
		return decideFunc(func(o Observation) Action {
			if o.Tick != 12 || o.Self.Weapons[0] != nil || o.Self.Weapons[1].Kind != "laser" {
				t.Fatalf("unexpected observation: %+v", o)
			}
			pick := 0
			return Action{Pick: &pick}
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d protocol messages", len(lines))
	}
	var ready struct{ Type string }
	if err = json.Unmarshal([]byte(lines[0]), &ready); err != nil || ready.Type != "ready" {
		t.Fatal(lines[0])
	}
	var action Action
	if err = json.Unmarshal([]byte(lines[1]), &action); err != nil || action.Tick != 12 || action.Pick == nil || *action.Pick != 0 {
		t.Fatal(lines[1])
	}
}
