# loxx — Roadmap & Source of Truth

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

**loxx** is an open-source (MIT), local-first shell-history tool for Linux:
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
4. `loxx uninstall` removes the hook lines and, if they confirm, the data.

---

## 2. Core principles (non-negotiable)

| # | Principle | How it's enforced |
|---|---|---|
| P1 | **Never slow the prompt.** | The hook runs `loxx record` in the background. Record latency is benchmarked in CI. |
| P2 | **Never store an unscrubbed secret.** | All writes go through the scrubber. There is no code path that writes raw text. A test corpus runs in CI. |
| P3 | **No network by default.** | The embedding model is compiled in. No telemetry. Any network feature is opt-in and documented. |
| P4 | **Single static binary.** | `CGO_ENABLED=0`, checked in CI. No runtime dependencies. |
| P5 | **Never execute on the user's behalf.** | Selecting a result only fills the prompt buffer. |
| P6 | **Fully reversible.** | rc-file edits are wrapped in markers and idempotent. `loxx uninstall` removes everything. |
| P7 | **Search still works without embeddings.** | If the model or embeddings aren't ready, fall back to FTS keyword search. |
| P8 | **Fully open source.** | MIT. Every dependency and the model weights must have an OSI-compatible license, checked before adding. |

---

## 3. Decisions

