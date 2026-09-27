#!/usr/bin/env python3
"""Dev-only: convert all-MiniLM-L6-v2 weights into the f16 file loc embeds.

Reads a Hugging Face snapshot directory and writes, into the output directory:
  minilm-l6-v2.f16.safetensors  every tensor loc uses, cast f32 -> f16
  vocab.txt                     the WordPiece vocabulary, copied unchanged

Dropped: embeddings.position_ids (an index buffer) and pooler.* (unused:
sentence-transformers mean-pools instead).

Usage: convert.py SNAPSHOT_DIR OUTPUT_DIR
Needs: numpy, safetensors.
"""

import hashlib
import shutil
import sys
from pathlib import Path

import numpy as np
from safetensors.numpy import load_file, save_file

F16_MAX = 65504.0


def main():
    src, out = Path(sys.argv[1]), Path(sys.argv[2])
    out.mkdir(parents=True, exist_ok=True)

    weights = load_file(src / "model.safetensors")
    kept = {}
    worst = 0.0
    for name, w in sorted(weights.items()):
        if name == "embeddings.position_ids" or name.startswith("pooler."):
            continue
        if w.dtype != np.float32:
            raise SystemExit(f"unexpected dtype {w.dtype} for {name}")
        if np.abs(w).max() >= F16_MAX:
            raise SystemExit(f"{name} overflows f16")
        h = w.astype(np.float16)
        worst = max(worst, float(np.abs(h.astype(np.float32) - w).max()))
        kept[name] = h

    sha = hashlib.sha256((src / "model.safetensors").read_bytes()).hexdigest()
    revision = (src / "REVISION").read_text().strip() if (src / "REVISION").exists() else "unknown"
    metadata = {
        "source": "sentence-transformers/all-MiniLM-L6-v2",
        "revision": revision,
        "source_sha256": sha,
        "license": "Apache-2.0",
        "dtype": "float16",
    }
    dst = out / "minilm-l6-v2.f16.safetensors"
    save_file(kept, dst, metadata=metadata)
    shutil.copyfile(src / "vocab.txt", out / "vocab.txt")

    print(f"kept {len(kept)} of {len(weights)} tensors")
    print(f"max abs f16 rounding error: {worst:.3g}")
    print(f"wrote {dst} ({dst.stat().st_size / 1e6:.1f} MB)")


if __name__ == "__main__":
    main()
