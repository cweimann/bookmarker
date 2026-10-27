#!/usr/bin/env python3
"""One-shot import of http://www.weimann.us/showlinks.cgi into data/*.md.

Merges with existing Markdown bookmarks (dedupe by normalized URL).
Does not delete anything already present.
"""

from __future__ import annotations

import html
import os
import re
import sys
import uuid
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlparse
from urllib.request import Request, urlopen

CGI_URL = "http://www.weimann.us/showlinks.cgi"
ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "data"
SAVED = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

ROW_RE = re.compile(
    r"<tr>\s*<td>\s*([^<]*?)\s*<td>\s*<a\s+href=([^\s>]+)>(.*?)</a>",
    re.I | re.S,
)
ENTRY_RE = re.compile(
    r"^## (?P<title>.+)\n"
    r"id: (?P<id>\S+)\n"
    r"url: (?P<url>.+)\n"
    r"source: (?P<source>.*)\n"
    r"saved: (?P<saved>\S+)\n",
    re.M,
)

# (slug, title, predicates) — first match wins. Unmatched → inbox.
RULES: list[tuple[str, str, list]] = []


def host(url: str) -> str:
    try:
        h = urlparse(url).hostname or ""
    except Exception:
        return ""
    h = h.lower()
    if h.startswith("www."):
        h = h[4:]
    return h


def norm_url(url: str) -> str:
    u = url.strip()
    try:
        p = urlparse(u)
    except Exception:
        return u.lower()
    scheme = (p.scheme or "http").lower()
    h = (p.hostname or "").lower()
    if h.startswith("www."):
        h = h[4:]
    path = p.path or ""
    if path != "/" and path.endswith("/"):
        path = path[:-1]
    q = ("?" + p.query) if p.query else ""
    return f"{scheme}://{h}{path}{q}"


def text_blob(title: str, url: str) -> str:
    return f"{title} {url}".lower()


def rule(slug: str, title: str, *preds):
    RULES.append((slug, title, list(preds)))


def host_in(*hosts):
    hs = {h.lower() for h in hosts}

    def pred(t, u):
        return host(u) in hs or any(host(u).endswith("." + h) for h in hs)

    return pred


def host_has(*parts):
    parts = [p.lower() for p in parts]

    def pred(t, u):
        h = host(u)
        return any(p in h for p in parts)

    return pred


def any_text(*needles):
    needles = [n.lower() for n in needles]

    def pred(t, u):
        b = text_blob(t, u)
        return any(n in b for n in needles)

    return pred


