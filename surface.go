package gospell

// surfaceEntry represents one generated surface form and the metadata needed
// to decide whether it is valid standalone and/or inside compounds.
type surfaceEntry struct {
	Word                  string
	RawFlags              []string
	StandaloneAllowed     bool
	CompoundStartAllowed  bool
	CompoundMiddleAllowed bool
	CompoundEndAllowed    bool
	CompoundForbidden     bool
	ForbiddenWord         bool // set by FORBIDDENWORD; blocks all use including case-fallback
	IsRoot                bool // true when the form came directly from a dic entry (state==0)
	OnlyInCompound        bool
}

type compoundPosition uint8

const (
	compoundPositionStart compoundPosition = iota
	compoundPositionMiddle
	compoundPositionEnd
)

func (e surfaceEntry) allowsStandalone() bool {
	return e.StandaloneAllowed && !e.OnlyInCompound
}

func (e surfaceEntry) allowsCompound(pos compoundPosition) bool {
	if e.CompoundForbidden {
		return false
	}
	switch pos {
	case compoundPositionStart:
		return e.CompoundStartAllowed
	case compoundPositionMiddle:
		return e.CompoundMiddleAllowed
	case compoundPositionEnd:
		return e.CompoundEndAllowed
	default:
		return false
	}
}
