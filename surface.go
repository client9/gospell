package gospell

// surfaceEntry represents one generated surface form and the metadata needed
// to decide whether it is valid standalone and/or inside compounds.
type surfaceEntry struct {
	Word                  string
	StandaloneAllowed     bool
	CompoundStartAllowed  bool
	CompoundMiddleAllowed bool
	CompoundEndAllowed    bool
	CompoundForbidden     bool
	OnlyInCompound        bool
	RawFlags              []string
}
