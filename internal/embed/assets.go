// Package embed turns shell commands into 384-dimensional vectors with
// sentence-transformers/all-MiniLM-L6-v2, implemented in pure Go. The model
// weights (f16) and vocabulary are compiled into the binary.
package embed

import _ "embed"

// The model files are produced by tools/model/convert.py. See
// model/README.md for their source, revision and license.
var (
	//go:embed model/minilm-l6-v2.f16.safetensors
	weightsF16 []byte

	//go:embed model/vocab.txt
	vocabTxt []byte
)
