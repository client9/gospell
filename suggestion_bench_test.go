package gospell

import (
	"bytes"
	"io"
	"os"
	"testing"
)

var (
	benchAffBytes []byte
	benchDicBytes []byte
	benchQuery    = "sillly"
)

func loadBenchmarkReaders(b *testing.B) ([]byte, []byte) {
	b.Helper()

	if benchAffBytes != nil && benchDicBytes != nil {
		return benchAffBytes, benchDicBytes
	}

	affF, err := os.Open("hunspell-en_US/en_US.aff")
	if err != nil {
		b.Fatal(err)
	}
	benchAffBytes, err = io.ReadAll(affF)
	_ = affF.Close()
	if err != nil {
		b.Fatal(err)
	}

	dicF, err := os.Open("hunspell-en_US/en_US.dic")
	if err != nil {
		b.Fatal(err)
	}
	benchDicBytes, err = io.ReadAll(dicF)
	_ = dicF.Close()
	if err != nil {
		b.Fatal(err)
	}

	return benchAffBytes, benchDicBytes
}

func newBenchmarkSpell(b *testing.B) *GoSpell {
	b.Helper()
	affBytes, dicBytes := loadBenchmarkReaders(b)

	gs, err := NewGoSpellReader(bytes.NewReader(affBytes), bytes.NewReader(dicBytes))
	if err != nil {
		b.Fatal(err)
	}
	return gs
}

// benchmarkSuggestInit measures one-time index construction.
func benchmarkSuggestInit(b *testing.B, suggester Suggestions) {
	gs := newBenchmarkSpell(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := suggester.Init(gs); err != nil {
			b.Fatal(err)
		}
	}
}

// benchmarkSuggestQuery measures lookup only against an already initialized
// suggester. The index build happens once before the timer starts.
func benchmarkSuggestQuery(b *testing.B, suggester Suggestions, limit int) {
	gs := newBenchmarkSpell(b)
	if err := suggester.Init(gs); err != nil {
		b.Fatal(err)
	}
	gs.suggester = suggester

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := gs.Suggest(benchQuery, limit)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSetSuggesterLevenshtein(b *testing.B) {
	benchmarkSuggestInit(b, NewLevenshteinSuggester(LevenshteinOptions{MaxDistance: 2}))
}

func BenchmarkSuggestLevenshtein(b *testing.B) {
	benchmarkSuggestQuery(b, NewLevenshteinSuggester(LevenshteinOptions{MaxDistance: 2}), 5)
}

func BenchmarkSetSuggesterTrigram(b *testing.B) {
	benchmarkSuggestInit(b, NewTrigramSuggester(TrigramOptions{
		RerankLimit:   32,
		MaxLengthDiff: 4,
	}))
}

func BenchmarkSuggestTrigram(b *testing.B) {
	benchmarkSuggestQuery(b, NewTrigramSuggester(TrigramOptions{
		RerankLimit:   32,
		MaxLengthDiff: 4,
	}), 5)
}

func BenchmarkSetSuggesterSymSpell(b *testing.B) {
	benchmarkSuggestInit(b, NewSymSpellSuggester(SymSpellOptions{
		MaxDistance:  2,
		PrefixLength: 7,
	}))
}

func BenchmarkSuggestSymSpell(b *testing.B) {
	benchmarkSuggestQuery(b, NewSymSpellSuggester(SymSpellOptions{
		MaxDistance:  2,
		PrefixLength: 7,
	}), 5)
}

func BenchmarkSetSuggesterMutation(b *testing.B) {
	benchmarkSuggestInit(b, NewMutationSuggester(MutationOptions{
		CandidateCap: 256,
	}))
}

func BenchmarkSuggestMutation(b *testing.B) {
	benchmarkSuggestQuery(b, NewMutationSuggester(MutationOptions{
		CandidateCap: 256,
	}), 5)
}
