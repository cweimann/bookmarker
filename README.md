# Bookmarker

Local-first bookmark manager: a small Go HTTP server that stores bookmarks as Markdown files. Replaces the old weimann.us CGI list.

No GitHub remote yet â€” push later when ready.

## Requirements

- Go 1.22+ (tested with Go 1.27 on Windows)

On this machine Go lives at `C:\Program Files\Go\bin`. Add that to your PATH, or call `go` with the full path.

## Run

```powershell
cd C:\Users\cweim\dev\bookmarker
go run .
```

Defaults:

| Setting   | Flag / env              | Default           |
|-----------|-------------------------|-------------------|
| Listen    | `-addr` / `BOOKMARKER_ADDR` | `0.0.0.0:8787` (all interfaces) |
| Data dir  | `-data` / `BOOKMARKER_DATA` | `data` (relative to cwd) |

Examples:

```powershell
go run . -addr 127.0.0.1:9000 -data D:\bookmarks
go build -o bookmarker.exe .
.\bookmarker.exe
```

Open http://127.0.0.1:8787/ (this machine) or http://10.1.32.219:8787/ (LAN). Use `-addr 127.0.0.1:8787` if you want localhost-only again.

If the page still shows the old multi-line layout with always-visible Delete, a stale `bookmarker.exe` is almost certainly still bound to port 8787. Run `restart.cmd` in this folder (or kill that process), then hard-refresh with Ctrl+F5. When `web/` exists next to the process working directory, the server serves those files from disk so UI tweaks do not require a rebuild.

## Chrome bookmarklet

Create a bookmark whose URL is (adjust host/port if you changed `-addr`):

```javascript
javascript:(function(){var u=encodeURIComponent(location.href);var t=encodeURIComponent(document.title);location.href='http://127.0.0.1:8787/add?url='+u+'&title='+t+'&source=bookmarklet';})();
```

`GET` or `POST` `/add?url=&title=&source=` always appends to the permanent **Unfiled → Inbox**. Unfiled and its Inbox cannot be renamed or deleted. With a `Referer` header it redirects back; otherwise it shows a short confirmation.

## Data layout

Folders contain categories; each folder has its own inbox:

```
data/
  folders/
    work/
      _folder.md           # folder title
      inbox.md             # that folder's inbox
      webapp-dev.md
      k12usa-live.md
    languages/
      _folder.md
      inbox.md
      go.md
      python.md
    unfiled/
      _folder.md
      inbox.md             # bookmarklet default target
```

On first start, a one-time migration moves legacy `data/inbox.md` + `data/categories/*.md` into these folders (see `internal/store/migrate.go`).

### Entry format

Each list file starts with a `# Title` heading. Every bookmark is an `##` section:

```markdown
# Inbox

## Example Site
id: 550e8400-e29b-41d4-a716-446655440000
url: https://example.com
source: bookmarklet
saved: 2026-10-26T23:35:00Z
```

Fields: `id` (UUID), `url`, `source`, `saved` (RFC3339 UTC). Writes are atomic (temp file + rename).

## UI

- Left column: **folders**
- Middle: **categories** in the selected folder (including that folder’s Inbox)
- Main: one-line bookmarks (ssh stripe/glyph, Edit delete, drag onto categories or folders)
- Drag a category onto a folder to refile it
- Create folder / create category in the current folder

## API (used by the UI)

| Method | Path | Purpose |
|--------|------|---------|
| GET/POST | `/add` | Save to permanent Unfiled → Inbox |
| GET/POST | `/api/folders` | List / create folders |
| GET | `/api/categories?folder=` | List categories (+ inbox) |
| POST | `/api/categories` | Create `{ "folder", "title" }` |
| POST | `/api/categories/move` | `{ "slug", "fromFolder", "toFolder" }` |
| GET | `/api/bookmarks?folder=&category=` | List bookmarks |
| POST | `/api/move` | Move bookmark between folder/category |
| POST | `/api/bookmarks/delete` | `{ "id", "folder", "category" }` |


## Import from Homepage

`scripts/import_homepage.py` reads `GET /api/bookmarks` from a Homepage instance (default `http://podman.local.weimann.us:3000`), maps each group to a category, and merges by URL (keeps ssh/http/https).

```powershell
python scripts\import_homepage.py
```

## Import from old CGI

`scripts/import_showlinks.py` fetches http://www.weimann.us/showlinks.cgi (or uses cached `old-cgi.html`), categorizes by URL/title heuristics, and merges into `data/` without deleting local bookmarks (sources not starting with `showlinks:`). Re-run refreshes the imported set:

```powershell
python scripts\import_showlinks.py
python scripts\import_showlinks.py --refetch
```

## Later

- Push this repo to GitHub when Christopher asks
- Further categorize the large inbox with agent help
- Optional Chrome extension (bookmarklet is enough for now)


