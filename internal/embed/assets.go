// Package embed is a pure-Go all-MiniLM-L6-v2 with its weights compiled in.
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
