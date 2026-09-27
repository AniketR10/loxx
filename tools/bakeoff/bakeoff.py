#!/usr/bin/env python3
"""Dev-only Phase 2 bake-off: compare embedding models on a local eval set.

Every model ranks the same candidate pool (the unique commands in a loc
history database) for each eval query. Each model is scored twice:
semantic-only, and hybrid (semantic + keyword merged with Reciprocal Rank
Fusion), since hybrid is what loc will ship.

Usage:
  bakeoff.py --db HISTORY_DB --queries QUERIES_JSONL --l12 PATH_TO_L12_DIR

Needs: sentence-transformers, model2vec, numpy; Ollama running with
nomic-embed-text pulled (skip with --no-ollama).
"""

import argparse
import json
import re
import sqlite3
import time
import urllib.request

import numpy as np

TOP_K = 10
RRF_K = 60


def load(db_path, queries_path):
    texts = [t for (t,) in sqlite3.connect(db_path).execute("SELECT text FROM commands ORDER BY id")]
    index = {t: i for i, t in enumerate(texts)}
    queries = []
    with open(queries_path) as f:
        for line in f:
            if not line.strip():
                continue
            q = json.loads(line)
            missing = [e for e in q["expected"] if e not in index]
            if missing:
                raise SystemExit(f"expected command not in pool for {q['query']!r}: {missing}")
            queries.append((q["query"], {index[e] for e in q["expected"]}))
    return texts, queries


def words(text):
    # Same split as loc's ftsQuery: runs of letters and digits.
    return re.findall(r"[^\W_]+", text)


def keyword_ranker(texts, joiner):
    """BM25 over FTS5. joiner " AND " mirrors today's `loc search`; " OR " is
    a candidate keyword side for hybrid search."""
    con = sqlite3.connect(":memory:")
    con.execute("CREATE VIRTUAL TABLE f USING fts5(text)")
    con.executemany("INSERT INTO f(rowid, text) VALUES (?, ?)", enumerate(texts))

    def rank(query, k):
        ws = words(query)
        if not ws:
            return []
        expr = joiner.join(f'"{w}"*' for w in ws)
        return [r for (r,) in con.execute("SELECT rowid FROM f WHERE f MATCH ? ORDER BY bm25(f) LIMIT ?", (expr, k))]

    return rank


def dense_ranker(doc_vecs, embed_query):
    def rank(query, k):
        scores = doc_vecs @ embed_query(query)
        return list(np.argsort(-scores)[:k])

    return rank


def rrf(*rankers):
    def rank(query, k):
        scores = {}
        for r in rankers:
            for pos, idx in enumerate(r(query, 50)):
                scores[idx] = scores.get(idx, 0.0) + 1.0 / (RRF_K + pos + 1)
        return sorted(scores, key=lambda i: -scores[i])[:k]

    return rank


def evaluate(rank, queries):
    r1 = r5 = mrr = 0.0
    misses = []
    for query, expected in queries:
        got = rank(query, TOP_K)
        pos = next((i for i, idx in enumerate(got) if idx in expected), None)
        if pos is None:
            misses.append(query)
            continue
        mrr += 1.0 / (pos + 1)
        r1 += pos == 0
        r5 += pos < 5
        if pos >= 5:
            misses.append(query)
    n = len(queries)
    return r1 / n, r5 / n, mrr / n, misses


def normalize(m):
    m = np.asarray(m, dtype=np.float32)
    return m / np.linalg.norm(m, axis=-1, keepdims=True)


def models(args):
    """Yields (name, embed_docs, embed_query) lazily so each model loads once."""
    from model2vec import StaticModel
    from sentence_transformers import SentenceTransformer

    potion = StaticModel.from_pretrained("minishlab/potion-base-8M")
    yield "potion-base-8M (model2vec)", lambda t: normalize(potion.encode(t)), lambda q: normalize(potion.encode([q]))[0]

    for name, path in [("all-MiniLM-L6-v2", "sentence-transformers/all-MiniLM-L6-v2"), ("all-MiniLM-L12-v2", args.l12)]:
        st = SentenceTransformer(path, device="cpu")
        yield name, (lambda st: lambda t: st.encode(t, normalize_embeddings=True, batch_size=64))(st), \
            (lambda st: lambda q: st.encode([q], normalize_embeddings=True)[0])(st)

    if not args.no_ollama:
        def ollama(inputs):
            req = urllib.request.Request(
                args.ollama + "/api/embed",
                data=json.dumps({"model": "nomic-embed-text", "input": inputs}).encode(),
                headers={"Content-Type": "application/json"},
            )
            with urllib.request.urlopen(req) as resp:
                return normalize(json.load(resp)["embeddings"])

        yield "nomic-embed-text (Ollama, reference)", \
            lambda t: ollama(["search_document: " + x for x in t]), \
            lambda q: ollama(["search_query: " + q])[0]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", required=True)
    ap.add_argument("--queries", required=True)
    ap.add_argument("--l12", required=True, help="local all-MiniLM-L12-v2 directory")
    ap.add_argument("--ollama", default="http://localhost:11434")
    ap.add_argument("--no-ollama", action="store_true")
    args = ap.parse_args()

    texts, queries = load(args.db, args.queries)
    print(f"pool: {len(texts)} unique commands, {len(queries)} queries\n")

    kw_and = keyword_ranker(texts, " AND ")
    kw_or = keyword_ranker(texts, " OR ")
    rows = [("keyword AND (today's loc search)", *evaluate(kw_and, queries), None),
            ("keyword OR (bm25)", *evaluate(kw_or, queries), None)]
    misses = {}
    for name, embed_docs, embed_query in models(args):
        start = time.perf_counter()
        docs = embed_docs(texts)
        elapsed = time.perf_counter() - start
        sem = dense_ranker(docs, embed_query)
        s = evaluate(sem, queries)
        h = evaluate(rrf(sem, kw_or), queries)
        rows.append((name + " / semantic", *s, elapsed))
        rows.append((name + " / hybrid", *h, None))
        misses[name] = h[3]

    print(f"{'ranker':<50} {'R@1':>5} {'R@5':>5} {'MRR':>5}  pool-embed-time")
    for name, r1, r5, mrr, _, t in rows:
        tstr = f"{t:.2f}s (python)" if t is not None else ""
        print(f"{name:<50} {r1:5.2f} {r5:5.2f} {mrr:5.2f}  {tstr}")
    print("\nhybrid misses (expected command not in top 5):")
    for name, ms in misses.items():
        print(f"  {name}: {len(ms)}")
        for q in ms:
            print(f"    - {q}")


if __name__ == "__main__":
    main()
