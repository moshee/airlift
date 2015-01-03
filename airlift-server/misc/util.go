package misc

import (
	"fmt"
	"strconv"
)

// Bytes represents a number of bytes which can nicely format itself.
type Bytes uint64

const (
	KB = 1024 << (10 * iota)
	MB
	GB
	TB
)

func (b Bytes) String() string {
	n := 0.0
	s := ""
	switch {
	case b < 0:
		return ""
	case b < KB:
		return fmt.Sprintf("%dB", b)
	case b < MB:
		s = "k"
		n = float64(b) / KB
	case b < GB:
		s = "M"
		n = float64(b) / MB
	case b < TB:
		s = "G"
		n = float64(b) / GB
	default:
		s = "T"
		n = float64(b) / TB
	}

	return strconv.FormatFloat(round(n, 1), 'f', -1, 64) + s
}

// round to prec digits
func round(n float64, prec int) float64 {
	n *= float64(prec) * 10
	x := float64(int64(n + 0.5))
	return x / (float64(prec) * 10)
}

// makeHash squashes a long hash into a shorter one represented as four base62
// characters.
func MakeHash(hash []byte) string {
	const (
		hashLen = 4
		chars   = "abcdefghijklmnopqrstuvwxyzZYXWVUTSRQPONMLKJIHGFEDCBA1234567890"
	)

	s := make([]byte, hashLen)

	for i, b := range hash {
		s[i%hashLen] ^= b
	}
	for i := range s {
		s[i] = chars[int(s[i])%len(chars)]
	}

	return string(s)
}

type SpaceConstrainedInt int

func (s SpaceConstrainedInt) String() string {
	switch {
	case s < 1000:
		return strconv.Itoa(int(s))
	case s < 1000000:
		f := float64(s) / 1000.0
		return strconv.FormatFloat(round(f, 1), 'f', -1, 64) + "k"
	}

	f := float64(s) / 1000000.0
	return strconv.FormatFloat(round(f, 1), 'f', -1, 64) + "M"
}

func (s SpaceConstrainedInt) Raw() string {
	return strconv.Itoa(int(s))
}
