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
| Output (stdout) capture via PTY | **Not in v1.** Backlog only. |
| Database | **SQLite** (not DuckDB) |
| CLI library | **stdlib `flag`** + a small hand-written subcommand dispatcher. No cobra: keeps `loc record` lean and the dependency count low. (User asked for a recommendation and accepted it, 2026-09-26) |
| Directory layout | As listed in Phase 0 (approved 2026-09-26). Directories are created when first needed, not up front. |
| GitHub repo | **Private** for now: `github.com/AniketR10/loc` |
| LICENSE file | **The user adds it themselves.** Claude must not create or edit `LICENSE`. |
| Go version | `go 1.25.0` minimum, `toolchain go1.25.12` (the locally installed version) |
| Linting | `go vet` + `staticcheck` **2026.1**, pinned. 2026.2+ requires Go 1.26 and would silently download a second toolchain. |
| Build tool | `Makefile` (`build`, `test`, `lint`, `bench`, `check-static`, `clean`) |
| CI | GitHub Actions: `actions/checkout@v7`, `actions/setup-go@v7` (latest releases as of 2026-09-26) |

### PROPOSED (confirm before the phase that uses it)
| Topic | Proposal | Needed by |
|---|---|---|
| SQLite driver | `modernc.org/sqlite` (pure Go, FTS5) | Phase 1 |
| Vector search | Brute-force cosine in Go over float32 blobs (no sqlite-vec) | Phase 3 |
| Embedding model | model2vec `potion-base-8M` (verify license, size, quality) | Phase 2 |
| Search ranking | FTS5 BM25 + vector → Reciprocal Rank Fusion → recency/frequency/cwd boosts | Phase 3 |
| TUI | Bubbletea + Bubbles + Lipgloss | Phase 5 |
| Config format / location | TOML at `$XDG_CONFIG_HOME/loc/config.toml` | Phase 6 |
| Data location | `$XDG_DATA_HOME/loc/history.db` (default `~/.local/share/loc/`) | Phase 1 |
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
- [ ] `LICENSE`: **the user adds it themselves** (do not create it)
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
- [ ] Test corpus in `testdata/secrets/` with positive examples (must redact) and negative examples (must NOT redact: git SHAs, UUIDs, docker digests, base64 file names…)
- [ ] Layer 1: skip commands that start with a space (ignorespace convention)
- [ ] Layer 2: known token patterns (AWS, GitHub, GitLab, Slack, Stripe, JWT, private-key blocks, Google API keys…). Check the license of any ruleset we borrow from (e.g. gitleaks is MIT; verify).
- [ ] Layer 3: structural rules (`PASSWORD=…`, `*_TOKEN=…`, `--password[= ]…`, `mysql -p…`, `Authorization: Bearer …`, `scheme://user:pass@host`, `-H 'X-Api-Key: …'`)
- [ ] Layer 4: high-entropy fallback with an allowlist (hex hashes, UUIDs)
- [ ] Replacement format: `<REDACTED:kind>`, so the command stays readable
- [ ] Record whether redaction happened (a count/flag) without storing the secret
- [ ] Fuzz test: the scrubber never panics, and its output never contains a corpus secret

**Tasks: Storage**
- [ ] Schema v1 (PROPOSED, confirm):
  - `commands(id, text UNIQUE, first_seen, last_seen, run_count, redacted, embedded_at NULL)`
  - `executions(id, command_id, cwd, git_root NULL, exit_code, duration_ms, started_at, session_id, hostname, source['live'|'import'])`
  - `commands_fts`: FTS5 over `commands.text`
  - `embeddings(command_id, model_id, dims, vector BLOB)`
  - `meta(key, value)`: schema version, active model id
- [ ] Migrations framework (versioned, forward-only)
- [ ] WAL mode, busy timeout, DB file permissions `0600`, data dir permissions `0700`
- [ ] Concurrency test: N processes writing at once without errors (simulates many terminal tabs)
- [ ] CLI: `loc add "<cmd>" [--cwd --exit]`: scrub → upsert command → insert execution
- [ ] CLI: `loc search "<query>"`: FTS5-only for now, plain-text output

**Exit criteria**
- [ ] All positive corpus secrets are redacted and all negative items are left intact (100%, not "mostly")
- [ ] A grep of the DB file for every corpus secret finds nothing
- [ ] 20 concurrent writers × 500 inserts: 0 errors
- [ ] `loc add` then `loc search` round-trips correctly

