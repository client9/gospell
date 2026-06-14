package gospell

import (
	"bytes"
	"io"
	"os"
	"testing"
	"unicode/utf8"
)

func readBenchDictFiles(b *testing.B) (affBytes, dicBytes []byte) {
	b.Helper()
	affF, err := os.Open(testDictDir + "/en_US.aff")
	if err != nil {
		b.Fatal(err)
	}
	affBytes, err = io.ReadAll(affF)
	_ = affF.Close()
	if err != nil {
		b.Fatal(err)
	}
	dicF, err := os.Open(testDictDir + "/en_US.dic")
	if err != nil {
		b.Fatal(err)
	}
	dicBytes, err = io.ReadAll(dicF)
	_ = dicF.Close()
	if err != nil {
		b.Fatal(err)
	}
	return affBytes, dicBytes
}

// BenchmarkLoad measures end-to-end load time including file I/O.
func BenchmarkLoad(b *testing.B) {
	aff := testDictDir + "/en_US.aff"
	dic := testDictDir + "/en_US.dic"
	b.ResetTimer()
	for b.Loop() {
		if _, err := NewGoSpell(aff, dic); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLoadFromMemory measures parse+expand+index build with files
// pre-read into memory so disk I/O does not contribute to the measurement.
func BenchmarkLoadFromMemory(b *testing.B) {
	affBytes, dicBytes := readBenchDictFiles(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := NewGoSpellReader(bytes.NewReader(affBytes), bytes.NewReader(dicBytes)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLoadIndexBuild isolates the dictByRuneLen construction step that
// runs at the end of NewGoSpellReader. Load once outside the timer, then
// benchmark just the index build in a loop.
func BenchmarkLoadIndexBuild(b *testing.B) {
	affBytes, dicBytes := readBenchDictFiles(b)
	gs, err := NewGoSpellReader(bytes.NewReader(affBytes), bytes.NewReader(dicBytes))
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		idx := make(map[int][]string, gs.maxWordLen)
		for w := range gs.dict {
			rl := utf8.RuneCountInString(w)
			idx[rl] = append(idx[rl], w)
		}
	}
}

// BenchmarkCompoundTypoMatchesDict compares the typo guard with and without
// the rune-length index. WithIndex is the normal path; WithoutIndex simulates
// a nil index so we can measure the full O(dict_size) scan for reference.
func BenchmarkCompoundTypoMatchesDict(b *testing.B) {
	affBytes, dicBytes := readBenchDictFiles(b)
	gs, err := NewGoSpellReader(bytes.NewReader(affBytes), bytes.NewReader(dicBytes))
	if err != nil {
		b.Fatal(err)
	}

	// "hello" is 5 runes; en_US has ~1500 five-letter words out of ~90k total.
	word := "hello"

	b.Run("WithIndex", func(b *testing.B) {
		for b.Loop() {
			gs.compoundTypoMatchesDict(word)
		}
	})

	b.Run("WithoutIndex", func(b *testing.B) {
		saved := gs.dictByRuneLen
		gs.dictByRuneLen = nil
		defer func() { gs.dictByRuneLen = saved }()
		for b.Loop() {
			gs.compoundTypoMatchesDict(word)
		}
	})
}
