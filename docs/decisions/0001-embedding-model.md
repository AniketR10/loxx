# 0001: Embedding model: all-MiniLM-L6-v2

- **Date:** 2026-09-27
- **Status:** Accepted, pending the Phase 2 go/no-go gate on pure-Go speed
- **Decided by:** the user, on Claude's recommendation after a measured bake-off

## Context

`loxx` embeds every unique shell command with a model compiled into the binary
(no Ollama, no network). The user first chose all-MiniLM-L12-v2. Before
writing any Go inference code, we ran a cheap comparison in Python, to avoid
spending a week building the wrong model.

## Method

- **Pool:** 369 unique commands from the user's real `~/.bash_history`
  (990 lines). Each line went through `loxx add`, so the pool is scrubbed
  exactly as `loxx` would store it.
- **Eval set:** 40 natural-language questions, each with the command(s) it
  should find. Claude drafted them from the history, deliberately phrased
  *without* the command's own words (e.g. "sync my fork with the upstream
  main branch" → `git pull upstream main`). Kept local
  (`testdata/eval/`, gitignored) because it contains private work commands.
- **Metrics:** top-1 hit rate, top-5 hit rate, MRR@10.
- **Rankers:** each model semantic-only, and hybrid (semantic + BM25 keyword
  OR, merged with Reciprocal Rank Fusion, k=60). Two keyword-only baselines.
- **Script:** `tools/bakeoff/bakeoff.py` (dev-only).

## Results

| Ranker | Top-1 | Top-5 | MRR | Embed 369 cmds (Python, torch/numpy) |
|---|---|---|---|---|
| keyword AND (Phase 1 `loxx search`) | 0.00 | 0.00 | 0.00 | |
| keyword OR, BM25 | 0.40 | 0.53 | 0.47 | |
| potion-base-8M (model2vec), semantic | 0.40 | 0.75 | 0.53 | 0.03 s |
| potion-base-8M (model2vec), hybrid | 0.40 | 0.80 | 0.55 | |
| **all-MiniLM-L6-v2, semantic** | **0.55** | **0.80** | **0.65** | 2.1 s |
| all-MiniLM-L6-v2, hybrid | 0.53 | 0.78 | 0.63 | |
| all-MiniLM-L12-v2, semantic | 0.57 | 0.75 | 0.65 | 4.4 s |
| all-MiniLM-L12-v2, hybrid | 0.57 | 0.80 | 0.65 | |
| nomic-embed-text (137M, Ollama), semantic | 0.50 | 0.68 | 0.56 | 44 s |
| nomic-embed-text, hybrid | 0.42 | 0.72 | 0.54 | |

With 40 queries each question is 2.5 points: treat differences under
about 0.10 as noise.

Questions **every** model missed (hybrid): "undo the last commit but keep my
changes" (`git reset --soft`), "temporarily shelve my uncommitted changes"
(`git stash`), "bring back the changes i shelved" (`git stash pop`), "apply a
single commit from another branch" (`git cherry-pick`), "which github account
am i logged in as" (`gh auth status`), "wipe the desktop app saved session
data" (`rm -rf` of a desktop app's config directory).

## Decision

**all-MiniLM-L6-v2**, f16 weights in the binary, hand-written pure-Go
inference. It ties L12 on quality at half the compute, and it ranks the right
command first far more often than potion-base-8M (22 vs 16 of 40), which is
what matters when Enter takes the top result. **Fallback:** if pure-Go L6 fails
the speed gate, use potion-base-8M. It matched on top-5 at a tiny fraction of
the cost.

## Learnings

### For loxx

1. **Keyword AND is useless for natural-language queries** (0/40). Phase 3's
   keyword side must be OR-style (or AND for short, as-you-type queries and OR
   for sentences).
2. **Naive RRF fusion did not reliably help.** Hybrid beat semantic-only for
   potion and L12 but slightly hurt L6 and nomic. Phase 3 must tune fusion
   (weights, RRF k, when keyword is allowed to win) against this eval set, not
   assume "hybrid ≥ both".
3. **The remaining misses are knowledge gaps, not model-size gaps.** No
   embedding model knows that "shelve" means `git stash`. This is direct
   evidence for the backlog idea of LLM-written one-line command descriptions
   embedded next to each command.
4. **The eval set is the project's most valuable asset for search quality.**
   Re-run it on every ranking change (Phase 3), and grow it when real searches
   fail.
5. **Scrubbing real history found a gap** that the synthetic corpus missed
   (Langfuse `sk-lf-<uuid>` keys; see the ROADMAP backlog). Real data beats
   invented test cases.

### General (any project choosing an embedding model)

1. **Measure before building.** An hour of Python bake-off replaced a week of
   pure-Go transformer work that would have been half wasted (L12 → L6).
2. **Bigger is not better for short, jargon-heavy text.** The 137M nomic model
   was the worst of the embedding models here, and 22M-parameter L6 tied 33M L12.
   Model-size intuition from long natural-language benchmarks does not transfer
   to shell commands.
3. **Static embeddings (model2vec) are a strong baseline.** Same top-5 recall
   as transformers at microsecond speed. They are weaker only at putting the
   single best match first.
4. **Pick metrics that match the UI.** Top-5 said "tie". Top-1 and MRR showed
   the difference that users would feel, because the UI selects the first
   result by default.
5. **Keep eval data built from personal history out of git** by default, and
   scrub it the same way the product does.
6. **Note the noise floor.** With tens of queries, only trust large gaps. Grow
   the eval set before making fine-grained decisions.

### Performance (pure-Go implementation, added 2026-09-27)

1. **Check the power source before benchmarking a laptop.** On battery, the
   same code ran about 2× slower than an earlier run (probably on AC). Only trust
   A/B comparisons made back-to-back in the same session, repeated.
2. **Hybrid CPUs (performance + efficiency cores) break naive parallelism.**
   Splitting one matrix multiply across 12 threads was *slower* than one
   thread. Independent work still scaled ~6×, so parallelize across items
   (commands), not inside one small multiply.
3. **In scalar Go, more accumulators is not always faster.** 4 was the sweet
   spot; 8 spilled registers and ran 2.8× slower.
4. **Verify the implementation separately from the precision.** Golden tests
   against f32 weights (cosine 1.0000000) proved the math exact, so the f16
   difference (0.9999992) is purely rounding.

