package util

import (
	"math/rand"
)

const (
	alphabetsLowerCase = "abcdefghijklmnopqrstuvwxyz"
)

func AlphabetsLowerCase(n int) string {
	return string(randBytes(alphabetsLowerCase, n))
}

func randBytes(pattern string, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = pattern[rand.Intn(len(pattern))]
	}
	return b
}
