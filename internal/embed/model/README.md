# Embedded model files

These files are compiled into the `loxx` binary by `internal/embed/assets.go`.

The weights file is **not stored in git**. Run `make model` (build, test and
lint do it automatically): it downloads the file from the
`model-minilm-l6-v2-f16` GitHub Release and refuses it unless the SHA-256
matches. Because of this, `go install github.com/AniketR10/loxx/cmd/loxx@latest`
does not work; install a release binary instead, or clone and run `make build`.

| File | What it is |
|---|---|
| `minilm-l6-v2.f16.safetensors` | Weights of [sentence-transformers/all-MiniLM-L6-v2](https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2), converted from f32 to f16 |
| `vocab.txt` | The model's WordPiece vocabulary, unchanged |
| `LICENSE` | The Apache License 2.0, which covers the model files |

## Source

- Model: `sentence-transformers/all-MiniLM-L6-v2`
- Revision: `1110a243fdf4706b3f48f1d95db1a4f5529b4d41`
- Original `model.safetensors` sha256: `53aa51172d142c89d9012cce15ae4d6cc0ca6895895114379cacb4fab128d9db`
- Original `vocab.txt` sha256: `07eced375cec144d27c900241f3e339478dec958f92fddbc551f295c992038a3`

## Modifications

Produced by `tools/model/convert.py`:

- Every weight tensor was cast from float32 to float16 (max absolute rounding
  error 0.00098).
- Two tensors that `loxx` does not use were removed: `embeddings.position_ids`
  (an index buffer) and `pooler.*` (sentence-transformers mean-pools instead).

Resulting sha256:

- `minilm-l6-v2.f16.safetensors`: `aa3d97aea538b3247506fd426683a526d23ed345ac2c20ea3172296f57ea272b`
- `vocab.txt`: unchanged

## License

The model files are licensed under the Apache License 2.0 (see `LICENSE`).
The rest of `loxx` is MIT licensed.