# Order matters: more specific first.
rule(
    "freebsd",
    "FreeBSD",
    host_has("freebsd", "freshports", "freenas", "trueNAS".lower(), "bsdnas", "vermaden", "daemonless", "webzfs", "freecore", "eurobsd"),
    host_in("dan.langille.org", "blog.feld.me", "blog.hofstede.it", "hayzam.com", "freebsdfoundation.org", "freebsdsoftware.org"),
    any_text("freebsd", "freeBSD", "zfs on linux", "bhyve", "pkgng", "poudriere", "rc.d", "netgraph", "linuxulator"),
)
rule(
    "zfs-storage",
    "ZFS & Storage",
    any_text("zfs", "openzfs", "truenas", "freenas", "nas ", "storage"),
    host_in("servethehome.com", "klarasystems.com"),
)
rule(
    "python",
    "Python",
    host_has("python", "realpython", "pycon"),
    host_in("planetpython.org"),
    any_text("python", "django", "flask", "pytest"),
)
rule(
    "go",
    "Go",
    host_in(
        "go.dev",
        "golang.org",
        "pkg.go.dev",
        "gobyexample.com",
        "blog.gopheracademy.com",
        "swtch.com",
        "go.dev",
    ),
    host_has("gopher"),
    any_text("golang", "gophercon", "go module", "goroutine"),
)
rule(
    "github-dev",
    "GitHub & Dev Tools",
    host_in(
        "github.com",
        "gist.github.com",
        "gitlab.com",
        "bitbucket.org",
        "code.google.com",
        "stackoverflow.com",
        "serverfault.com",
        "stackexchange.com",
        "hg-git.github.io",
        "hacker-laws.com",
        "app.programmingfonts.org",
    ),
    any_text("git-guides", "stackoverflow", "serverfault", "stackexchange"),
)
rule(
    "networking",
    "Networking",
    any_text(
        "network",
        "tcp ",
        "udp ",
        "bgp",
        "dns ",
        "wireguard",
        "vpn",
        "pfSense",
        "pfsense",
        "netgraph",
        "firewall",
        "nat ",
        "openwrt",
        "ubiquiti",
        "unifi",
        "chrony",
    ),
    host_in(
        "fasterdata.es.net",
        "serverfault.com",
        "wiki.openwrt.org",
        "lists.openwrt.org",
        "ubntwiki.com",
        "chrony.tuxfamily.org",
    ),
    host_has("openwrt", "ubnt", "unifi"),
)
rule(
    "email",
    "Email & Mail",
    any_text("email", "mail server", "postfix", "dovecot", "smtp", "imap", "bsdmail", "rspamd"),
)
rule(
    "firearms",
    "Firearms",
    host_has("firearm", "gun", "recoil", "ar15", "cz-", "cajun"),
    host_in("thefirearmblog.com", "langdontactical.com", "cajungunworks.com"),
    any_text("shotgun", "rifle", "pistol", "nocturne", " holster", "ammo"),
)
rule(
    "news-politics",
    "News & Politics",
    host_in(
        "zerohedge.com",
        "mises.org",
        "thepostmillennial.com",
        "theverge.com",
        "arstechnica.com",
        "notbeinggoverned.com",
        "fee.org",
        "cato.org",
        "econlib.org",
        "lysanderspooner.org",
        "lifehacker.com",
    ),
    host_has("reuters", "nytimes", "washingtonpost", "bbc.", "npr.org"),
    any_text("politics", "libertarian", "austrian economics"),
)
rule(
    "reading",
    "Reading & Essays",
    host_in("medium.com", "blog.plover.com", "ferd.ca", "garbagecollected.org", "blog.jooq.org"),
    host_has("medium.com", "substack.com"),
)
rule(
    "microsoft-windows",
    "Microsoft & Windows",
    host_has("microsoft", "windows", "azure", "office.com"),
    host_in("support.microsoft.com", "learn.microsoft.com", "docs.microsoft.com"),
)
rule(
    "linux",
    "Linux",
    host_has("linux", "ubuntu", "debian", "archlinux", "kernel.org", "redhat"),
    any_text("linux ", "ubuntu", "debian", "systemd"),
)
rule(
    "comics-fun",
    "Comics & Fun",
    host_in("xkcd.com", "clientsfromhell.net", "todayifoundout.com", "imgur.com", "i.imgur.com", "i.redd.it"),
    any_text("xkcd", "clients from hell", "today i found out"),
)
rule(
    "reddit",
    "Reddit",
    host_in("reddit.com", "old.reddit.com", "i.redd.it"),
)
rule(
    "wikipedia",
    "Wikipedia",
    host_has("wikipedia.org", "wikimedia"),
)
rule(
    "shopping",
    "Shopping",
    host_has("amazon.", "smile.amazon", "ebay.", "newegg", "fastenal", "metalsdepot"),
    host_in("fastenal.com", "metalsdepot.com", "homestretchfurniture.com", "lanefurniture.com", "thesweethome.com"),
)
rule(
    "media-video",
    "Media & Video",
    host_has("youtube", "vimeo", "exquisite.tube"),
    any_text("youtube", "video"),
)
rule(
    "security",
    "Security",
    any_text("security", "password", "oauth", "tls", "ssl", "cve-", "rustdesk", "vpn"),
    host_has("schneier", "krebsonsecurity"),
)
rule(
    "postgres",
    "PostgreSQL",
    host_has("postgres", "postgresql"),
    any_text("postgres", "postgresql"),
)
rule(
    "ships-marine",
    "Ships & Marine",
    host_has("marinevessel", "marinetraffic", "vesselfinder"),
    any_text("ship-owner", "vessel", "ais "),
)


def categorize(title: str, url: str) -> str | None:
    for slug, _title, preds in RULES:
        for p in preds:
            if p(title, url):
                return slug
    return None  # inbox


def cat_title(slug: str) -> str:
    for s, t, _ in RULES:
        if s == slug:
            return t
    return slug


def parse_cgi(html_text: str) -> list[dict]:
    out = []
    seen = set()
    for m in ROW_RE.finditer(html_text):
        src = html.unescape(m.group(1)).strip() or "showlinks"
        url = html.unescape(m.group(2).strip().strip('"').strip("'"))
        title = re.sub(
            r"\s+",
            " ",
            html.unescape(re.sub(r"<[^>]+>", "", m.group(3))),
        ).strip()
        if not url or not url.lower().startswith(("http://", "https://")):
            continue
        key = norm_url(url)
        if key in seen:
            continue
        seen.add(key)
        if not title:
            title = url
        out.append({"title": title, "url": url, "source": src})
    return out


def parse_md_file(path: Path) -> tuple[str, list[dict]]:
    if not path.exists():
        return path.stem.replace("-", " ").title(), []
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
    tmp.write_text("\n".join(lines) + ("\n" if lines else ""), encoding="utf-8")
    os.replace(tmp, path)


