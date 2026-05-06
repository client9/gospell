package gospell

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func BenchmarkLoad(b *testing.B) {
	b.Skip()
	for i := 0; i < b.N; i++ {
		_, err := NewGoSpell("hunspell-en_US-2026/en_US.aff", "hunspell-en_US-2026/en_US.dic")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLoadFromMemory isolates parse+expand cost by pre-reading files into
// memory, so disk I/O doesn't contribute to the measurement.
func BenchmarkLoadFromMemory(b *testing.B) {
	b.Skip()
	affF, err := os.Open("hunspell-en_US-2026/en_US.aff")
	if err != nil {
		b.Fatal(err)
	}
	affBytes, err := io.ReadAll(affF)
	_ = affF.Close()
	if err != nil {
		b.Fatal(err)
	}

	dicF, err := os.Open("hunspell-en_US-2026/en_US.dic")
	if err != nil {
		b.Fatal(err)
	}
	dicBytes, err := io.ReadAll(dicF)
	_ = dicF.Close()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := NewGoSpellReader(bytes.NewReader(affBytes), bytes.NewReader(dicBytes))
		if err != nil {
			b.Fatal(err)
		}
	}
}
