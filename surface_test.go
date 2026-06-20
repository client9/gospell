package gospell

import "testing"

func TestAllowsCompoundForbidden(t *testing.T) {
	e := surfaceEntry{
		CompoundForbidden:     true,
		CompoundStartAllowed:  true,
		CompoundMiddleAllowed: true,
		CompoundEndAllowed:    true,
	}
	for _, pos := range []compoundPosition{compoundPositionStart, compoundPositionMiddle, compoundPositionEnd} {
		if e.allowsCompound(pos) {
			t.Errorf("allowsCompound(pos=%d): want false when CompoundForbidden=true", pos)
		}
	}
}

func TestAllowsCompoundMiddle(t *testing.T) {
	if !(surfaceEntry{CompoundMiddleAllowed: true}).allowsCompound(compoundPositionMiddle) {
		t.Error("allowsCompound(middle) = false, want true")
	}
	if (surfaceEntry{}).allowsCompound(compoundPositionMiddle) {
		t.Error("allowsCompound(middle) = true, want false when not allowed")
	}
}

func TestAllowsCompoundUnknownPosition(t *testing.T) {
	e := surfaceEntry{CompoundStartAllowed: true, CompoundMiddleAllowed: true, CompoundEndAllowed: true}
	if e.allowsCompound(compoundPosition(99)) {
		t.Error("allowsCompound(unknown position) = true, want false")
	}
}
