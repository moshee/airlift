package main

import (
	"crypto/cipher"
	"crypto/rand"

	"golang.org/x/crypto/twofish"
)

const (
	PasswordStorageMechanism = "the dotfile in almost plaintext because I don't know how this works"
)

type Config struct {
	Scheme string
	Host   string
	Port   string
	Pass   []byte
}

func getPassword(conf *Config) (string, error) {
	if conf.Pass == nil {
		return "", errPassNotFound
	}
	pass := make([]byte, len(conf.Pass)-32)
	fishify(pass, conf.Pass)
	return string(pass), nil
}

func updatePassword(conf *Config, newpass string) error {
	conf.Pass = make([]byte, 32+len(newpass))
	pass := conf.Pass[32:]
	copy(pass, newpass)
	rand.Read(conf.Pass[:32])
	fishify(pass, conf.Pass)
	return writeConfig(conf)
}

func fishify(dst, chunk []byte) {
	key := chunk[:16]
	iv := chunk[16:32]
	pass := chunk[32:]
	fish, _ := twofish.NewCipher(key)
	ctr := cipher.NewCTR(fish, iv)
	ctr.XORKeyStream(dst, pass)
}

func copyString(s string) error {
	return errNotCopying
}
