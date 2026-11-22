package decision

import "testing"

func TestRecoveryEntryBoundary(t *testing.T) {
	for _, tc := range []struct {
		hp      float64
		retreat bool
	}{{649, true}, {650, false}} {
		c, o := boostScene(t)
		o.Self.HP = tc.hp
		a := c.Decide(o)
		if (a.Move[0] < -.99) != tc.retreat {
			t.Fatalf("HP %g got %+v, retreat %v", tc.hp, a, tc.retreat)
		}
	}
}
