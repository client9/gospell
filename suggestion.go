package gospell

// SuggestionSource provides read-only access to a loaded dictionary so a
// suggester can build indexes or other internal structures.
type SuggestionSource interface {
	ForEachWord(func(word string) bool)
	HasWord(word string) bool
	WordCount() int
	MaxWordLen() int
}

// Suggestion is a ranked candidate returned by a suggestion engine.
type Suggestion struct {
	Word  string
	Score int
}

// Suggestions is the pluggable suggestion engine interface used by GoSpell.
type Suggestions interface {
	Init(src SuggestionSource) error
	Suggest(word string, limit int) ([]Suggestion, error)
}

func absIntLocal(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
