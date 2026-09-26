package scrub

import (
	"math"
	"regexp"
	"strings"
)

// Thresholds for the high-entropy fallback, tuned against testdata/secrets.
const (
	minRandomLen         = 20
	minEntropyBits       = 3.5  // Shannon entropy per character
	minClassChangeFactor = 0.45 // fraction of adjacent chars that switch class
)

// entropyRule is the last layer: it catches random-looking tokens that no
// known pattern or structural rule identified.
var entropyRule = rule{
	kind: "high-entropy",
	re:   regexp.MustCompile(`[A-Za-z0-9+/_=-]{20,}`),
	skip: func(s string, m []int) bool { return !looksRandom(s[m[0]:m[1]]) },
}

// looksRandom reports whether tok looks like a generated secret rather than a
// word, path, identifier, hash or UUID.
//
// Hex-only strings are deliberately skipped: git SHAs, digests and UUIDs are
// far more common in commands than hex secrets, which usually appear as
// KEY=value and are caught by the structural rules instead.
func looksRandom(tok string) bool {
	tok = strings.TrimRight(tok, "=")
	if len(tok) < minRandomLen {
		return false
	}
	var upper, lower, digit, hexOnly = false, false, false, true
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		switch {
		case 'A' <= c && c <= 'Z':
			upper = true
		case 'a' <= c && c <= 'z':
			lower = true
		case '0' <= c && c <= '9':
			digit = true
		}
		if !isHexOrDash(c) {
			hexOnly = false
		}
	}
	if hexOnly || !upper || !lower || !digit {
		return false
	}
	return shannonEntropy(tok) >= minEntropyBits && classChangeFactor(tok) >= minClassChangeFactor
}

func isHexOrDash(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F' || c == '-'
}

func shannonEntropy(s string) float64 {
	var counts [256]int
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	n := float64(len(s))
	h := 0.0
	for _, c := range counts {
		if c > 0 {
			p := float64(c) / n
			h -= p * math.Log2(p)
		}
	}
	return h
}

// classChangeFactor is the fraction of adjacent character pairs whose class
// (upper, lower, digit, other) differs. Random base62/base64 text scores
// around 0.6; words, CamelCase identifiers and paths score far lower.
func classChangeFactor(s string) float64 {
	changes := 0
	for i := 1; i < len(s); i++ {
		if charClass(s[i]) != charClass(s[i-1]) {
			changes++
		}
	}
	return float64(changes) / float64(len(s)-1)
}

func charClass(c byte) int {
	switch {
	case 'A' <= c && c <= 'Z':
		return 0
	case 'a' <= c && c <= 'z':
		return 1
	case '0' <= c && c <= '9':
		return 2
	}
	return 3
}
