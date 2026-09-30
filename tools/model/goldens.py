#!/usr/bin/env python3
"""Dev-only: generate golden token ids and embeddings for loxx's Go model.

The reference is sentence-transformers itself (full f32 weights), so the Go
tests measure both implementation errors and the f16 conversion loss.

Usage: goldens.py SNAPSHOT_DIR OUTPUT_JSON
Needs: sentence-transformers.
"""

import json
import sys
from pathlib import Path

from sentence_transformers import SentenceTransformer

# Generic commands and edge cases, never real user history: this file is
# committed.
CASES = [
    # everyday commands
    "git status",
    "git push --force-with-lease origin main",
    "git log --oneline --graph --decorate --all",
    "docker run -d -v pgdata:/var/lib/postgresql/data -e POSTGRES_PASSWORD=<REDACTED:secret-env> postgres:16",
    "docker-compose -f docker-compose.prod.yml up -d --build",
    "kubectl port-forward svc/api 8080:80 -n prod",
    "kubectl get pods -A -o wide | grep CrashLoopBackOff",
    "find . -name '*.go' -exec gofmt -l {} +",
    "tar -czvf backup-2026-09-27.tar.gz ~/Documents",
    "ssh -i ~/.ssh/id_ed25519 deploy@10.0.0.5",
    "curl -fsSL https://example.com/install.sh | sh",
    "sudo systemctl restart nginx",
    "npm run build && npm test",
    "python3 -m venv .venv && source .venv/bin/activate",
    "for i in $(seq 1 10); do echo $i; done",
    "awk -F: '{print $1}' /etc/passwd | sort | uniq -c",
    "sed -i 's/foo/bar/g' config.yaml",
    'export PATH="$HOME/.local/bin:$PATH"',
    "rsync -avz --delete ./dist/ user@host:/var/www/",
    "ffmpeg -i input.mp4 -vf scale=1280:720 output.mp4",
    "go test ./... -run TestFoo -v",
    "cargo build --release --target x86_64-unknown-linux-musl",
    "jq '.data[] | {id, name}' response.json",
    "pnpm dev -- --help",
    "nvm use 20",
    # natural-language queries
    "what was the docker command for the postgres volume",
    "undo the last commit but keep my changes",
    "restart the web server",
    "list kubernetes pods in production",
    "How do I find large files in my home directory?",
    # edge cases: empty and whitespace
    "",
    "   ",
    "tab\tseparated\nand newline",
    # case, accents and unicode
    "UPPERCASE COMMAND WITH MIXED Case",
    "ÉCOLE Café naïve résumé",
    "İstanbul ΟΔΟΣ Straße",
    "日本語のコマンド 中文命令 한국어",
    "emoji 🚀 deploy ✅ done",
    "control\x00chars\x07here",
    "zero\u200bwidth\u00a0space",
    # punctuation-heavy
    "--flag=value --other='quoted value' -xvf",
    "C:\\Windows\\System32\\drivers\\etc\\hosts",
    "$(date +%s) ${HOME} `whoami`",
    "a|b&c;d>e<f",
    # WordPiece: unknown and very long words
    "supercalifragilisticexpialidocious pneumonoultramicroscopic",
    "a" * 120,
    "xqzjvkw qqqq zzzz",
    # truncation past 256 tokens
    "echo " + " ".join(f"argument{i}" for i in range(300)),
]


def main():
    src, out = Path(sys.argv[1]), Path(sys.argv[2])
    model = SentenceTransformer(str(src), device="cpu")
    tok = model.tokenizer
    embeddings = model.encode(CASES, normalize_embeddings=True, batch_size=16)
    cases = []
    for text, vec in zip(CASES, embeddings):
        ids = tok(text, truncation=True, max_length=model.max_seq_length)["input_ids"]
        cases.append({"text": text, "ids": ids, "embedding": [float(f"{x:.7g}") for x in vec]})
    golden = {
        "model": "sentence-transformers/all-MiniLM-L6-v2",
        "revision": (src / "REVISION").read_text().strip(),
        "max_seq_length": model.max_seq_length,
        "cases": cases,
    }
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(golden, ensure_ascii=False, indent=0) + "\n")
    print(f"wrote {len(cases)} cases to {out} (max_seq_length {model.max_seq_length})")


if __name__ == "__main__":
    main()
