package embed

import (
	"fmt"
	"math"
	"sync"
)

// Architecture of all-MiniLM-L6-v2 (see ROADMAP.md §3).
const (
	Dims      = 384 // output vector size
	MaxTokens = 256 // sentence-transformers max_seq_length, incl. [CLS] and [SEP]

	hidden  = Dims
	heads   = 12
	headDim = hidden / heads
	ffn     = 1536
	layers  = 6
	lnEps   = 1e-12
)

// ModelID identifies the model and weights that produced a vector. Vectors
// with different ids must never be compared.
const ModelID = "all-MiniLM-L6-v2@1110a243/f16"

// Model is a loaded embedding model. It is immutable and safe for concurrent
// use.
type Model struct {
	tok       *Tokenizer
	word      []float32 // [vocab][hidden]
	pos       []float32 // [512][hidden]
	tokenType []float32 // row 0 of [2][hidden]; single-segment input
	embNorm   layerNorm
	layers    [layers]layer
}

type layer struct {
	query, key, value, attnOut linear
	attnNorm                   layerNorm
	intermediate, output       linear
	outNorm                    layerNorm
}

// linear computes y = x·Wᵀ + b with W stored [out][in], as in PyTorch.
type linear struct {
	w, b    []float32
	in, out int
}

type layerNorm struct{ gamma, beta []float32 }

var (
	defaultOnce  sync.Once
	defaultModel *Model
	defaultErr   error
)

// Default returns the model compiled into the binary, loading it on first use.
func Default() (*Model, error) {
	defaultOnce.Do(func() { defaultModel, defaultErr = Load(weightsF16, vocabTxt) })
	return defaultModel, defaultErr
}

// Load builds a model from safetensors weights (F16 or F32) and a vocab.txt.
func Load(weights, vocab []byte) (*Model, error) {
	tok, err := NewTokenizer(vocab, MaxTokens)
	if err != nil {
		return nil, err
	}
	ts, err := readSafetensors(weights)
	if err != nil {
		return nil, err
	}
	l := loader{ts: ts}
	m := &Model{
		tok:       tok,
		word:      l.get("embeddings.word_embeddings.weight", len(tok.vocab), hidden),
		pos:       l.get("embeddings.position_embeddings.weight", 512, hidden),
		tokenType: l.get("embeddings.token_type_embeddings.weight", 2, hidden)[:hidden],
		embNorm:   l.norm("embeddings.LayerNorm"),
	}
	for i := range m.layers {
		p := fmt.Sprintf("encoder.layer.%d.", i)
		m.layers[i] = layer{
			query:        l.linear(p+"attention.self.query", hidden, hidden),
			key:          l.linear(p+"attention.self.key", hidden, hidden),
			value:        l.linear(p+"attention.self.value", hidden, hidden),
			attnOut:      l.linear(p+"attention.output.dense", hidden, hidden),
			attnNorm:     l.norm(p + "attention.output.LayerNorm"),
			intermediate: l.linear(p+"intermediate.dense", hidden, ffn),
			output:       l.linear(p+"output.dense", ffn, hidden),
			outNorm:      l.norm(p + "output.LayerNorm"),
		}
	}
	if l.err != nil {
		return nil, l.err
	}
	return m, nil
}

// loader fetches tensors by name, checking shapes and remembering the first
// error so Load can stay linear.
type loader struct {
	ts  map[string]tensor
	err error
}

func (l *loader) get(name string, shape ...int) []float32 {
	t, ok := l.ts[name]
	if !ok {
		if l.err == nil {
			l.err = fmt.Errorf("model: missing tensor %s", name)
		}
		return make([]float32, product(shape))
	}
	if fmt.Sprint(t.shape) != fmt.Sprint(shape) {
		if l.err == nil {
			l.err = fmt.Errorf("model: tensor %s has shape %v, want %v", name, t.shape, shape)
		}
		return make([]float32, product(shape))
	}
	return t.data
}

func (l *loader) linear(prefix string, in, out int) linear {
	return linear{w: l.get(prefix+".weight", out, in), b: l.get(prefix+".bias", out), in: in, out: out}
}

func (l *loader) norm(prefix string) layerNorm {
	return layerNorm{gamma: l.get(prefix+".weight", hidden), beta: l.get(prefix+".bias", hidden)}
}

func product(shape []int) int {
	p := 1
	for _, d := range shape {
		p *= d
	}
	return p
}

// Embed returns the L2-normalized embedding of text.
func (m *Model) Embed(text string) []float32 {
	ids := m.tok.Encode(text)
	n := len(ids)

	x := make([]float32, n*hidden)
	for i, id := range ids {
		row := x[i*hidden : (i+1)*hidden]
		w := m.word[int(id)*hidden:]
		p := m.pos[i*hidden:]
		for j := range row {
			row[j] = w[j] + p[j] + m.tokenType[j]
		}
	}
	m.embNorm.apply(x, n)

	q := make([]float32, n*hidden)
	k := make([]float32, n*hidden)
	v := make([]float32, n*hidden)
	ctx := make([]float32, n*hidden)
	tmp := make([]float32, n*hidden)
	inter := make([]float32, n*ffn)
	for i := range m.layers {
		l := &m.layers[i]
		l.query.apply(x, n, q)
		l.key.apply(x, n, k)
		l.value.apply(x, n, v)
		attention(q, k, v, n, ctx)
		l.attnOut.apply(ctx, n, tmp)
		addInPlace(x, tmp)
		l.attnNorm.apply(x, n)

		l.intermediate.apply(x, n, inter)
		gelu(inter)
		l.output.apply(inter, n, tmp)
		addInPlace(x, tmp)
		l.outNorm.apply(x, n)
	}
	return meanPoolNormalize(x, n)
}

