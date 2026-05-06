package gospell

import (
	"fmt"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

func encodingForSET(set string) (encoding.Encoding, bool) {
	switch set {
	case "", "UTF-8":
		return nil, true
	case "ISO8859-1", "ISO-8859-1":
		return charmap.ISO8859_1, true
	case "ISO8859-2", "ISO-8859-2":
		return charmap.ISO8859_2, true
	case "ISO8859-15", "ISO-8859-15":
		return charmap.ISO8859_15, true
	default:
		return nil, false
	}
}

func decodeWithEncoding(enc encoding.Encoding, b []byte) ([]byte, error) {
	if enc == nil {
		return b, nil
	}
	out, err := enc.NewDecoder().Bytes(b)
	if err != nil {
		return nil, fmt.Errorf("decode bytes: %w", err)
	}
	return out, nil
}
