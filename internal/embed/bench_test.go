package embed

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkLoad(b *testing.B) {
	for b.Loop() {
		if _, err := Load(weightsF16, vocabTxt); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEmbed(b *testing.B) {
	m, err := Default()
	if err != nil {
		b.Fatal(err)
	}
	inputs := []string{
		"git status",
		"docker run -d -v pgdata:/var/lib/postgresql/data -e POSTGRES_PASSWORD=<REDACTED:secret-env> postgres:16",
		"echo " + strings.Repeat("argument ", 300), // truncated to MaxTokens
	}
	for _, in := range inputs {
		tokens := len(m.tok.Encode(in))
		b.Run(fmt.Sprintf("%dtokens", tokens), func(b *testing.B) {
			for b.Loop() {
				m.Embed(in)
			}
		})
	}
}