**Out of scope:** embeddings, hooks, TUI.

---

### Phase 2: Embedding engine (the riskiest technical piece: prove it early)
**Goal:** Pure-Go embeddings compiled into the binary, with measured quality.

**Tasks**
- [ ] Verify the chosen model's license, file size, dimensions and tokenizer type. Record them in §3.
- [ ] Port the model2vec inference to pure Go: tokenizer → token-id lookup → mean pooling → normalize
- [ ] **Golden-vector test:** for about 50 strings, Go output must match the Python reference (`model2vec` package) within a set tolerance. Generate the goldens once and commit them to `testdata/`.
- [ ] Compile the model into the binary with `go:embed`. Record the binary-size delta.
- [ ] `loc embed --pending`: embed commands with `embedded_at IS NULL` in batches, guarded by a lockfile
- [ ] Store `model_id`, so a future model change triggers a re-embed instead of mixing vectors
- [ ] **Eval harness:** `testdata/eval/queries.jsonl` with about 30 natural-language queries and their expected commands, **written by the user from their real history** (**ask the user to provide them**). Metrics: recall@5 and MRR.
- [ ] Baseline comparison: the same eval using Ollama `nomic-embed-text` (dev only, optional), to know how much quality we give up

**Exit criteria**
- [ ] Golden tests pass
- [ ] Embedding speed per command and model load time are measured and recorded
- [ ] Eval numbers are recorded for semantic-only search
- [ ] **Go/no-go gate (decided with the user):** is the quality acceptable? If not, choose one of:
  a) a larger or different static model,
  b) a transformer model in pure Go (hugot or hand-written MiniLM),
  c) optional Ollama backend promoted into v1.

  Record the decision in §3.

**Out of scope:** ranking fusion, hooks, TUI.

---

### Phase 3: Hybrid search
**Goal:** Search results that beat both keyword-only and semantic-only search.

**Tasks**
- [ ] Vector search: load vectors and compute brute-force cosine. Benchmark at 10k / 100k / 500k rows.
- [ ] FTS5 BM25 search (tokenizer config that handles `-`, `/`, `.`, `:` in commands)
- [ ] Reciprocal Rank Fusion of the two lists
- [ ] Boosts: recency, run_count, current dir / git root match, exit-code filter
- [ ] Fallback (P7): no vectors → FTS only, with no error shown to the user
- [ ] `loc search` output: command, dir, relative time, exit status, run count
- [ ] Re-run the eval: keyword-only vs semantic-only vs hybrid

**Exit criteria**
- [ ] Hybrid ≥ the better of the two single methods on recall@5 in the eval (numbers recorded)
- [ ] Search latency at 100k commands is measured and recorded. Target: < 100 ms end-to-end, **to be confirmed**.

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
- LLM-generated one-line descriptions of each unique command (needs Ollama or a bundled LLM)
- Better in-process model (transformer via pure Go)
- Opt-in output capture: `loc run -- <cmd>`, and/or terminal integration via OSC 133
- `loc stats` (most used, most failed, per-project)
- Per-directory / per-project history views
- Encrypted-at-rest database
- Export/import (JSON) for moving machines

---

## 6. Progress log

**Current phase: Phase 1 (not started)**. Before starting, confirm these §3 PROPOSED items: SQLite driver, data location, schema v1.

| Date | Phase | What happened / evidence |
|---|---|---|
| 2026-09-26 | Planning | Brief reviewed; architecture revised (hybrid search, dedup, no daemon, no PTY, in-process embeddings, Go). Decisions locked (§3). Roadmap created. |
| 2026-09-26 | Phase 0 ✅ | Private repo `AniketR10/loc` created. Local: vet + staticcheck + tests pass; `bin/loc` is statically linked (`ldd`: not a dynamic executable), 1.5 MB. First CI run passed: https://github.com/AniketR10/loc/actions/runs/36258401581. LICENSE is still pending (the user is adding it). |

---

## 7. Backlog (unscheduled)
_Ideas that come up mid-phase go here, not into the code._

- CI: GitHub says `ubuntu-latest` moves to Ubuntu 26 from 2026-10-19. Static binaries shouldn't care, but check the first CI run after that date.
