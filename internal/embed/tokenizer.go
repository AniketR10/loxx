package embed

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// maxWordRunes matches the WordPiece max_input_chars_per_word default: longer
// words become a single [UNK].
const maxWordRunes = 100

// Tokenizer is an uncased BERT WordPiece tokenizer. It reproduces the Hugging
// Face fast tokenizer used by sentence-transformers: BertNormalizer (clean
// text, isolate CJK characters, strip accents, lowercase), BertPreTokenizer
// (split on whitespace and punctuation), WordPiece, then [CLS] ... [SEP].
type Tokenizer struct {
	vocab         map[string]int32
	unk, cls, sep int32
	maxLen        int // including [CLS] and [SEP]
}

// NewTokenizer builds a tokenizer from a vocab.txt file (one token per line,
// the line number is the id). maxLen is the maximum sequence length,
// including the [CLS] and [SEP] tokens.
func NewTokenizer(vocabTxt []byte, maxLen int) (*Tokenizer, error) {
	vocab := make(map[string]int32, 32000)
	sc := bufio.NewScanner(bytes.NewReader(vocabTxt))
	for id := int32(0); sc.Scan(); id++ {
		vocab[sc.Text()] = id
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	t := &Tokenizer{vocab: vocab, maxLen: maxLen}
	for _, special := range []struct {
		name string
		id   *int32
	}{{"[UNK]", &t.unk}, {"[CLS]", &t.cls}, {"[SEP]", &t.sep}} {
		id, ok := vocab[special.name]
		if !ok {
			return nil, fmt.Errorf("vocab is missing %s", special.name)
		}
		*special.id = id
	}
	if maxLen < 2 {
		return nil, fmt.Errorf("maxLen %d leaves no room for [CLS] and [SEP]", maxLen)
	}
	return t, nil
}

// Encode returns the token ids for text, starting with [CLS] and ending with
// [SEP], truncated to the tokenizer's maximum length.
func (t *Tokenizer) Encode(text string) []int32 {
	ids := []int32{t.cls}
	limit := t.maxLen - 1 // leave room for [SEP]
	for _, word := range preTokenize(normalize(text)) {
		ids = t.wordPiece(word, ids)
		if len(ids) >= limit {
			ids = ids[:limit]
			break
		}
	}
	return append(ids, t.sep)
}

// normalize applies BertNormalizer with clean_text, handle_chinese_chars,
// strip_accents and lowercase all enabled, in that order.
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == 0 || r == utf8.RuneError || isControl(r):
			// dropped
		case isWhitespace(r):
			b.WriteByte(' ')
		case isCJK(r):
			b.WriteByte(' ')
			b.WriteRune(r)
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	// Strip accents: decompose, then drop nonspacing marks.
	decomposed := norm.NFD.String(b.String())
	b.Reset()
	for _, r := range decomposed {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// preTokenize splits on whitespace and makes every punctuation character its
// own word, like BertPreTokenizer.
func preTokenize(s string) []string {
	var words []string
	start := -1
	for i, r := range s {
		switch {
		case r == ' ':
			if start >= 0 {
				words = append(words, s[start:i])
				start = -1
			}
		case isPunct(r):
			if start >= 0 {
				words = append(words, s[start:i])
				start = -1
			}
			words = append(words, s[i:i+utf8.RuneLen(r)])
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		words = append(words, s[start:])
	}
	return words
}

// wordPiece appends the ids for word using greedy longest-match-first
// WordPiece. A word that cannot be fully matched becomes a single [UNK].
func (t *Tokenizer) wordPiece(word string, ids []int32) []int32 {
	if utf8.RuneCountInString(word) > maxWordRunes {
		return append(ids, t.unk)
	}
	n := len(ids)
	for start := 0; start < len(word); {
		end := len(word)
		found := int32(-1)
		for end > start {
			piece := word[start:end]
			if start > 0 {
				piece = "##" + piece
			}
			if id, ok := t.vocab[piece]; ok {
				found = id
				break
			}
			_, size := utf8.DecodeLastRuneInString(word[start:end])
			end -= size
		}
		if found < 0 {
			return append(ids[:n], t.unk)
		}
		ids = append(ids, found)
		start = end
	}
	return ids
}

// isControl matches the tokenizers crate: tab, newline and carriage return
// count as whitespace; every other "Other" category character is dropped.
func isControl(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs)
}

func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || unicode.IsSpace(r)
}

// isPunct matches BERT's definition: all ASCII punctuation and symbols, plus
// every Unicode punctuation character.
func isPunct(r rune) bool {
	return (r >= 33 && r <= 47) || (r >= 58 && r <= 64) || (r >= 91 && r <= 96) ||
		(r >= 123 && r <= 126) || unicode.IsPunct(r)
}

// isCJK reports whether r is in the CJK Unified Ideographs blocks that BERT
// treats as separate words.
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x20000 && r <= 0x2A6DF) ||
		(r >= 0x2A700 && r <= 0x2B73F) ||
		(r >= 0x2B740 && r <= 0x2B81F) ||
		(r >= 0x2B820 && r <= 0x2CEAF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x2F800 && r <= 0x2FA1F)
}
