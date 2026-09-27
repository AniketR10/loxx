package embed

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// golden is testdata/embed/golden.json, generated from sentence-transformers
// (full f32 weights) by tools/model/goldens.py.
type golden struct {
	Model        string `json:"model"`
	Revision     string `json:"revision"`
	MaxSeqLength int    `json:"max_seq_length"`
	Cases        []struct {
		Text      string    `json:"text"`
		IDs       []int32   `json:"ids"`
		Embedding []float32 `json:"embedding"`
	} `json:"cases"`
}

func loadGolden(t testing.TB) golden {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "embed", "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Cases) == 0 {
		t.Fatal("golden.json has no cases")
	}
	return g
}

func TestTokenizerGolden(t *testing.T) {
	g := loadGolden(t)
	tok, err := NewTokenizer(vocabTxt, g.MaxSeqLength)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range g.Cases {
		got := tok.Encode(c.Text)
		if !equalIDs(got, c.IDs) {
			t.Errorf("Encode(%q)\n  got  %v\n  want %v", c.Text, got, c.IDs)
		}
	}
}

func equalIDs(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func cosine(a, b []float32) float64 {
	var d, na, nb float64
	for i := range a {
		d += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return d / math.Sqrt(na*nb)
}

// checkEmbeddings compares m against the goldens and returns the worst cosine
// similarity.
func checkEmbeddings(t *testing.T, m *Model, minCosine float64) float64 {
	t.Helper()
	worst := 1.0
	for _, c := range loadGolden(t).Cases {
		got := m.Embed(c.Text)
		if len(got) != len(c.Embedding) {
			t.Fatalf("Embed(%q): %d dims, want %d", c.Text, len(got), len(c.Embedding))
		}
		cos := cosine(got, c.Embedding)
		worst = min(worst, cos)
		if cos < minCosine {
			t.Errorf("Embed(%q): cosine %.6f to reference, want >= %v", c.Text, cos, minCosine)
		}
	}
	return worst
}

// minCosineF16 is the agreement required between loc's shipped f16 model and
// the f32 sentence-transformers reference.
const minCosineF16 = 0.9999

func TestEmbedGoldenF16(t *testing.T) {
	m, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("worst cosine vs reference (f16 weights): %.7f", checkEmbeddings(t, m, minCosineF16))
}

// TestEmbedGoldenF32 checks the implementation alone, without f16 rounding.
// Point LOC_EMBED_F32 at the original model.safetensors to run it.
func TestEmbedGoldenF32(t *testing.T) {
	path := os.Getenv("LOC_EMBED_F32")
	if path == "" {
		t.Skip("set LOC_EMBED_F32 to the original f32 model.safetensors")
	}
	weights, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Load(weights, vocabTxt)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("worst cosine vs reference (f32 weights): %.7f", checkEmbeddings(t, m, 0.99999))
}
