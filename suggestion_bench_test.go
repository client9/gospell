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

	affF, err := os.Open("hunspell-en_US-2026/en_US.aff")
	if err != nil {
		b.Fatal(err)
	}
	benchAffBytes, err = io.ReadAll(affF)
	_ = affF.Close()
	if err != nil {
		b.Fatal(err)
	}

	dicF, err := os.Open("hunspell-en_US-2026/en_US.dic")
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

func benchmarkSuggestEngine(b *testing.B, suggester Suggestions) {
	gs := newBenchmarkSpell(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := gs.SetSuggester(suggester); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkSuggestQuery(b *testing.B, suggester Suggestions, limit int) {
	gs := newBenchmarkSpell(b)
	if err := gs.SetSuggester(suggester); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := gs.Suggest(benchQuery, limit)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSetSuggesterLevenshtein(b *testing.B) {
	benchmarkSuggestEngine(b, NewLevenshteinSuggester(LevenshteinOptions{MaxDistance: 2}))
}

func BenchmarkSuggestLevenshtein(b *testing.B) {
	benchmarkSuggestQuery(b, NewLevenshteinSuggester(LevenshteinOptions{MaxDistance: 2}), 5)
}

func BenchmarkSetSuggesterTrigram(b *testing.B) {
	benchmarkSuggestEngine(b, NewTrigramSuggester(TrigramOptions{
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
