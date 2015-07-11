package shorthash

const (
	Chars      = "abcdefghijklmnopqrstuvwxyzZYXWVUTSRQPONMLKJIHGFEDCBA1234567890"
	Vowels     = "aeiou"
	Consonants = "bdfghklmnpqrstvwxyz"
)

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

// Memorable produces a shortened output with alternating consonants and
// vowels.
func Memorable(hash []byte, hashLen int) string {
	s := make([]byte, uint(hashLen))

	for i, b := range hash {
		s[i%hashLen] ^= b
	}

	for i := range s {
		if i%2 == 0 {
			s[i] = Consonants[int(s[i])%len(Consonants)]
		} else {
			s[i] = Vowels[int(s[i])%len(Vowels)]
		}
	}

	return string(s)
}
