# Contributing

Thanks for helping. This page explains how to build, test and change loxx.

## What you need

Go 1.25 or newer, `make`, `curl`, bash and zsh. On a Mac, the tests skip
bash, which loxx doesn't support there.

## Build and test

```
make build   # builds bin/loxx
make test    # runs all tests, including real bash and zsh sessions
make lint    # checks the code for common mistakes
```

The first build downloads the search model (about 45 MB) and checks it is the
right file. The model is not stored in git.

More checks, if you need them:

| Command | What it checks |
|---|---|
| `make fuzz` | Throws random input at the secret remover for a minute. |
| `make dist` | Builds the files for a release into `dist/`. |
| `sh tools/installtest/run.sh` | Installs, uses and removes loxx on a clean Fedora (needs podman). |
| `make dist && LOXX_RELEASE_URL=file://$PWD/dist sh tools/installtest/macos.sh` | The same for a new Mac user, in a temporary home folder. CI runs it on GitHub's Macs. |
| `LOXX_BENCH_HOOKS=1 go test ./cmd/loxx -run TestHookOverhead -v` | How much loxx slows the terminal. |
| `go run ./tools/eval --db DB --queries FILE` | How good search results are (see [docs/decisions](docs/decisions/)). |

## Rules every change must follow

1. Never slow down the terminal. Saving happens in the background.
2. Never save a secret. Everything saved goes through `internal/scrub`.
3. Never use the internet.
4. Stay one single file with no extra dependencies (`CGO_ENABLED=0`; checked
   by `tools/checkstatic.sh`).
5. Never run a command for the user.
6. `loxx uninstall` must undo everything `loxx setup` did, exactly.
7. Search must still work (by keyword) if the search model isn't ready.
8. Only use code with licenses compatible with MIT.

If you change how search ranks results, show the search quality numbers from
`tools/eval` before and after your change.

## Teaching loxx a new kind of secret

1. Add made-up examples to `testdata/secrets/positive.jsonl`.
2. Add a look-alike that must **not** be hidden to
   `testdata/secrets/negative.jsonl`.
3. Run `make test fuzz`.

Never commit a real secret, not even an old one.

## How it works, briefly

- A small hook in your shell calls `loxx record` in the background after each
  command. (bash needs [bash-preexec](https://github.com/rcaloras/bash-preexec)
  for this; it is included.)
- Commands are cleaned of secrets (`internal/scrub`) and saved in SQLite
  (`internal/store`).
- Search combines exact keyword matching with a small built-in language model,
  [all-MiniLM-L6-v2](https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2),
  that understands meaning (`internal/embed`, `internal/search`).
- Why things were built this way: [docs/decisions](docs/decisions/) and
  [ROADMAP.md](ROADMAP.md).