def is_local_bookmark(b: dict) -> bool:
    """Keep bookmarks Christopher added locally (not a prior showlinks import)."""
    src = (b.get("source") or "").lower()
    return not src.startswith("showlinks:")


def load_existing(keep_imported: bool) -> tuple[dict[str, list[dict]], dict[str, str], set[str]]:
    """Returns buckets{slug|inbox}, titles, existing_norm_urls."""
    buckets: dict[str, list[dict]] = defaultdict(list)
    titles: dict[str, str] = {"inbox": "Inbox"}
    urls: set[str] = set()

    def take(slug: str, title: str, items: list[dict]) -> None:
        kept = items if keep_imported else [b for b in items if is_local_bookmark(b)]
        titles[slug] = title
        buckets[slug] = kept
        for b in kept:
            urls.add(norm_url(b["url"]))

    inbox_path = DATA / "inbox.md"
    t, items = parse_md_file(inbox_path)
    take("inbox", t or "Inbox", items)

    cat_dir = DATA / "categories"
    if cat_dir.is_dir():
        for p in sorted(cat_dir.glob("*.md")):
            slug = p.stem
            t, items = parse_md_file(p)
            take(slug, t, items)
            # Drop empty category files when re-importing (except keep slug in titles if local items remain)
            if not keep_imported and not buckets[slug]:
                del buckets[slug]
                # keep human title only if file had local items; otherwise allow rewrite
                if slug in titles and slug not in buckets:
                    del titles[slug]
    return buckets, titles, urls


def fetch(url: str) -> str:
    req = Request(url, headers={"User-Agent": "BookmarkerImporter/1.0"})
    with urlopen(req, timeout=60) as resp:
        return resp.read().decode("utf-8", errors="replace")


def main() -> int:
    if (DATA / ".migrated-to-tree").exists():
        print(
            "data/ uses the folder-tree layout; this importer still writes the old "
            "data/categories/ layout, which the server no longer reads. Update it first.",
            file=sys.stderr,
        )
        return 2
    cache = ROOT / "old-cgi.html"
    if cache.exists() and "--refetch" not in sys.argv:
        html_text = cache.read_text(encoding="utf-8", errors="replace")
        print(f"using cache {cache} ({len(html_text)} bytes)")
    else:
        print(f"fetching {CGI_URL}")
        html_text = fetch(CGI_URL)
        cache.write_text(html_text, encoding="utf-8")

    incoming = parse_cgi(html_text)
    print(f"parsed {len(incoming)} unique http(s) links from CGI")

    keep_imported = "--keep-imported" in sys.argv
    buckets, titles, existing = load_existing(keep_imported=keep_imported)
    preserved = sum(len(v) for v in buckets.values())
    print(f"preserved bookmarks (local{' + prior import' if keep_imported else ' only'}): {preserved}")

    # Remove empty category files left from a previous import when refreshing.
    if not keep_imported:
        cat_dir = DATA / "categories"
        if cat_dir.is_dir():
            for p in cat_dir.glob("*.md"):
                if p.stem not in buckets:
                    p.unlink()
                    print(f"removed stale category file {p.name}")


    imported = 0
    skipped_dup = 0
    skipped_bad = 0
    cat_counts: Counter[str] = Counter()

    for row in incoming:
        key = norm_url(row["url"])
        if key in existing:
            skipped_dup += 1
            continue
        slug = categorize(row["title"], row["url"])
        dest = slug or "inbox"
        if slug and dest not in titles:
            titles[dest] = cat_title(slug)
        bm = {
            "id": str(uuid.uuid4()),
            "title": row["title"],
            "url": row["url"],
            "source": f"showlinks:{row['source']}",
            "saved": SAVED,
        }
        buckets[dest].append(bm)
        existing.add(key)
        imported += 1
        cat_counts[dest] += 1

    # Ensure category titles for rules we used
    for slug, title, _ in RULES:
        if slug in buckets and slug not in titles:
            titles[slug] = title

    write_md(DATA / "inbox.md", titles.get("inbox", "Inbox"), buckets.get("inbox", []))
    for slug, items in sorted(buckets.items()):
        if slug == "inbox":
            continue
        write_md(
            DATA / "categories" / f"{slug}.md",
            titles.get(slug, cat_title(slug)),
            items,
        )

    print(f"imported: {imported}")
    print(f"skipped duplicates: {skipped_dup}")
    print(f"skipped bad: {skipped_bad}")
    print("by destination:")
    for k, c in cat_counts.most_common():
        label = titles.get(k, k)
        print(f"  {c:4}  {k} ({label})")
    print(f"inbox total now: {len(buckets.get('inbox', []))}")
    print(f"categories on disk: {len(list((DATA / 'categories').glob('*.md')))}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
