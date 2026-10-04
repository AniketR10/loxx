#!/usr/bin/env python3
"""Checks the website in site/ before it is published:

- every page has a title, a description, one <h1>, and (unless it is the 404
  page) a canonical URL matching where it is served;
- every link inside the site points at a page that exists, and every #anchor
  at an id on that page;
- the sitemap lists exactly the pages search engines should index;
- nothing loads from another website (no trackers, fonts or CDNs).

Usage: python3 tools/sitecheck.py [site-dir]
"""
import pathlib
import re
import sys
from html.parser import HTMLParser

BASE = "https://loxx.run"
site = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "site")
errors = []


class Page(HTMLParser):
    def __init__(self):
        super().__init__()
        self.ids, self.links, self.loads = set(), [], []
        self.meta, self.canonical, self.h1, self.title = {}, None, 0, ""
        self._in_title = False

    def handle_starttag(self, tag, attrs):
        a = dict(attrs)
        if "id" in a:
            self.ids.add(a["id"])
        if tag == "a" and "href" in a:
            self.links.append(a["href"])
        if tag == "h1":
            self.h1 += 1
        if tag == "title":
            self._in_title = True
        if tag == "meta":
            key = a.get("name") or a.get("property")
            if key:
                self.meta[key] = a.get("content", "")
        if tag == "link" and a.get("rel") == "canonical":
            self.canonical = a.get("href")
        if tag == "link" and a.get("rel") in ("stylesheet", "icon", "preload"):
            self.loads.append(a.get("href", ""))
        if tag in ("script", "img", "iframe", "video", "audio", "source") and "src" in a:
            self.loads.append(a["src"])

    def handle_endtag(self, tag):
        if tag == "title":
            self._in_title = False

    def handle_data(self, data):
        if self._in_title:
            self.title += data


def url_of(path):
    rel = path.relative_to(site).as_posix()
    return "/" + (rel[: -len("index.html")] if rel.endswith("index.html") else rel)


def file_of(url):
    path = url.split("#")[0].split("?")[0]
    if path.endswith("/"):
        path += "index.html"
    return site / path.lstrip("/")


pages = {url_of(p): p for p in site.rglob("*.html")}
parsed = {}
for url, path in pages.items():
    p = Page()
    p.feed(path.read_text())
    parsed[url] = p

indexable = set()
for url, p in parsed.items():
    where = f"{url}:"
    if not p.title.strip():
        errors.append(f"{where} no <title>")
    if p.h1 != 1:
        errors.append(f"{where} has {p.h1} <h1> (want 1)")
    if "noindex" not in p.meta.get("robots", ""):
        indexable.add(url)
        desc = p.meta.get("description", "")
        if not 50 <= len(desc) <= 170:
            errors.append(f"{where} description is {len(desc)} characters (want 50–170)")
        if p.canonical != BASE + url:
            errors.append(f"{where} canonical is {p.canonical!r}, want {BASE + url!r}")
        if p.meta.get("og:image", "").rsplit("/", 1)[-1] != "og.png":
            errors.append(f"{where} no og:image")
    for src in p.loads:
        if re.match(r"^(https?:)?//", src):
            errors.append(f"{where} loads from another website: {src}")
        elif not file_of(src).is_file():
            errors.append(f"{where} loads a missing file: {src}")
    for href in p.links:
        if href.startswith(("http://", "https://", "mailto:")):
            continue
        target, _, anchor = href.partition("#")
        target_url = url if target == "" else target
        if not target_url.startswith("/"):
            errors.append(f"{where} relative link {href!r}; use a path from the site root")
            continue
        if not file_of(target_url).is_file():
            errors.append(f"{where} broken link {href!r}")
            continue
        page = parsed.get(target_url if target_url.endswith((".html", "/")) else target_url + "/")
        if anchor and page is not None and anchor not in page.ids:
            errors.append(f"{where} link {href!r}: no id {anchor!r} on that page")

sitemap = set(re.findall(r"<loc>(.*?)</loc>", (site / "sitemap.xml").read_text()))
want = {BASE + u for u in indexable}
for missing in sorted(want - sitemap):
    errors.append(f"sitemap.xml: missing {missing}")
for extra in sorted(sitemap - want):
    errors.append(f"sitemap.xml: lists {extra}, which is not an indexable page")

for e in errors:
    print("sitecheck:", e)
print(f"sitecheck: {len(pages)} pages, {len(errors)} problem(s)")
sys.exit(1 if errors else 0)