### LOCKED (decided by the user, 2026-09-26)
| Topic | Decision |
|---|---|
| Language | **Go** (pure Go, no CGO) |
| Project / binary name | **loxx** (renamed from `loc` on 2026-09-30 by the user, before Phase 6 bakes the name into the installer; this also resolves the old name clash with `cgag/loc`, a lines-of-code counter). Everything was renamed: command, binary, shell function, `LOXX_*` env vars, data folder `~/.local/share/loxx` (an existing `~/.local/share/loc` holding our `history.db` is **moved automatically** on first use), Go module `github.com/AniketR10/loxx`, docs. **The GitHub repo must be renamed by the user** (Settings → Rename); GitHub redirects the old URLs. |
| Go module path | **github.com/AniketR10/loxx** (was `…/loc` until 2026-09-30) |
| License | **MIT** |
| v1 platform | **Linux only** (amd64 + arm64: see PROPOSED) |
| v1 shells | **Bash + Zsh** |
| How the search panel opens | **Type `loxx`** (no arguments). **No keyboard shortcut** (user, 2026-09-29; replaces "take over Ctrl-R": Ctrl-R and the candidate keys are used by the user or by VS Code's terminal). The hook defines a `loxx` shell function: in **zsh** the chosen command lands in the next prompt (`print -z`); in **bash**, which cannot pre-fill the next prompt, it is added to history, **one ↑ away**. Running the binary directly (no hook) prints the choice to stdout. Enter never executes (P5). |
| Setup burden | **Install only.** No manual steps (except the package-manager caveat in Phase 6) |
| Embedding runtime | **In-process, model compiled into the binary.** No Ollama required. |
| Embedding model | **`sentence-transformers/all-MiniLM-L6-v2`**, pinned to HF revision `1110a243fdf4706b3f48f1d95db1a4f5529b4d41` (user decision 2026-09-27 after the bake-off; see `docs/decisions/0001-embedding-model.md`). Verified: **Apache-2.0**; BERT with **6 layers**, hidden 384, 12 heads, FFN 1536, GELU, LayerNorm eps 1e-12; uncased WordPiece, vocab 30,522 (identical to L12); BertNormalizer (clean_text, chinese chars, lowercase, strip accents) + BertPreTokenizer + WordPiece (`##`, max 100 chars/word); **max_seq_length 256**; mean pooling → L2 normalize → 384-dim. Weights 90.9 MB f32 (~45 MB f16). model.safetensors sha256 `53aa5117…d9db`. **Fallback** if pure-Go speed fails the gate: potion-base-8M. |
| Embedding speed | **Accepted as-is** (~65 ms per typical query on battery, ~110 ms model load). **No AVX2 assembly** unless real-world use shows it's too slow (user decision, 2026-09-27) |
| Model weights distribution | **Not in git.** `make model` downloads `minilm-l6-v2.f16.safetensors` from the `model-minilm-l6-v2-f16` GitHub Release on this repo and verifies SHA-256 `aa3d97ae…272b` (build/test/lint depend on it). vocab.txt, LICENSE and README stay in git. User chose build-time download + SHA-256 (2026-09-27); Claude recommended GitHub Release over Hugging Face because users already download binaries from GitHub Releases (no new point of failure). End users are unaffected: release binaries embed the model. **Known cost: `go install …@latest` doesn't work** (document in the README). CI fetches with `gh` + token while the repo is private. **Rejected alternative (2026-09-27):** having `loxx` download the model on first run (to `~/.loxx/models/`). It gives a smaller binary (~7 MB vs ~52 MB), but breaks P3 (network use), the offline/air-gapped story and the XDG data location. |
| `executions.source` values | **`'live'`, `'import'`, `'manual'`**. `'manual'` is kept for `loxx add` (user, 2026-09-27; was O7) |
| `commands.embedded_at` | **Dropped** in migration v3; the `embeddings` table is the single source of truth (user: "remove the unnecessary things", 2026-09-27; was O8) |
| Vector search | **Brute-force cosine in Go** over stored float32 vectors, no sqlite-vec (user, 2026-09-27) |
| Search ranking | **Keyword BM25 + semantic, fused (RRF), then recency/frequency/cwd boosts**, with every weight **tuned against the eval set, not assumed** (user, 2026-09-27) |
| Search latency target | **Keyword results < 50 ms at 100k; semantic results ready < 300 ms at 10k** (user, 2026-09-27; replaced "< 100 ms end-to-end at 100k" after measurements showed model load + query embedding alone take ~133 ms on battery). SQLite stays the only store; revisit if real histories exceed ~30k commands. |
| Bash hook mechanism | **Vendored `bash-preexec`** (user, 2026-09-28). License to be verified before vendoring (expected MIT). Known trade-off: it uses bash's DEBUG trap, which can clash with other tools that set their own. |
| Imported commands without timestamps | **Time unknown**: `executions.started_at` is NULL and the UI shows "imported" instead of a relative time. `commands.first_seen/last_seen` still hold the import time for internal ordering only (user, 2026-09-28; was O2) |
| Minimum shell versions | **bash ≥ 4.4, zsh ≥ 5.1** (user, 2026-09-28; was O4) |
| Commands inside `ssh` sessions | **Out of scope for v1**: loxx records only on the machine where it is installed. No syncing or forwarding (user, 2026-09-28; was O5) |
| Prompt-overhead target | **≤ 10 ms added per command** (user, 2026-09-28, after measuring bash +3.9 ms and zsh +1.3 ms) |
| TUI library | **Bubbletea v2.0.9 alone** (no Bubbles, no Lipgloss), user 2026-09-29 after a measured spike: +2.1 MB binary, 24.8 ms first paint, 16 MIT/BSD modules (`docs/decisions/0002-tui-library.md`). Pinned below v2.0.10, which needs Go 1.26. |
| Panel behaviour (user, 2026-09-29) | **Empty query → most recent commands.** **Tab cycles filters** (all → this dir → this session → failed). **"Failed" = the last run failed.** **"This dir" = the same directory, or anywhere in the same git repo when in one.** Enter fills the prompt and never executes (P5). |
| Language policy | **Dev-only tooling may use any language** (Python for model conversion, golden generation, bake-offs). **Code that ships to users stays Go**, because of P1 (fast startup on every prompt), P3/P4 (single static binary, zero setup) and the install-only promise, not out of language preference. (User said the focus is performance and correctness, not language, 2026-09-27) |
| Inference implementation | **Hand-written pure Go** (user decision, 2026-09-27; hugot rejected). If a transformer wins: BERT forward pass + WordPiece. If model2vec wins: WordPiece + lookup + mean pooling. Weights loaded from `go:embed`. Only extra dependency: `golang.org/x/text` v0.41.0 (BSD-3, needed for Unicode NFD in the tokenizer; v0.42+ requires Go 1.26). |
| Weight precision | **f16** in the binary, converted to f32 at load (user decision, 2026-09-27; carried over from L12 to L6) |
| Dev-only downloads | **Approved** (2026-09-27): L12 model files, a Python venv with sentence-transformers + CPU torch, Ollama `nomic-embed-text`, plus for the bake-off: the `model2vec` package, potion-base-8M and all-MiniLM-L6-v2. Never shipped to users. |
| Eval queries | **Claude drafts them from `~/.bash_history`, and the user edits them** (2026-09-27) |
| Output (stdout) capture via PTY | **Not in v1.** Backlog only. |
| Database | **SQLite** (not DuckDB) |
| CLI library | **stdlib `flag`** + a small hand-written subcommand dispatcher. No cobra: keeps `loxx record` lean and the dependency count low. (User asked for a recommendation and accepted it, 2026-09-26) |
| Directory layout | As listed in Phase 0 (approved 2026-09-26). Directories are created when first needed, not up front. |
| GitHub repo | **Private** for now: `github.com/AniketR10/loxx` |
| LICENSE file | **The user adds it themselves.** Claude must not create or edit `LICENSE`. |
| Go version | `go 1.25.0` minimum, `toolchain go1.25.12` (the locally installed version) |
| SQLite driver | **modernc.org/sqlite** v1.59.0 (pure Go, FTS5), approved 2026-09-27 |
| Data location | **`$XDG_DATA_HOME/loxx/history.db`** (default `~/.local/share/loxx/`), overridable with **`LOXX_DB_PATH`**, approved 2026-09-27 |
| Embeddings table | **Created in Phase 2**, not Phase 1 (only create tables when they're used), approved 2026-09-27 |
| Linting | `go vet` + `staticcheck` **2026.1**, pinned. 2026.2+ requires Go 1.26 and would silently download a second toolchain. |
| Build tool | `Makefile` (`build`, `test`, `lint`, `bench`, `check-static`, `clean`) |
| CI | GitHub Actions: `actions/checkout@v7`, `actions/setup-go@v7` (latest releases as of 2026-09-26) |

### PROPOSED (confirm before the phase that uses it)
| Topic | Proposal | Needed by |
|---|---|---|
| Config format / location | TOML at `$XDG_CONFIG_HOME/loxx/config.toml` | Phase 6 |
| Linux architectures | amd64 + arm64 | Phase 7 |
| Release tooling | GoReleaser + GitHub Actions | Phase 7 |
| Distro packages | AUR, Fedora COPR, others | Phase 7 |

### OPEN (unknown: must ask)
| # | Question | Blocks |
|---|---|---|
| O3 | Install URL / hosting for `install.sh` (GitHub raw? GitHub Pages? a custom domain?) | Phase 6 |
| O6 | Go 1.26 is out (1.26.8 seen 2026-09-26), but 1.25.12 is installed locally. Move the project to Go 1.26? **Four dependencies are now pinned below latest because of this** (staticcheck 2026.1, x/text v0.41.0, x/term v0.45.0, bubbletea v2.0.9). | Any time (rising priority) |

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
- [x] `go mod init github.com/AniketR10/loxx`, and set the minimum Go version in `go.mod`
- [x] Directory layout (approved):
  ```
  cmd/loxx/            main entry, CLI wiring
  internal/store/     SQLite schema, migrations, queries
  internal/scrub/     secret scrubber
  internal/embed/     model2vec tokenizer + embedding
  internal/search/    hybrid ranking
  internal/capture/   record fast path, session ids
  internal/importer/  bash/zsh history import
  internal/tui/       Bubbletea UI
  internal/setup/     rc-file install/uninstall
  shell/              loxx.bash, loxx.zsh (go:embed)
  testdata/           secrets corpus, eval queries, golden vectors
  ```
- [x] CLI skeleton: `loxx version`, `loxx help` (stdlib `flag`: see §3)
- [x] Makefile: `build`, `test`, `lint`, `bench`, `check-static`, `clean`
- [x] GitHub repo created (private: approved by the user)
- [x] CI: build with `CGO_ENABLED=0`, `go test ./...`, lint, and a check that the binary is static

**Exit criteria**
- [x] `CGO_ENABLED=0 go build ./cmd/loxx` produces a static binary (checked with `file`/`ldd`)
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
  - `executions(id, command_id, cwd NULL, git_root NULL, exit_code NULL, duration_ms NULL, started_at NULL, session_id, hostname, source['live'|'import'|'manual'])`. NULL means unknown (e.g. imports). The `'manual'` source value was added for `loxx add` (**confirm: O7**).
  - `commands_fts`: FTS5 (default `unicode61` tokenizer), kept in sync by triggers
  - `meta(key, value)`: holds `schema_version`
  - `embeddings`: **deferred to Phase 2** (user decision)
- [x] Migrations framework: versioned and forward-only. Checks the version without a lock first, then migrates under `BEGIN IMMEDIATE`. Refuses to open a database from a newer loxx.
- [x] WAL, `busy_timeout=10000`, `synchronous=NORMAL`, `foreign_keys=ON`, and `_txlock=immediate` (all writes take the write lock up front). DB file `0600` (the -wal and -shm files inherit it), newly created data dir `0700`. Existing dirs are never chmod-ed.
- [x] Concurrency test: 20 **separate processes** race to create, migrate and write one fresh DB
- [x] CLI: `loxx add [--cwd DIR] [--exit CODE] "<cmd>"`: prints a notice on stderr when it redacts something
- [x] CLI: `loxx search [--limit N] <query>`: words are ANDed, each word also matches as a prefix, ranked by BM25. Default limit 5 (from the original brief). Exits 1 on no matches (like grep).
- [x] Data path: `$LOXX_DB_PATH` → `$XDG_DATA_HOME/loxx/history.db` → `~/.local/share/loxx/history.db`

**Exit criteria**
- [x] All positive corpus secrets are redacted and all negative items are left intact (100%): `TestPositiveCorpus`, `TestNegativeCorpus`
- [x] A grep of the DB files (db + wal + shm, while open and after close) for every corpus secret finds nothing, with a positive control proving the check reads stored data: `TestNoSecretsOnDisk`. Also confirmed by a manual smoke test of the real binary.
- [x] 20 concurrent writers × 500 inserts: 0 errors, 10,000/10,000 rows, correct run counts, in about 2.1 s: `TestConcurrentWriters`
- [x] `loxx add` then `loxx search` round-trips correctly: `TestAddAndSearch`, plus a manual smoke test

**Measurements (2026-09-27, dev machine)**
- `scrub.Scrub`: about 6.5 µs for `git status`, 135–145 µs for long commands with secrets (0–7 allocations). This will matter for the Phase 4 `loxx record` latency budget.
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
- [x] **Embedding golden test:** 48 strings (`testdata/embed/golden.json`, from `tools/model/goldens.py`). Bar: cosine ≥ 0.9999 (f16) and ≥ 0.99999 (f32, opt-in via `LOXX_EMBED_F32`).
- [x] Weight conversion script `tools/model/convert.py`: f32 → f16 safetensors (101 of 104 tensors; position_ids and pooler dropped), max rounding error 0.00098, **45.1 MB**. Binary-size delta: measured once `loxx` imports the embed package (`loxx embed`).
- [x] Ship the model's Apache-2.0 license text alongside the weights (`internal/embed/model/LICENSE` + `README.md` with source revision, checksums and modifications). Mention it in the top-level README when that is written (Phase 7).
- [x] `make model`: download + SHA-256 verification (tested: a bad hash, a 404, and wrong content are all rejected with no partial file left). CI caches the file.
- [x] Created the `model-minilm-l6-v2-f16` GitHub Release (prerelease, `--latest=false`) with the weights + LICENSE, 2026-09-27, with the user's OK. The local SHA-256 was verified before upload. A read-back check was declined by the user; CI's `make model` verifies the SHA-256 on first download, and the prerelease/not-latest flags must be confirmed before Phase 6's `install.sh` relies on "latest".
- [x] Migration v2: `embeddings(command_id PK → commands ON DELETE CASCADE, model_id, dims, vector BLOB)`, tested including an upgrade of a v1 database with data. `meta.active_model` was not added (the per-row `model_id` covers model changes; accepted 2026-09-27). `commands.embedded_at` was dropped in v3.
- [x] `loxx embed --pending [--workers N]`: batches of 64, most recently used first, saved per batch (Ctrl-C loses at most one batch), `flock` lockfile `<db>.embed.lock` (a second run exits 0 with a notice), parallel **across commands** (default workers = NumCPU/2 = 6 here). It does **not** lower its own CPU priority: Linux `setpriority` only affects one thread of a Go process, so Phase 4's hook should launch it with `nice`.
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

**Real run, `loxx embed --pending` on the user's 369 unique commands (battery):** 1 worker 40.9 s wall; **6 workers (default) 10.5 s** (57.7 s CPU); 12 workers 7.8 s (72.7 s CPU; the extra threads land on efficiency cores). **Peak RSS ≈ 230 MB** (92 MB f32 weights + the 45 MB embedded f16 copy + Go GC headroom). Fine for a background job; revisit for the long-lived TUI process (see backlog). **Binary: 6.6 MB → 52.4 MB**, still statically linked.

Tried and measured A/B, **none helped**, so the code stays simple: cache tiling (≈ same), GOAMD64=v3 / FMA fusion (≈ same), an 8-accumulator 2×4 kernel (2.8× slower: register spills), splitting one multiply across cores (slower: on this 2P+8E hybrid CPU the extra threads land on efficiency cores). Raw independent compute *does* scale ~6× on 12 threads, so parallelism belongs **across commands** in `loxx embed`, not inside one multiply. The CPU supports AVX2+FMA, so SIMD assembly is the remaining big lever.

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
- [x] `loxx search` output: command, then `~/dir · 3 weeks ago · exit 0 · 4 runs` (relative time, never negative on clock skew).
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
  - **Keyword first results < 50 ms at 100k: met, 1–12 ms** including opening the DB (`BenchmarkKeywordLatency`, `LOXX_BENCH_DB`).
  - **Semantic ready < 300 ms at 10k: met, ~0.19 s** end-to-end for the real `loxx search` process (5 runs: 0.16–0.21 s). The user's 369 commands: ~0.17 s. 100k: ~0.75 s (no target).
  - Model and vectors load in parallel in background goroutines (`search.New`).
  - Peak RSS of `loxx search`: 148 MB (369 commands), 185 MB (10k), **537 MB (100k)** (see backlog).

**Out of scope:** TUI, hooks.

---

### Phase 4: Capture (shell hooks + import)
**Goal:** Every command in bash and zsh is recorded automatically without slowing the prompt.

**Prereqs:** install zsh on the dev machine (**pending: the user runs `sudo dnf install zsh`**). O2, O4 and O5 are answered (§3, 2026-09-28).

zsh 5.9 turned out to be installed already.

**Tasks**
- [x] `loxx record [--exit N] [--start T] [--end T] [--cwd DIR] [--session ID] -- CMD`: open DB → scrub → insert → exit, no model loading. Also stores the git root (walks up to `.git`, a dir or a worktree file) and the hostname. Accepts `EPOCHREALTIME` with a comma decimal (some locales). Ignored and empty commands exit 0 silently.
- [x] `loxx record` latency: **7.2 ms** median per run (background; the prompt never waits for it).
- [x] `shell/loxx.zsh`: native `preexec`/`precmd` (runs first among precmd hooks so `$?` is intact), backgrounds `loxx record` with `&!`.
- [x] `shell/loxx.bash`: bash-preexec 0.7.0 vendored unmodified (MIT notice printed as comments), sourced from a here-document so its top-level `return` can't skip the rest of `.bashrc`. **bash-preexec strips `ignorespace` from HISTCONTROL** (so it can see every command), which would make bash save space-prefixed commands to its history file; the hook detects the user's original setting and deletes those entries itself (tested). On bash ≥ 5.3 bash-preexec uses PS0, not the DEBUG trap. Timestamps: `$EPOCHREALTIME` on bash 5+, whole seconds via `printf '%(%s)T'` on 4.4 (no forks).
- [x] `loxx init bash|zsh`: prints the hook with the `loxx` binary's absolute path filled in (shell-quoted).
- [x] Session id per shell instance (`$$-<start time>`), plus hostname.
- [x] Background embedding trigger: `startBackgroundEmbed` → `nice -n 10 loxx embed --pending --wait 2s`, detached (`setsid`), only when no embed holds the lock; the 2 s wait batches rapid commands into one model load. `LOXX_BACKGROUND_EMBED=0` disables it (tests must).
- [x] Importer (`internal/importer`, `loxx import`): bash plain and `#<epoch>`-timestamped (multi-line entries rejoined), zsh plain and extended (duration kept, backslash continuations, **unmetafied**: zsh escapes bytes 0x00 and 0x83–0xA2). Scrubbed, `source='import'`, one transaction (`store.AddBatch`), idempotent per file via a `meta` marker (`--force` re-imports). Imported commands without times show **"imported"** (`Result.LastRun` is nil).
- [x] **Deferred from Phase 3 → moved to the backlog** (user, 2026-09-28): the recency/cwd-boost eval needs ~2 weeks of real captured history.
- [x] Edge cases, tested in **real interactive bash and zsh** running in a pseudo-terminal opened in Go (`TestShellHooks`, `TestShellHooksRapidFire`; no `script(1)` dependency, temp HOME/HISTFILE): exit codes, cwd, session, time, duration; unicode; a multi-line `for` loop stored as one command; Ctrl-C'd `sleep` → exit 130; space-prefixed commands not recorded (and kept out of bash's history); **100 typed-ahead commands → exactly 100 records**. Not tested: commands over Linux's 128 KiB single-argument limit can't be passed to `loxx record` and are silently not recorded (accepted; it only affects absurdly long commands).

**Exit criteria**
- [x] Prompt overhead measured (`LOXX_BENCH_HOOKS=1 go test ./cmd/loxx -run TestHookOverhead`, median of 5 bursts of 200 typed-ahead commands; power state not noted): **bash +3.9 ms per command, zsh +1.3 ms**. **Target (user, 2026-09-28): ≤ 10 ms per command: met.** bash costs more because bash-preexec forks for `$(history 1)` and the hook forks once more to background `loxx record` silently.
- [x] 1 day of real use (2026-09-28 19:04 → 09-29 16:39), checked against the real DB read-only: **41 live commands over 13 sessions, every field filled, 0 duplicates, 0 space-prefixed recorded, 0 pending embeddings**, files 0600/0700. Cross-checked with `~/.bash_history`, which had two commands loxx lacked:
  - ` source …/activate`: space-prefixed, so correctly not recorded. bash saved it only because Fedora's `/etc/profile` sets `HISTCONTROL=ignoredups` (no `ignorespace`); the hook correctly leaves that alone.
  - `tmux`: **a real gap, now fixed.** The terminal was closed while tmux ran, so the shell died without another prompt and the command was lost. Now an exit hook (bash `EXIT` trap chained onto any existing one; zsh `zshexit`) records the in-flight command with its exit code and duration unknown (user decision, 2026-09-29). Tested with SIGHUP in both shells (`TestShellHooksHangup`). A typed `exit` is now recorded too.
  - **Also found and fixed during this check:** `TestConcurrentWriters` had turned flaky (10 of 15 runs lost writers): processes racing to set up a *new* database got `SQLITE_BUSY` at the first connection (WAL switch) without busy_timeout applying. `store.Open` now holds an exclusive `flock` on `history.db.lock` for the first connection and migration check (20/20 runs pass; `loxx record` 4.1 ms median on AC). `loxx uninstall` (Phase 6) must remove `history.db.lock` and `history.db.embed.lock`.
- [x] Import of the user's real history completes and is searchable (2026-09-28): 965 commands from `~/.bash_history`, 25 space-prefixed skipped, 1 secret redacted; results show "imported". This surfaced a bug, now fixed: `make build`'s version came from the nearest *any* tag (`model-minilm-l6-v2-f16-15-g…`); `VERSION` now only matches `v[0-9]*` tags.

**Out of scope:** TUI, the Ctrl-R binding, installer.

---

### Phase 5: Search panel (`loxx`)
**Goal:** typing `loxx` opens a fast inline search, and Enter puts the chosen command in the prompt (zsh) or one ↑ away (bash), never executing it.

**Tasks**
- [x] Bubbletea v2.0.9 panel (`internal/tui`), rendered on `/dev/tty`; the chosen command goes to **stdout** only (`loxx panel`, and plain `loxx`). Exit 1 with no output on cancel.
- [x] Inline (not fullscreen), up to 15 lines, never taller than the terminal minus the prompt line; resize via `WindowSizeMsg`; the panel erases itself on exit.
- [x] Keyword results on every keystroke; meaning-based results 150 ms after typing pauses; results from older keystrokes are dropped (sequence number); a footer says "matching by meaning…" meanwhile.
- [x] Rows: command (newlines shown as ↵), then `~/dir · 3 weeks ago · 4× ✓/✗` (or "imported"), cut to the terminal width, wide characters handled (`x/ansi`); secrets are shown as stored (`<REDACTED:…>`).
- [x] Filters: all / this dir (or same git repo) / this session / failed (last run failed); **Tab / Shift-Tab cycle them**. "This session" is skipped when the session is unknown (it would silently match everything: a bug caught by the tests). Filters apply inside the SQL (keyword, recent) and to the vector scan (semantic).
- [x] Keys: ↑/↓ (Ctrl-P/N), Enter, Tab/Shift-Tab, Esc/Ctrl-C/Ctrl-G cancel, Backspace, Ctrl-U, Ctrl-W.
- [x] Empty query shows the most recent commands.
- [x] `loxx` shell function in both hooks: no arguments → panel; zsh `print -z` pre-fills the next prompt; bash `history -s` puts it one ↑ away and prints a dim "↑ to use: …" hint; with arguments → the binary.
- [x] The `loxx` binary with no arguments opens the panel and prints the choice (works without the hook).
- [x] Never uses `TIOCSTI`.
- [x] Tests (real binary in a pseudo-terminal, 80×24): `TestPanel` (keyword, recent-first, arrows, Tab→failed, meaning with no shared words, Esc, Ctrl-C, empty history) and `TestLocFunction` (real bash and zsh: cancel leaves nothing behind; choose → Enter in zsh / ↑ Enter in bash actually runs it).
- [x] Result formatting moved to `internal/format` (shared by `loxx search` and the panel).

**Measurements (2026-09-29, AC power)**: time from launching `loxx panel` to the first frame: **~30 ms median at any history size** (your 1,006 commands: 29.7 ms; 10k: 29.7 ms; 100k: 28.8 ms; max 40 ms; 20/20 runs), because the model and vectors load in the background after the first frame. Binary: 52.4 → **54.1 MB** (+1.6 MB).

**Exit criteria**
- [x] Time to the panel being visible: **~30 ms median, max 40 ms** (AC). **Target (user, 2026-09-29): ≤ 100 ms: met.**
- [x] Works in the terminal the user actually uses: **VS Code's integrated terminal** (user, 2026-09-29). Checked by hand by the user, who reported "everything is working fine" (2026-09-29). Which scenarios they covered (e.g. inside tmux) was not itemized.
- [x] Cancel leaves the prompt and history exactly as they were: `TestLocFunction` in real bash and zsh (a command typed right after a cancel runs cleanly), plus the user's manual check. In bash the `loxx` function returns before `history -s` when cancelled.

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
- [ ] `loxx setup`: detect the user's shells and add a marked, idempotent block to `~/.bashrc` / `~/.zshrc`:
  ```
  # >>> loxx >>>
  eval "$(loxx init bash)"
  # <<< loxx <<<
  ```
- [ ] `install.sh` calls `loxx setup` automatically
- [ ] First run: automatic history import + background embedding, with no user action
- [ ] `loxx uninstall`: remove marked blocks → ask whether to delete data → remove binary (only if installed by `install.sh`)
- [ ] `loxx status`: record count, pending embeddings, redaction count, DB size, hook detected in the current shell?
- [ ] `loxx forget <id | --match pattern>`: delete from all tables, including FTS and vectors
- [ ] Config file support (location/format per §3, once confirmed)
- [ ] Package-manager caveat: distro packages must not edit `$HOME`. Decide per package whether to use `/etc/profile.d` or a post-install "run `loxx setup`" message (**decide with the user**).

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
- Opt-in output capture: `loxx run -- <cmd>`, and/or terminal integration via OSC 133
- `loxx stats` (most used, most failed, per-project)
- Per-directory / per-project history views
- Encrypted-at-rest database
- Export/import (JSON) for moving machines

---

## 6. Progress log

**Current phase: Phase 6 (not started)**. Phase 5 is complete: the user commits it first. Before starting, confirm §3 PROPOSED "Config format / location" and answer O3 (install URL / hosting).

| Date | Phase | What happened / evidence |
|---|---|---|
| 2026-09-26 | Planning | Brief reviewed; architecture revised (hybrid search, dedup, no daemon, no PTY, in-process embeddings, Go). Decisions locked (§3). Roadmap created. |
| 2026-09-26 | Phase 0 ✅ | Private repo `AniketR10/loxx` created. Local: vet + staticcheck + tests pass; `bin/loxx` is statically linked (`ldd`: not a dynamic executable), 1.5 MB. First CI run passed: https://github.com/AniketR10/loxx/actions/runs/36258401581. LICENSE was added by the user in 8d30b1b. |
| 2026-09-27 | Phase 1 ✅ | Scrubber (4 layers, 43 positive / 51 negative corpus, fuzzed) + SQLite store (schema v1, migrations, WAL, 0600/0700) + `loxx add` / `loxx search`. All exit criteria pass locally (`make lint test check-static`). The fuzzer found and fixed a multi-password leak. Not yet committed: the user commits. |
| 2026-09-27 | Phase 2 ✅ | Bake-off (L6 chosen; `docs/decisions/0001-embedding-model.md`), exact pure-Go tokenizer + BERT (cosine 1.0000000 f32 / 0.9999992 f16), f16 weights via `make model` + GitHub Release, schema v2 `embeddings`, `loxx embed --pending`, Go eval harness reproducing the bake-off (0.55/0.80/0.65). Speed accepted by the user. Not committed yet: the user commits. |
| 2026-09-27 | Phase 3 ✅ | Hybrid search (`internal/search`): AND keywords + semantic, weighted RRF (kw 0.5, k=60), tuned on 40 questions + 25 fragments (0.55/0.80/0.65 and 1.00/1.00/1.00). run_count boost rejected by measurement; recency/cwd boosts deferred to after Phase 4; filters moved to Phase 5. Targets revised with the user and met (keyword 1–12 ms at 100k; semantic ~0.19 s at 10k). Schema v3 drops `embedded_at`; f16 lookup table (load 105→70 ms). Not committed yet: the user commits. |
| 2026-09-29 | Phase 4 ✅ | Capture: `loxx record`, `loxx import`, `loxx init bash|zsh` (bash-preexec 0.7.0 vendored; ignorespace preserved; exit hook for commands interrupted by closing the terminal), background embed trigger. Tested in real bash/zsh in a pty (edge cases, 100-command rapid fire, hangup). Overhead bash +3.9 ms / zsh +1.3 ms (target ≤ 10 ms). Real use: 1 day, 41 commands, 0 duplicates; fixed the tmux/hangup loss, the model-tag version string, and a new-database SQLITE_BUSY race. Not committed yet: the user commits. |
| 2026-09-29 | Phase 5 ✅ | Search panel: typing `loxx` opens an inline Bubbletea v2.0.9 panel (spike: +1.6 MB, ~30 ms to first frame; target ≤ 100 ms); keyword results per keystroke, meaning-based after a 150 ms pause; Tab filters (all/dir-or-repo/session/failed); Enter → zsh pre-filled prompt / bash one ↑ away; never executes. No keyboard shortcut (user: Ctrl-R and candidates are taken by them or by VS Code). Tested in a pty and in real bash/zsh; the user confirmed it works in the VS Code terminal. Not committed yet: the user commits. |

---

## 7. Backlog (unscheduled)
_Ideas that come up mid-phase go here, not into the code._

- **Recency and cwd/git-root boosts** (deferred from Phase 3, moved here from Phase 4 by the user on 2026-09-28): after ~2 weeks of real captured history, build an eval with context (query + cwd + time) and measure the boosts. Ship them only if they beat `DefaultParams`.
- **Minimum similarity for meaning-based results** (found 2026-09-29): nearest-neighbour search always returns *something*, even for gibberish ("zzzqqq" matched a tar command). A cutoff would hide far-off matches, but it changes ranking, so tune it on the eval sets (and a new "should match nothing" set) before shipping.
- Memory at scale: `loxx search` peaks at 537 MB with 100k commands (154 MB f32 vectors + SQLite blob copies while loading; `EmbeddingMatrix` allocates ~470 MB at 100k). Options to measure: stream-decode without per-row copies, or store vectors as f16/int8 (2–4× smaller).
- Memory: `loxx embed` peaks at ~230 MB RSS (f32 weights + the embedded f16 copy + GC headroom). Before the TUI (Phase 5) holds the model for a whole session, try `debug.SetGCPercent`/`SetMemoryLimit` or dropping the f16 bytes after conversion, measured A/B.
- Scrubber gap (found 2026-09-27 on real history): Langfuse secret keys `sk-lf-<uuid>` are only caught next to a telling name (`*_SECRET_KEY=`). Passed bare, the hex+dash value is skipped by the entropy layer. Add a known-token rule for `sk-lf-` (and ask whether `pk-lf-` public keys should be redacted too).
- CI: GitHub says `ubuntu-latest` moves to Ubuntu 26 from 2026-10-19. Static binaries shouldn't care, but check the first CI run after that date.
