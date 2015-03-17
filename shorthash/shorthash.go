package shorthash

const Chars = "abcdefghijklmnopqrstuvwxyzZYXWVUTSRQPONMLKJIHGFEDCBA1234567890"

// Make squashes a long hash into a shorter one represented as hashLen base62
// characters.
func Make(hash []byte, hashLen int) string {
	s := make([]byte, uint(hashLen))

	for i, b := range hash {
		s[i%hashLen] ^= b
	}
	for i := range s {
		s[i] = Chars[int(s[i])%len(Chars)]
	}

	return string(s)
}