// apply computes y[n][out] = x[n][in]·Wᵀ + b.
//
// Each weight row is read once and reused for all n tokens, four at a time
// (dot4). It is deliberately simple and single-threaded: on the dev machine
// (i7-1255U, 2 performance + 8 efficiency cores) cache tiling, an 8-accumulator
// kernel, GOAMD64=v3 and splitting one multiply across cores were each measured
// A/B and none helped (several hurt). See ROADMAP Phase 2 measurements.
func (l linear) apply(x []float32, n int, y []float32) {
	in, out := l.in, l.out
	for o := 0; o < out; o++ {
		w := l.w[o*in : (o+1)*in]
		b := l.b[o]
		i := 0
		for ; i+4 <= n; i += 4 { // four tokens share each load of w
			s0, s1, s2, s3 := dot4(x[i*in:(i+4)*in], w)
			y[i*out+o] = b + s0
			y[(i+1)*out+o] = b + s1
			y[(i+2)*out+o] = b + s2
			y[(i+3)*out+o] = b + s3
		}
		for ; i < n; i++ {
			y[i*out+o] = b + dot(x[i*in:(i+1)*in], w)
		}
	}
}

// dot4 returns the dot products of w with the four consecutive rows of x
// (x holds 4*len(w) values). Four accumulators is the sweet spot: an 8-way
// variant (2 weight rows x 4 tokens) measured 2.8x slower in Go.
func dot4(x, w []float32) (s0, s1, s2, s3 float32) {
	d := len(w)
	x0, x1, x2, x3 := x[:d], x[d:2*d], x[2*d:3*d], x[3*d:4*d]
	for j, wj := range w {
		s0 += x0[j] * wj
		s1 += x1[j] * wj
		s2 += x2[j] * wj
		s3 += x3[j] * wj
	}
	return
}

// dot is an unrolled float32 dot product; a and b have equal length.
func dot(a, b []float32) float32 {
	b = b[:len(a)]
	var s0, s1, s2, s3 float32
	i := 0
	for ; i+4 <= len(a); i += 4 {
		s0 += a[i] * b[i]
		s1 += a[i+1] * b[i+1]
		s2 += a[i+2] * b[i+2]
		s3 += a[i+3] * b[i+3]
	}
	for ; i < len(a); i++ {
		s0 += a[i] * b[i]
	}
	return (s0 + s1) + (s2 + s3)
}

// attention is multi-head scaled dot-product self-attention over a single
// unpadded sequence, so no attention mask is needed.
func attention(q, k, v []float32, n int, out []float32) {
	scale := float32(1 / math.Sqrt(headDim))
	scores := make([]float32, n)
	for h := 0; h < heads; h++ {
		off := h * headDim
		for i := 0; i < n; i++ {
			qi := q[i*hidden+off : i*hidden+off+headDim]
			maxScore := float32(math.Inf(-1))
			for j := 0; j < n; j++ {
				scores[j] = dot(qi, k[j*hidden+off:j*hidden+off+headDim]) * scale
				maxScore = max(maxScore, scores[j])
			}
			var sum float32
			for j := range scores {
				scores[j] = float32(math.Exp(float64(scores[j] - maxScore)))
				sum += scores[j]
			}
			oi := out[i*hidden+off : i*hidden+off+headDim]
			clear(oi)
			for j := 0; j < n; j++ {
				p := scores[j] / sum
				vj := v[j*hidden+off : j*hidden+off+headDim]
				for d := range oi {
					oi[d] += p * vj[d]
				}
			}
		}
	}
}

// apply normalizes each of the n rows of x in place.
func (nm layerNorm) apply(x []float32, n int) {
	for i := 0; i < n; i++ {
		row := x[i*hidden : (i+1)*hidden]
		var mean, variance float64
		for _, v := range row {
			mean += float64(v)
		}
		mean /= hidden
		for _, v := range row {
			d := float64(v) - mean
			variance += d * d
		}
		variance /= hidden
		inv := 1 / math.Sqrt(variance+lnEps)
		for j, v := range row {
			row[j] = float32((float64(v)-mean)*inv)*nm.gamma[j] + nm.beta[j]
		}
	}
}

// gelu applies the exact (erf-based) GELU used by BERT's "gelu" activation.
func gelu(x []float32) {
	for i, v := range x {
		x[i] = float32(0.5 * float64(v) * (1 + math.Erf(float64(v)/math.Sqrt2)))
	}
}

func addInPlace(dst, src []float32) {
	for i := range dst {
		dst[i] += src[i]
	}
}

// meanPoolNormalize averages the n token vectors (all are real tokens: there
// is no padding) and scales the result to unit length.
func meanPoolNormalize(x []float32, n int) []float32 {
	out := make([]float32, hidden)
	for i := 0; i < n; i++ {
		for j, v := range x[i*hidden : (i+1)*hidden] {
			out[j] += v
		}
	}
	var sq float64
	for j := range out {
		out[j] /= float32(n)
		sq += float64(out[j]) * float64(out[j])
	}
	inv := float32(1 / math.Sqrt(sq))
	for j := range out {
		out[j] *= inv
	}
	return out
}
