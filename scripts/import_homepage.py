#!/usr/bin/env python3
"""Import bookmarks from a gethomepage Homepage instance into data/*.md.

Uses GET /api/bookmarks (group name + href/name entries).
Merges into existing Markdown: skip duplicate URLs; create categories from
Homepage groups; never deletes existing bookmarks.
"""

from __future__ import annotations

import json
import os
import re
import sys
import uuid
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlparse
from urllib.request import Request, urlopen

DEFAULT_BASE = "http://podman.local.weimann.us:3000"
ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "data"
SAVED = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

ENTRY_RE = re.compile(
    r"^## (?P<title>.+)\n"
    r"id: (?P<id>\S+)\n"
    r"url: (?P<url>.+)\n"
    r"source: (?P<source>.*)\n"
    r"saved: (?P<saved>\S+)\n",
    re.M,
)
SLUG_RE = re.compile(r"[^a-z0-9]+")


def slugify(title: str) -> str:
    s = SLUG_RE.sub("-", title.lower().strip()).strip("-")
    return s or "category"


def norm_url(url: str) -> str:
    u = url.strip()
    try:
        p = urlparse(u)
    except Exception:
        return u.lower()
    scheme = (p.scheme or "").lower()
    h = (p.hostname or "").lower()
    if h.startswith("www."):
        h = h[4:]
    path = p.path or ""
    if path != "/" and path.endswith("/"):
        path = path[:-1]
    # Keep userinfo for ssh://user@host
    user = ""
    if p.username:
        user = p.username
        if p.password:
            user += ":" + p.password
        user += "@"
    netloc = f"{user}{h}"
    if p.port:
        netloc += f":{p.port}"
    q = ("?" + p.query) if p.query else ""
    if scheme:
        return f"{scheme}://{netloc}{path}{q}"
    return u.lower()


def parse_md_file(path: Path) -> tuple[str, list[dict]]:
    if not path.exists():
        return path.stem, []
    text = path.read_text(encoding="utf-8")
    title = path.stem
    for line in text.splitlines():
        if line.startswith("# "):
            title = line[2:].strip()
            break
    bookmarks = []
    for m in ENTRY_RE.finditer(text):
        bookmarks.append(
            {
                "id": m.group("id"),
                "title": m.group("title").strip(),
                "url": m.group("url").strip(),
                "source": m.group("source").strip(),
                "saved": m.group("saved").strip(),
            }
        )
    return title, bookmarks


def write_md(path: Path, title: str, bookmarks: list[dict]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    lines = [f"# {title}", ""]
    for b in bookmarks:
        lines.extend(
            [
                f"## {b['title']}",
                f"id: {b['id']}",
                f"url: {b['url']}",
                f"source: {b['source']}",
                f"saved: {b['saved']}",
                "",
            ]
        )
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text("\n".join(lines) + "\n", encoding="utf-8")
    os.replace(tmp, path)


def load_all() -> tuple[dict[str, list[dict]], dict[str, str], set[str]]:
    buckets: dict[str, list[dict]] = defaultdict(list)
    titles: dict[str, str] = {"inbox": "Inbox"}
    urls: set[str] = set()

    t, items = parse_md_file(DATA / "inbox.md")
    titles["inbox"] = t or "Inbox"
    buckets["inbox"] = items
    for b in items:
        urls.add(norm_url(b["url"]))

    cat_dir = DATA / "categories"
    if cat_dir.is_dir():
        for p in sorted(cat_dir.glob("*.md")):
            slug = p.stem
            t, items = parse_md_file(p)
            titles[slug] = t
            buckets[slug] = items
            for b in items:
                urls.add(norm_url(b["url"]))
    return buckets, titles, urls


def fetch_bookmarks(base: str) -> list[dict]:
    url = base.rstrip("/") + "/api/bookmarks"
    req = Request(url, headers={"User-Agent": "BookmarkerImporter/1.0"})
    with urlopen(req, timeout=30) as resp:
        return json.loads(resp.read().decode("utf-8"))


def main() -> int:
    base = DEFAULT_BASE
    for i, a in enumerate(sys.argv[1:], 1):
        if a == "--base" and i < len(sys.argv) - 1:
            base = sys.argv[i + 1]
        elif a.startswith("http://") or a.startswith("https://"):
            base = a

    cache = ROOT / "homepage-bookmarks.json"
    if cache.exists() and "--refetch" not in sys.argv and "--base" not in sys.argv:
        # Still refetch by default for Homepage (local, fast); cache only if --cache
        pass
    print(f"fetching {base.rstrip('/')}/api/bookmarks")
    groups = fetch_bookmarks(base)
    cache.write_text(json.dumps(groups, indent=2), encoding="utf-8")

    fetched = 0
    for g in groups:
        fetched += len(g.get("bookmarks") or [])
    print(f"fetched: {fetched} bookmarks in {len(groups)} groups")

    buckets, titles, existing = load_all()
    before = sum(len(v) for v in buckets.values())
    print(f"existing local bookmarks: {before}")

    added = 0
    skipped_dup = 0
    skipped_empty = 0
    touched: Counter[str] = Counter()
    created_slugs: set[str] = set()

    for g in groups:
        group_name = (g.get("name") or "Homepage").strip()
        slug = slugify(group_name)
        if slug not in titles:
            titles[slug] = group_name
            created_slugs.add(slug)
        elif slug not in buckets:
            titles[slug] = group_name

        for b in g.get("bookmarks") or []:
            href = (b.get("href") or "").strip()
            name = (b.get("name") or "").strip() or href
            if not href:
                skipped_empty += 1
                continue
            key = norm_url(href)
            if key in existing:
                skipped_dup += 1
                continue
            bm = {
                "id": str(uuid.uuid4()),
                "title": name,
                "url": href,
                "source": f"homepage:{group_name}",
                "saved": SAVED,
            }
            buckets[slug].append(bm)
            existing.add(key)
            added += 1
            touched[slug] += 1

    write_md(DATA / "inbox.md", titles.get("inbox", "Inbox"), buckets.get("inbox", []))
    for slug, items in sorted(buckets.items()):
        if slug == "inbox":
            continue
        write_md(
            DATA / "categories" / f"{slug}.md",
            titles.get(slug, slug),
            items,
        )

    print(f"newly added: {added}")
    print(f"skipped duplicates: {skipped_dup}")
    print(f"skipped empty: {skipped_empty}")
    print(f"categories created: {len(created_slugs)}")
    if created_slugs:
        print("  " + ", ".join(sorted(created_slugs)))
    print("categories touched (new items):")
    for slug, n in touched.most_common():
        print(f"  {n:3}  {slug} ({titles.get(slug, slug)})")
    print(f"total bookmarks now: {sum(len(v) for v in buckets.values())}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
