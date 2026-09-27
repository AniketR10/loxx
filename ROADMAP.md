# loc — Roadmap & Source of Truth

> **Read this file at the start of every work session.** It is the single source of
> truth for scope, decisions, and progress. If something is not written here, it is
> not decided — ask the user, then record the answer here.

---

## 0. Rules for working on this project

1. **Work only on the current phase.** Do not start tasks from a later phase, even "quick" ones.
   The current phase is marked in [§6 Progress](#6-progress-log).
2. **Do not assume.** Every choice is either:
   - **LOCKED**: the user decided it. Follow it.
   - **PROPOSED**: a suggestion. Confirm it with the user before the phase that needs it starts.
   - **OPEN**: unknown. Ask before acting.

   Never quietly turn a PROPOSED item into LOCKED.
3. **Measure, don't claim.** Every performance or quality number in this file is a *target*,
   not a fact, until a benchmark or eval result is recorded next to it.
4. **A phase is done only when all of its exit criteria pass.** Tick the boxes, and record the
   date and evidence in the progress log.
5. **Scope changes go through this file.** New idea → add it to [§7 Backlog](#7-backlog-unscheduled).
   Don't build it.
6. **The core principles in §2 are non-negotiable.** If a task conflicts with one, stop and ask.

---

## 1. What we are building

**loc** is an open-source (MIT), local-first shell-history tool for Linux:
- It records every command you run in bash/zsh, along with its directory, exit code, time and duration.
- It removes secrets before anything is stored.
- It embeds commands with a small model compiled into the binary.
- Ctrl-R opens a TUI with hybrid keyword + semantic search. Enter puts the chosen command
  into your prompt (it does not run it).

**User promise:** install it, and that's all. No Ollama, no config, no manual setup,
no network calls.

### End-user flow (target)
1. `curl -sSf <install-url> | sh`
   - Installs the binary.
   - Detects bash/zsh.
   - Adds one marked hook line to the rc file.
2. The user opens a new terminal. Existing history is imported, scrubbed and embedded in
   the background. New commands are recorded silently.
3. They press Ctrl-R, type "docker command for the postgres volume", pick a result, and press Enter.
   The command is now in their prompt, editable and not executed.
4. `loc uninstall` removes the hook lines and, if they confirm, the data.

---

## 2. Core principles (non-negotiable)

| # | Principle | How it's enforced |
|---|---|---|
| P1 | **Never slow the prompt.** | The hook runs `loc record` in the background. Record latency is benchmarked in CI. |
| P2 | **Never store an unscrubbed secret.** | All writes go through the scrubber. There is no code path that writes raw text. A test corpus runs in CI. |
| P3 | **No network by default.** | The embedding model is compiled in. No telemetry. Any network feature is opt-in and documented. |
| P4 | **Single static binary.** | `CGO_ENABLED=0`, checked in CI. No runtime dependencies. |
| P5 | **Never execute on the user's behalf.** | Selecting a result only fills the prompt buffer. |
| P6 | **Fully reversible.** | rc-file edits are wrapped in markers and idempotent. `loc uninstall` removes everything. |
| P7 | **Search still works without embeddings.** | If the model or embeddings aren't ready, fall back to FTS keyword search. |
| P8 | **Fully open source.** | MIT. Every dependency and the model weights must have an OSI-compatible license, checked before adding. |

---

## 3. Decisions

### LOCKED (decided by the user, 2026-09-26)
| Topic | Decision |
|---|---|
| Language | **Go** (pure Go, no CGO) |
| Project / binary name | **loc** |
| Go module path | **github.com/AniketR10/loc** |
| License | **MIT** |
| v1 platform | **Linux only** (amd64 + arm64: see PROPOSED) |
| v1 shells | **Bash + Zsh** |
| Default keybinding | **Take over Ctrl-R**, with a one-line config opt-out |
| Setup burden | **Install only.** No manual steps (except the package-manager caveat in Phase 6) |
| Embedding runtime | **In-process, model compiled into the binary.** No Ollama required. |
| Embedding model | **`sentence-transformers/all-MiniLM-L6-v2`**, pinned to HF revision `1110a243fdf4706b3f48f1d95db1a4f5529b4d41` (user decision 2026-09-27 after the bake-off; see `docs/decisions/0001-embedding-model.md`). Verified: **Apache-2.0**; BERT with **6 layers**, hidden 384, 12 heads, FFN 1536, GELU, LayerNorm eps 1e-12; uncased WordPiece, vocab 30,522 (identical to L12); BertNormalizer (clean_text, chinese chars, lowercase, strip accents) + BertPreTokenizer + WordPiece (`##`, max 100 chars/word); **max_seq_length 256**; mean pooling → L2 normalize → 384-dim. Weights 90.9 MB f32 (~45 MB f16). model.safetensors sha256 `53aa5117…d9db`. **Fallback** if pure-Go speed fails the gate: potion-base-8M. |
| Embedding speed | **Accepted as-is** (~65 ms per typical query on battery, ~110 ms model load). **No AVX2 assembly** unless real-world use shows it's too slow (user decision, 2026-09-27) |
| Model weights distribution | **Not in git.** `make model` downloads `minilm-l6-v2.f16.safetensors` from the `model-minilm-l6-v2-f16` GitHub Release on this repo and verifies SHA-256 `aa3d97ae…272b` (build/test/lint depend on it). vocab.txt, LICENSE and README stay in git. User chose build-time download + SHA-256 (2026-09-27); Claude recommended GitHub Release over Hugging Face because users already download binaries from GitHub Releases (no new point of failure). End users are unaffected: release binaries embed the model. **Known cost: `go install …@latest` doesn't work** (document in the README). CI fetches with `gh` + token while the repo is private. **Rejected alternative (2026-09-27):** having `loc` download the model on first run (to `~/.loc/models/`). It gives a smaller binary (~7 MB vs ~52 MB), but breaks P3 (network use), the offline/air-gapped story and the XDG data location. |
| `executions.source` values | **`'live'`, `'import'`, `'manual'`**. `'manual'` is kept for `loc add` (user, 2026-09-27; was O7) |
| `commands.embedded_at` | **Dropped** in migration v3; the `embeddings` table is the single source of truth (user: "remove the unnecessary things", 2026-09-27; was O8) |
| Vector search | **Brute-force cosine in Go** over stored float32 vectors, no sqlite-vec (user, 2026-09-27) |
| Search ranking | **Keyword BM25 + semantic, fused (RRF), then recency/frequency/cwd boosts**, with every weight **tuned against the eval set, not assumed** (user, 2026-09-27) |
| Search latency target | **Keyword results < 50 ms at 100k; semantic results ready < 300 ms at 10k** (user, 2026-09-27; replaced "< 100 ms end-to-end at 100k" after measurements showed model load + query embedding alone take ~133 ms on battery). SQLite stays the only store; revisit if real histories exceed ~30k commands. |
| Language policy | **Dev-only tooling may use any language** (Python for model conversion, golden generation, bake-offs). **Code that ships to users stays Go**, because of P1 (fast startup on every prompt), P3/P4 (single static binary, zero setup) and the install-only promise, not out of language preference. (User said the focus is performance and correctness, not language, 2026-09-27) |
| Inference implementation | **Hand-written pure Go** (user decision, 2026-09-27; hugot rejected). If a transformer wins: BERT forward pass + WordPiece. If model2vec wins: WordPiece + lookup + mean pooling. Weights loaded from `go:embed`. Only extra dependency: `golang.org/x/text` v0.41.0 (BSD-3, needed for Unicode NFD in the tokenizer; v0.42+ requires Go 1.26). |
| Weight precision | **f16** in the binary, converted to f32 at load (user decision, 2026-09-27; carried over from L12 to L6) |
| Dev-only downloads | **Approved** (2026-09-27): L12 model files, a Python venv with sentence-transformers + CPU torch, Ollama `nomic-embed-text`, plus for the bake-off: the `model2vec` package, potion-base-8M and all-MiniLM-L6-v2. Never shipped to users. |
| Eval queries | **Claude drafts them from `~/.bash_history`, and the user edits them** (2026-09-27) |
| Output (stdout) capture via PTY | **Not in v1.** Backlog only. |
| Database | **SQLite** (not DuckDB) |
| CLI library | **stdlib `flag`** + a small hand-written subcommand dispatcher. No cobra: keeps `loc record` lean and the dependency count low. (User asked for a recommendation and accepted it, 2026-09-26) |
| Directory layout | As listed in Phase 0 (approved 2026-09-26). Directories are created when first needed, not up front. |
| GitHub repo | **Private** for now: `github.com/AniketR10/loc` |
| LICENSE file | **The user adds it themselves.** Claude must not create or edit `LICENSE`. |
| Go version | `go 1.25.0` minimum, `toolchain go1.25.12` (the locally installed version) |
| SQLite driver | **modernc.org/sqlite** v1.59.0 (pure Go, FTS5), approved 2026-09-27 |
| Data location | **`$XDG_DATA_HOME/loc/history.db`** (default `~/.local/share/loc/`), overridable with **`LOC_DB_PATH`**, approved 2026-09-27 |
| Embeddings table | **Created in Phase 2**, not Phase 1 (only create tables when they're used), approved 2026-09-27 |
| Linting | `go vet` + `staticcheck` **2026.1**, pinned. 2026.2+ requires Go 1.26 and would silently download a second toolchain. |
| Build tool | `Makefile` (`build`, `test`, `lint`, `bench`, `check-static`, `clean`) |
| CI | GitHub Actions: `actions/checkout@v7`, `actions/setup-go@v7` (latest releases as of 2026-09-26) |

### PROPOSED (confirm before the phase that uses it)
| Topic | Proposal | Needed by |
|---|---|---|
| TUI | Bubbletea + Bubbles + Lipgloss | Phase 5 |
| Config format / location | TOML at `$XDG_CONFIG_HOME/loc/config.toml` | Phase 6 |
| Bash hook mechanism | Vendored `bash-preexec` (verify license) vs `PS0` + `PROMPT_COMMAND` | Phase 4 |
| Linux architectures | amd64 + arm64 | Phase 7 |
| Release tooling | GoReleaser + GitHub Actions | Phase 7 |
| Distro packages | AUR, Fedora COPR, others | Phase 7 |

### OPEN (unknown: must ask)
| # | Question | Blocks |
|---|---|---|
| O1 | The name `loc` is also used by an existing lines-of-code counter (`cgag/loc`). Keep `loc` anyway? | Phase 7 (packaging names) |
| O2 | Should imported bash history without timestamps (no `HISTTIMEFORMAT`) get the import time, or no time at all? | Phase 4 |
| O3 | Install URL / hosting for `install.sh` (GitHub raw? GitHub Pages? a custom domain?) | Phase 6 |
| O4 | Minimum supported bash and zsh versions | Phase 4 |
| O5 | Should `loc` record commands run inside `ssh` sessions on remote hosts? (Probably out of scope for v1, but unconfirmed) | Phase 4 |
| O6 | Go 1.26 is out (1.26.8 seen 2026-09-26), but 1.25.12 is installed locally. Move the project to Go 1.26? | Any time (low priority) |

---

## 4. Verified environment facts (dev machine, checked 2026-09-26)
- OS: Fedora 43, Linux 7.1, x86_64. The display server has not been checked yet (it only matters for a clipboard fallback, which is not planned).
- Go **1.25.12** installed.
- Default shell: **bash 5.3.0**.
- **zsh is NOT installed.** Install it (`sudo dnf install zsh`) before zsh work in Phase 4.
- Ollama **0.16.1** installed. For dev only: an optional quality baseline in the Phase 2 eval. Users never need it.
- git 2.55. `gh` is logged in as `AniketR10`.
- The project directory was empty. Not yet a git repo.

---

## 5. Phases

Each phase lists its **Goal**, **Tasks**, **Exit criteria** and **Out of scope**.

---

### Phase 0: Foundations
**Goal:** An empty but real project that builds a static binary and runs CI.

**Tasks**
- [x] `git init`, `.gitignore`
- [x] `LICENSE`: added by the user (MIT, © 2026 Aniket Rawat, commit 8d30b1b). Do not edit it.
- [x] `go mod init github.com/AniketR10/loc`, and set the minimum Go version in `go.mod`
- [x] Directory layout (approved):
  ```
  cmd/loc/            main entry, CLI wiring
  internal/store/     SQLite schema, migrations, queries
  internal/scrub/     secret scrubber
  internal/embed/     model2vec tokenizer + embedding
  internal/search/    hybrid ranking
  internal/capture/   record fast path, session ids
  internal/importer/  bash/zsh history import
  internal/tui/       Bubbletea UI
  internal/setup/     rc-file install/uninstall
  shell/              loc.bash, loc.zsh (go:embed)
  testdata/           secrets corpus, eval queries, golden vectors
  ```
- [x] CLI skeleton: `loc version`, `loc help` (stdlib `flag`: see §3)
- [x] Makefile: `build`, `test`, `lint`, `bench`, `check-static`, `clean`
- [x] GitHub repo created (private: approved by the user)
- [x] CI: build with `CGO_ENABLED=0`, `go test ./...`, lint, and a check that the binary is static

**Exit criteria**
- [x] `CGO_ENABLED=0 go build ./cmd/loc` produces a static binary (checked with `file`/`ldd`)
- [x] CI passes on the first commit

**Out of scope:** any functionality.

---

### Phase 1: Storage + Scrubber (the data foundation)
**Goal:** Commands can be safely stored and found by keyword. No secret ever reaches disk.

**Tasks: Scrubber (build first; everything else depends on it)**
- [x] Test corpus in `testdata/secrets/`: `positive.jsonl` (43 commands, each listing its secrets) and `negative.jsonl` (51 commands that must stay unchanged). All secrets are fake or public documentation examples.
- [x] Layer 1: skip commands that start with a space (`scrub.Ignored`)
- [x] Layer 2: known token patterns: AWS, GitHub, GitLab, Slack, Stripe, Google, OpenAI, Anthropic, Hugging Face, npm, PyPI, DigitalOcean, SendGrid, JWT, private-key blocks. **Written from scratch; no third-party ruleset copied**, so there's no license question.
- [x] Layer 3: structural rules: `KEY=value` (secret-ish key names, skipping `*_FILE`/`*_PATH`/`PWD`/…), `--password`/`--token`/… flags, URL `user:pass@`, `Authorization`/API-key/cookie headers, JSON `"password": "…"`, query params, `mysql -p…`, `sshpass -p`, `redis-cli -a`, `curl -u user:pass`, openssl `pass:…`, `aws configure set aws_secret_access_key …`
- [x] Layer 4: high-entropy fallback: ≥20 chars, upper + lower + digit, Shannon entropy ≥ 3.5, and class-change factor ≥ 0.45. Hex-only strings (SHAs, digests, UUIDs) are skipped.
- [x] Replacement format: `<REDACTED:kind>`
- [x] Redaction count returned (`Command.Redactions()`) and stored in `commands.redacted`
- [x] Fuzz test (`FuzzScrub`): no panics, valid UTF-8 is preserved, scrubbing is idempotent. **It found a real leak**: in `mysql -pA -pB` only the first password was redacted. Fixed by applying each rule until it finds nothing new. The regression input is kept in `internal/scrub/testdata/fuzz/`. A later 90 s run (~213k inputs) was clean. `make fuzz` runs it (`FUZZTIME=60s` by default).
- [x] P2 is enforced by the type system: `store.Execution` takes a `scrub.Command`, which can only be built by `scrub.Scrub`.

**Tasks: Storage**
- [x] Schema v1 (approved with changes, 2026-09-27):
  - `commands(id, text UNIQUE, first_seen, last_seen, run_count, redacted, embedded_at NULL)`: times are unix milliseconds
  - `executions(id, command_id, cwd NULL, git_root NULL, exit_code NULL, duration_ms NULL, started_at NULL, session_id, hostname, source['live'|'import'|'manual'])`. NULL means unknown (e.g. imports). The `'manual'` source value was added for `loc add` (**confirm: O7**).
  - `commands_fts`: FTS5 (default `unicode61` tokenizer), kept in sync by triggers
  - `meta(key, value)`: holds `schema_version`
  - `embeddings`: **deferred to Phase 2** (user decision)
- [x] Migrations framework: versioned and forward-only. Checks the version without a lock first, then migrates under `BEGIN IMMEDIATE`. Refuses to open a database from a newer loc.
- [x] WAL, `busy_timeout=10000`, `synchronous=NORMAL`, `foreign_keys=ON`, and `_txlock=immediate` (all writes take the write lock up front). DB file `0600` (the -wal and -shm files inherit it), newly created data dir `0700`. Existing dirs are never chmod-ed.
- [x] Concurrency test: 20 **separate processes** race to create, migrate and write one fresh DB
- [x] CLI: `loc add [--cwd DIR] [--exit CODE] "<cmd>"`: prints a notice on stderr when it redacts something
- [x] CLI: `loc search [--limit N] <query>`: words are ANDed, each word also matches as a prefix, ranked by BM25. Default limit 5 (from the original brief). Exits 1 on no matches (like grep).
- [x] Data path: `$LOC_DB_PATH` → `$XDG_DATA_HOME/loc/history.db` → `~/.local/share/loc/history.db`

**Exit criteria**
- [x] All positive corpus secrets are redacted and all negative items are left intact (100%): `TestPositiveCorpus`, `TestNegativeCorpus`
- [x] A grep of the DB files (db + wal + shm, while open and after close) for every corpus secret finds nothing, with a positive control proving the check reads stored data: `TestNoSecretsOnDisk`. Also confirmed by a manual smoke test of the real binary.
- [x] 20 concurrent writers × 500 inserts: 0 errors, 10,000/10,000 rows, correct run counts, in about 2.1 s: `TestConcurrentWriters`
- [x] `loc add` then `loc search` round-trips correctly: `TestAddAndSearch`, plus a manual smoke test

**Measurements (2026-09-27, dev machine)**
- `scrub.Scrub`: about 6.5 µs for `git status`, 135–145 µs for long commands with secrets (0–7 allocations). This will matter for the Phase 4 `loc record` latency budget.
- Binary: 1.5 MB → **6.6 MB** with SQLite. Linked modules: modernc.org/{sqlite,libc,mathutil,memory}, golang.org/x/sys, google/uuid, remyoudompheng/bigfft (BSD-3), dustin/go-humanize (MIT). The MPL-2.0 module in the graph is **not** linked.

**Known limitations (accepted for now, revisit if they bite)**
- Hex-only secrets without a telling key name or flag (e.g. a bare 64-hex API key as a positional argument) are not caught. This is the trade-off for never redacting git SHAs and digests.
- Only the command text is scrubbed. `cwd` and hostname are stored as-is.
- The corpus contains fake secrets in token formats. If GitHub secret-scanning push protection is on for the repo, a push could be blocked; the fix is to mark them as test data.

**Out of scope:** embeddings, hooks, TUI.

---

### Phase 2: Embedding engine (the riskiest technical piece: prove it early)
**Goal:** all-MiniLM-L6-v2 running in pure Go (no CGO), compiled into the binary, with measured speed and quality.

**Decisions:** see §3 (inference: hand-written; precision: f16; dev downloads approved; eval drafted by Claude, edited by the user).

**Tasks**
- [x] **Model bake-off:** potion-base-8M vs MiniLM-L6 vs MiniLM-L12 vs nomic-embed-text on 40 drafted queries over 369 real commands. **Winner: all-MiniLM-L6-v2** (top-1 0.55, top-5 0.80, MRR 0.65). Full results and learnings: `docs/decisions/0001-embedding-model.md`. Script: `tools/bakeoff/bakeoff.py`.
- [x] Verify the winner's (L6) license, file size, dimensions and tokenizer type. Recorded in §3.
- [x] WordPiece tokenizer in Go (BERT basic tokenizer: lowercase, strip accents, split punctuation and CJK characters; then greedy longest-match WordPiece; add `[CLS]`/`[SEP]`; truncate to 256 tokens, L6's max_seq_length). Done: `internal/embed/tokenizer.go`.
- [x] **Tokenizer golden test:** token ids must match the Python reference exactly for the golden strings, including shell-heavy ones (`docker-compose`, `--flag=value`, paths, unicode)
- [x] BERT forward pass in Go (`internal/embed/model.go`): embeddings + position + token-type → LayerNorm → 6 × (self-attention + FFN with GELU + residual + LayerNorm) → mean pooling over the attention mask → L2 normalize
- [x] **Embedding golden test:** 48 strings (`testdata/embed/golden.json`, from `tools/model/goldens.py`). Bar: cosine ≥ 0.9999 (f16) and ≥ 0.99999 (f32, opt-in via `LOC_EMBED_F32`).
- [x] Weight conversion script `tools/model/convert.py`: f32 → f16 safetensors (101 of 104 tensors; position_ids and pooler dropped), max rounding error 0.00098, **45.1 MB**. Binary-size delta: measured once `loc` imports the embed package (`loc embed`).
- [x] Ship the model's Apache-2.0 license text alongside the weights (`internal/embed/model/LICENSE` + `README.md` with source revision, checksums and modifications). Mention it in the top-level README when that is written (Phase 7).
- [x] `make model`: download + SHA-256 verification (tested: a bad hash, a 404, and wrong content are all rejected with no partial file left). CI caches the file.
- [x] Created the `model-minilm-l6-v2-f16` GitHub Release (prerelease, `--latest=false`) with the weights + LICENSE, 2026-09-27, with the user's OK. The local SHA-256 was verified before upload. A read-back check was declined by the user; CI's `make model` verifies the SHA-256 on first download, and the prerelease/not-latest flags must be confirmed before Phase 6's `install.sh` relies on "latest".
- [x] Migration v2: `embeddings(command_id PK → commands ON DELETE CASCADE, model_id, dims, vector BLOB)`, tested including an upgrade of a v1 database with data. `meta.active_model` was not added (the per-row `model_id` covers model changes; accepted 2026-09-27). `commands.embedded_at` was dropped in v3.
- [x] `loc embed --pending [--workers N]`: batches of 64, most recently used first, saved per batch (Ctrl-C loses at most one batch), `flock` lockfile `<db>.embed.lock` (a second run exits 0 with a notice), parallel **across commands** (default workers = NumCPU/2 = 6 here). It does **not** lower its own CPU priority: Linux `setpriority` only affects one thread of a Go process, so Phase 4's hook should launch it with `nice`.
- [x] `model_id` stored per vector (`embed.ModelID` = `all-MiniLM-L6-v2@1110a243/f16`). A different id makes every command pending again, and vectors from different models are never mixed (tested).
- [x] Benchmarks: see the measurements below (load, per-token latency, real 369-command run with 1/6/12 workers, peak RAM)
- [x] **Eval harness:** `go run ./tools/eval --db DB --queries testdata/eval/queries.jsonl` (keyword AND + semantic; top-1/top-5/MRR, avg query time). 40 queries drafted by Claude from the user's history (kept local, gitignored); **the user has not edited them yet**.
- [x] Baseline comparison: nomic-embed-text scored top-1 0.50 / top-5 0.68 / MRR 0.56, *below* L6 (see the bake-off).

**Exit criteria**
- [x] Tokenizer golden test: 100% exact match (48/48)
- [x] Embedding golden test passes: worst cosine **0.9999992** with the shipped f16 weights, **1.0000000** with the original f32 weights (the implementation is exact; only f16 rounding differs)
- [x] Load time, per-command latency and RAM are measured and recorded (below)
- [x] Eval numbers are recorded for semantic-only search. **The Go pipeline reproduces the Python bake-off exactly: top-1 0.55 / top-5 0.80 / MRR 0.65**, avg query 62.8 ms (battery).
- [x] **Go/no-go gate: GO** (user decisions 2026-09-27): quality: L6 chosen on the bake-off; speed: "keep the current implementation at 65 ms unless real-world usage shows it's too slow; no AVX2 assembly yet".

**Measurements so far (2026-09-27, i7-1255U laptop, single thread)**

⚠️ The laptop was **on battery** (balanced profile, cores at 0.4–2 GHz) for the trustworthy runs. Earlier runs, probably on AC, were about 1.7–2× faster but were compared across runs and are not reliable. Only same-session A/B comparisons count.

| What | On battery (A/B-verified) | Earlier, probably AC |
|---|---|---|
| Model load (parse + f16→f32) | ~105–115 ms, 92 MB allocated | ~118–137 ms |
| Embed 5 tokens | ~29 ms | ~17–20 ms |
| Embed 49 tokens | ~290 ms | ~143 ms |
| Embed 256 tokens (max) | ~1.8 s | ~0.95 s |

Cost is roughly linear: **~6 ms per token on battery**. The user's real data: commands median **14 tokens** (p90 34, max 112), eval queries median **11 tokens**. So a typical search query is **~65 ms on battery** (plus a one-time ~110 ms model load per process), and embedding all 369 unique commands takes **~42 s of single-core CPU** on battery.

**Real run, `loc embed --pending` on the user's 369 unique commands (battery):** 1 worker 40.9 s wall; **6 workers (default) 10.5 s** (57.7 s CPU); 12 workers 7.8 s (72.7 s CPU; the extra threads land on efficiency cores). **Peak RSS ≈ 230 MB** (92 MB f32 weights + the 45 MB embedded f16 copy + Go GC headroom). Fine for a background job; revisit for the long-lived TUI process (see backlog). **Binary: 6.6 MB → 52.4 MB**, still statically linked.

Tried and measured A/B, **none helped**, so the code stays simple: cache tiling (≈ same), GOAMD64=v3 / FMA fusion (≈ same), an 8-accumulator 2×4 kernel (2.8× slower: register spills), splitting one multiply across cores (slower: on this 2P+8E hybrid CPU the extra threads land on efficiency cores). Raw independent compute *does* scale ~6× on 12 threads, so parallelism belongs **across commands** in `loc embed`, not inside one multiply. The CPU supports AVX2+FMA, so SIMD assembly is the remaining big lever.

**Out of scope:** ranking fusion, hooks, TUI.

---

### Phase 3: Hybrid search
**Goal:** Search results that beat both keyword-only and semantic-only search.

**Tasks**
- [x] Vector search: `store.EmbeddingMatrix` (one contiguous matrix) + `search.topKByDot` (heap top-k, verified against a full sort). Scan: 2.5 ms at 10k, 25 ms at 100k, 130–150 ms at 500k (battery).
- [x] FTS5 BM25 keyword search (`store.KeywordIDs`, AND or OR mode). The default `unicode61` tokenizer is **kept**: splitting on `-` `/` `.` `:` worked on the fragment eval ("port-forward", "reset --soft", "base64 -d", "chmod +x" all found). **AND mode wins in hybrid** (below): a sentence rarely matches any command under AND, which leaves it to semantic ranking.
- [x] Weighted Reciprocal Rank Fusion (`search.fuse`), tuned on two eval sets with a grid (`go run ./tools/eval --grid`). **Shipped `DefaultParams`: AND keywords, keyword weight 0.5, semantic weight 1, k=60.** This is a flat optimum: the neighbours k=60/kw=0.25 and k=20/kw=0.5 score the same. The earlier naive default (OR, equal weights) scored lower on both sets.
- [x] Boosts, **decided by measurement**:
  - **run_count boost: rejected.** Even w=0.001·ln(1+runs) dropped MRR 0.65→0.57 (questions) and 1.00→0.71 (fragments): frequent commands (`clear` ×93, `git branch` ×48) swamp relevance, because adjacent fused ranks differ by only ~0.0004.
  - **recency and cwd/git-root boosts: deferred to after Phase 4.** They can't be measured yet: every imported command has the same time and cwd. Phase 4 capture provides real data (see the Phase 4 task).
  - **exit-code / this-dir / this-session filters: moved to Phase 5** with the TUI filter keys, where their exact meaning gets decided (e.g. "failed" = last run failed, or any run failed?).
- [x] Fallback (P7): no vectors → keyword results, silently (`TestSearchFallsBackToKeywords`). A real load *error* prints a one-line warning and still shows keyword matches.
- [x] `loc search` output: command, then `~/dir · 3 weeks ago · exit 0 · 4 runs` (relative time, never negative on clock skew).
- [x] Re-ran the eval through the shipped code path (`tools/eval` → `internal/search`). A second local eval set of **25 typed fragments** (`testdata/eval/fragments.jsonl`, gitignored) was added, because the 40 paraphrased questions deliberately avoid command words and can't show what keywords are good at.

  | Ranker | Questions (40) top1/top5/MRR | Fragments (25) top1/top5/MRR |
  |---|---|---|
  | keyword AND | 0.00 / 0.00 / 0.00 | 0.96 / 1.00 / 0.98 |
  | keyword OR | 0.40 / 0.53 / 0.47 | 0.96 / 1.00 / 0.98 |
  | semantic only | 0.55 / 0.80 / 0.65 | 0.88 / 0.96 / 0.91 |
  | **hybrid (DefaultParams)** | **0.55 / 0.80 / 0.65** | **1.00 / 1.00 / 1.00** |

**Measurements (2026-09-27, battery, fresh process per search)**

| Step | 369 cmds (user) | 10k | 100k | 500k |
|---|---|---|---|---|
| Model load (after the f16 lookup table: ~105 → ~70 ms, same-run A/B 1.4×) | 70 ms | 70 ms | 70 ms | 70 ms |
| Query embedding (median 11 tokens) | 63 ms | 63 ms | 63 ms | 63 ms |
| Load vectors from SQLite (`EmbeddingMatrix`, ~8 allocs/row) | <5 ms | 58 ms | **556 ms** | ~2.8 s (extrapolated) |
| Brute-force top-k scan (`topKByDot`) | <1 ms | 2.5 ms | 25 ms | 130–150 ms |
| Keyword (FTS5) | <1 ms | | | |

Finding: **< 100 ms end-to-end per fresh process is not reachable on battery at any size** with the current design. Model load + query embedding alone are ~133 ms. At 100k, loading vectors from SQLite dominates (556 ms). Scanning is not the problem. **Decision (user, 2026-09-27): "target what users feel"**: keyword results < 50 ms at 100k; semantic ready < 300 ms at 10k; SQLite stays the only store; revisit if real histories exceed ~30k.

**Exit criteria**
- [x] Hybrid ≥ the better of the two single methods on **top-1 and MRR**: equal to semantic on questions, above both on fragments (table above). Caveat: 65 queries, all drafted by Claude; differences under ~0.10 are noise.
- [x] Latency measured against the revised targets (user decision 2026-09-27: "target what users feel"):
  - **Keyword first results < 50 ms at 100k: met, 1–12 ms** including opening the DB (`BenchmarkKeywordLatency`, `LOC_BENCH_DB`).
  - **Semantic ready < 300 ms at 10k: met, ~0.19 s** end-to-end for the real `loc search` process (5 runs: 0.16–0.21 s). The user's 369 commands: ~0.17 s. 100k: ~0.75 s (no target).
  - Model and vectors load in parallel in background goroutines (`search.New`).
  - Peak RSS of `loc search`: 148 MB (369 commands), 185 MB (10k), **537 MB (100k)** (see backlog).

**Out of scope:** TUI, hooks.

---

### Phase 4: Capture (shell hooks + import)
**Goal:** Every command in bash and zsh is recorded automatically without slowing the prompt.

**Prereqs:** install zsh on the dev machine. Answer O2, O4, O5.

**Tasks**
- [ ] `loc record`: minimal fast path (open DB → scrub → insert → exit). No model loading.
- [ ] Benchmark `loc record` latency. The hook must detach it (background it so the prompt never waits).
- [ ] `shell/loc.zsh`: `preexec` saves the command and start time; `precmd` gets exit code and duration and calls `loc record &!`
- [ ] `shell/loc.bash`: same behavior via the chosen mechanism (§3 PROPOSED). Handle `HISTCONTROL`, multi-line commands, and subshells.
- [ ] `loc init bash|zsh`: prints the hook script (embedded with `go:embed`)
- [ ] Session id per shell instance, plus hostname
- [ ] Background embedding trigger: after recording, start `loc embed --pending` if no worker holds the lock (debounced, detached)
- [ ] Importer: `~/.bash_history` (plain, and with timestamps) and `~/.zsh_history` (plain, and extended format). Scrub everything, mark `source='import'`.
- [ ] **Deferred from Phase 3:** once capture has real timestamps and directories, build an eval with context (query + cwd + time) and measure recency and cwd/git-root boosts. Ship them only if they beat `DefaultParams`.
- [ ] Edge-case tests: very long commands, unicode, commands with newlines, rapid-fire commands, Ctrl-C'd commands, `exit`

**Exit criteria**
- [ ] Measured prompt overhead with the hook vs without it is recorded (target: not perceptible; the number is decided with the user)
- [ ] 1 day of real use in both bash and zsh: every command recorded, none duplicated or lost (spot-checked)
- [ ] Import of your real history completes and is searchable

**Out of scope:** TUI, the Ctrl-R binding, installer.

---

### Phase 5: TUI + Ctrl-R
**Goal:** Ctrl-R opens a fast inline search, and Enter fills the prompt.

**Tasks**
- [ ] Bubbletea TUI rendered on `/dev/tty`. The selected command goes to **stdout** only.
- [ ] Inline mode (about 15 lines under the prompt, not fullscreen). Handles terminal resize.
- [ ] Input box → keyword results as you type → semantic results merged after a debounce
- [ ] Result rows: command, dir, relative time, ✓/✗ exit, run count; secrets shown as `<REDACTED:…>`
- [ ] Filters: all / this dir / this session / failed only (key to cycle them)
- [ ] Keys: ↑/↓, Enter (accept), Tab (accept for editing; confirm the behavior with the user), Esc/Ctrl-C (cancel, leaving the buffer unchanged)
- [ ] Pre-fill the query with the current prompt buffer contents
- [ ] zsh widget: `zle` widget bound to `^R`, sets `BUFFER` and `CURSOR`
- [ ] bash widget: `bind -x` on `\C-r`, sets `READLINE_LINE` and `READLINE_POINT`
- [ ] Config opt-out: keep native Ctrl-R, and optionally bind a different key
- [ ] Never use `TIOCSTI` (disabled by default since Linux 6.2)

**Exit criteria**
- [ ] Time from keypress to TUI visible is measured (target to be agreed)
- [ ] Works in: GNOME Terminal/Ptyxis (Fedora default: verify which), plus at least one more terminal (**ask the user which ones they use**), and inside tmux
- [ ] Cancel leaves the prompt exactly as it was

**Out of scope:** installer.

---

### Phase 6: Zero-setup install, uninstall, and management
**Goal:** "Install, open a new terminal, done", and just as easy to remove.

**Prereqs:** answer O3.

**Tasks**
- [ ] `install.sh`:
  - Detect arch and download the matching release binary
  - **Verify its checksum**
  - Install to `~/.local/bin` (check it's on PATH; if not, add a marked PATH line)
- [ ] `loc setup`: detect the user's shells and add a marked, idempotent block to `~/.bashrc` / `~/.zshrc`:
  ```
  # >>> loc >>>
  eval "$(loc init bash)"
  # <<< loc <<<
  ```
- [ ] `install.sh` calls `loc setup` automatically
- [ ] First run: automatic history import + background embedding, with no user action
- [ ] `loc uninstall`: remove marked blocks → ask whether to delete data → remove binary (only if installed by `install.sh`)
- [ ] `loc status`: record count, pending embeddings, redaction count, DB size, hook detected in the current shell?
- [ ] `loc forget <id | --match pattern>`: delete from all tables, including FTS and vectors
- [ ] Config file support (location/format per §3, once confirmed)
- [ ] Package-manager caveat: distro packages must not edit `$HOME`. Decide per package whether to use `/etc/profile.d` or a post-install "run `loc setup`" message (**decide with the user**).

**Exit criteria**
- [ ] Fresh Fedora VM/container: run the installer → new terminal → commands recorded → Ctrl-R works, **with zero other steps**
- [ ] Running the installer twice causes no duplicate rc blocks
- [ ] Uninstall leaves the rc files byte-identical to before (diff checked)

---

### Phase 7: Release v0.1.0
**Goal:** A public, trustworthy first release.

**Tasks**
- [ ] GoReleaser: Linux amd64/arm64 static binaries, checksums, (optional) signing: **ask** whether to use cosign or GPG
- [ ] README: what it is, the privacy guarantees (no network, scrubbing), install, uninstall, how search works, limitations
- [ ] `SECURITY.md` (how to report a scrubber miss), `CONTRIBUTING.md`, issue templates
- [ ] Dogfood period: the user runs it daily for a period **the user decides**, and bugs are logged here
- [ ] Resolve O1 (name collision) before submitting packages
- [ ] Packages: chosen per §3 PROPOSED, once confirmed

**Exit criteria**
- [ ] Tagged `v0.1.0` release with binaries + checksums
- [ ] Install from the release works on a clean machine
- [ ] No open P1–P8 violations

---

### Phase 8+: Toward "fully fledged" (order to be decided with the user after v0.1)
Candidates. Each becomes a scheduled phase only after the user picks it:
- macOS support
- fish shell
- Optional Ollama backend (bigger models)
- LLM-generated one-line descriptions of each unique command (needs Ollama or a bundled LLM). **Evidence from the 2026-09-27 bake-off:** every model missed questions needing command *meaning* ("shelve changes" → `git stash`, "apply a single commit" → `cherry-pick`, "undo last commit, keep changes" → `reset --soft`). This is the most likely next quality jump.
- Better in-process model (transformer via pure Go)
- Opt-in output capture: `loc run -- <cmd>`, and/or terminal integration via OSC 133
- `loc stats` (most used, most failed, per-project)
- Per-directory / per-project history views
- Encrypted-at-rest database
- Export/import (JSON) for moving machines

---

## 6. Progress log

**Current phase: Phase 4 (not started)**. Phase 3 is complete: the user commits it first.

| Date | Phase | What happened / evidence |
|---|---|---|
| 2026-09-26 | Planning | Brief reviewed; architecture revised (hybrid search, dedup, no daemon, no PTY, in-process embeddings, Go). Decisions locked (§3). Roadmap created. |
| 2026-09-26 | Phase 0 ✅ | Private repo `AniketR10/loc` created. Local: vet + staticcheck + tests pass; `bin/loc` is statically linked (`ldd`: not a dynamic executable), 1.5 MB. First CI run passed: https://github.com/AniketR10/loc/actions/runs/36258401581. LICENSE was added by the user in 8d30b1b. |
| 2026-09-27 | Phase 1 ✅ | Scrubber (4 layers, 43 positive / 51 negative corpus, fuzzed) + SQLite store (schema v1, migrations, WAL, 0600/0700) + `loc add` / `loc search`. All exit criteria pass locally (`make lint test check-static`). The fuzzer found and fixed a multi-password leak. Not yet committed: the user commits. |
| 2026-09-27 | Phase 2 ✅ | Bake-off (L6 chosen; `docs/decisions/0001-embedding-model.md`), exact pure-Go tokenizer + BERT (cosine 1.0000000 f32 / 0.9999992 f16), f16 weights via `make model` + GitHub Release, schema v2 `embeddings`, `loc embed --pending`, Go eval harness reproducing the bake-off (0.55/0.80/0.65). Speed accepted by the user. Not committed yet: the user commits. |
| 2026-09-27 | Phase 3 ✅ | Hybrid search (`internal/search`): AND keywords + semantic, weighted RRF (kw 0.5, k=60), tuned on 40 questions + 25 fragments (0.55/0.80/0.65 and 1.00/1.00/1.00). run_count boost rejected by measurement; recency/cwd boosts deferred to after Phase 4; filters moved to Phase 5. Targets revised with the user and met (keyword 1–12 ms at 100k; semantic ~0.19 s at 10k). Schema v3 drops `embedded_at`; f16 lookup table (load 105→70 ms). Not committed yet: the user commits. |

---

## 7. Backlog (unscheduled)
_Ideas that come up mid-phase go here, not into the code._

- Memory at scale: `loc search` peaks at 537 MB with 100k commands (154 MB f32 vectors + SQLite blob copies while loading; `EmbeddingMatrix` allocates ~470 MB at 100k). Options to measure: stream-decode without per-row copies, or store vectors as f16/int8 (2–4× smaller).
- Memory: `loc embed` peaks at ~230 MB RSS (f32 weights + the embedded f16 copy + GC headroom). Before the TUI (Phase 5) holds the model for a whole session, try `debug.SetGCPercent`/`SetMemoryLimit` or dropping the f16 bytes after conversion, measured A/B.
- Scrubber gap (found 2026-09-27 on real history): Langfuse secret keys `sk-lf-<uuid>` are only caught next to a telling name (`*_SECRET_KEY=`). Passed bare, the hex+dash value is skipped by the entropy layer. Add a known-token rule for `sk-lf-` (and ask whether `pk-lf-` public keys should be redacted too).
- CI: GitHub says `ubuntu-latest` moves to Ubuntu 26 from 2026-10-19. Static binaries shouldn't care, but check the first CI run after that date.
