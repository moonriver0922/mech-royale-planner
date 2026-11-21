package mech

import "testing"

func TestContinuingLaserTarget(t *testing.T) {
	current := &Track{Seen: 31, LOS: true, Dist: 60.2}
	b := Brain{now: 31, stickID: 14, stickAt: 30.9, tracks: map[int]*Track{14: current}}
	laser := &WeaponState{Charge: 0.5, Stats: WeaponStats{Charge: 0.75, Range: 280}}
	if b.continuingLaserTarget(laser) != current {
		t.Fatal("lost the visible target as it crossed the engagement boundary")
	}
	if b.continuingLaserTarget(nil) != nil {
		t.Fatal("held without a laser")
	}
	for _, tc := range []struct {
		name                     string
		seen, dist, charge, last float64
		los                      bool
	}{
		{"blocked", 31, 60, .5, 30.9, false},
		{"unseen", 30.9, 60, .5, 30.9, true},
		{"out of weapon range", 31, 285, .5, 30.9, true},
		{"charge not invested", 31, 60, .1, 30.9, true},
		{"grace expired", 31, 60, .5, 30.5, true},
	} {
		current.Seen, current.Dist, current.LOS = tc.seen, tc.dist, tc.los
		laser.Charge, b.stickAt = tc.charge, tc.last
		if b.continuingLaserTarget(laser) != nil {
			t.Errorf("kept target when %s", tc.name)
		}
	}
}
