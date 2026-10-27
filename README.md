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

`GET` or `POST` `/add?url=&title=&source=` always appends to the **root Inbox** (`data/inbox.md`). With a `Referer` header it redirects back; otherwise it shows a short confirmation.

## Data layout

`data/` is the root folder. Subfolders nest on disk; each folder has `_folder.md` + `inbox.md` plus category `.md` files:

```
data/
  _folder.md              # root title
  inbox.md                # root inbox (bookmarklet target)
  some-root-category.md
  work/
    _folder.md
    inbox.md
    webapp-dev.md
    deepen/               # nested folder (UI shows up to 3 columns)
      _folder.md
      inbox.md
```

UI column cap is `store.MaxColumns` (default 3). Legacy `data/folders/…` (including Unfiled) migrates once into this tree (`internal/store/migrate.go`).

### Entry format

Each list file starts with a `# Title` heading. Every bookmark is an `##` section with `id`, `url`, `source`, `saved` (RFC3339). Writes are atomic.

## UI

- Up to **3 columns**: root → selected folder → nested folder. Each column lists Inbox, categories, and child folders; Add creates a category or folder.
- Main pane: one-line bookmarks (ssh stripe/glyph, Edit pencils/×). Sticky title/Edit; side columns do not scroll with the list.
- Drag bookmarks onto categories/folders; drag categories onto folders.

## API (used by the UI)

| Method | Path | Purpose |
|--------|------|---------|
| GET/POST | `/add` | Save to root Inbox |
| GET | `/api/config` | `{ maxColumns }` |
| GET | `/api/listing?path=` | Folders + categories at path (`""` = root) |
| POST | `/api/folders` | `{ parentPath, title }` |
| POST | `/api/folders/rename` | `{ path, title }` |
| POST | `/api/categories` | `{ path, title }` |
| POST | `/api/categories/rename` | `{ path, slug, title }` |
| POST | `/api/categories/move` | `{ slug, fromPath, toPath }` |
| GET | `/api/bookmarks?path=&category=` | List bookmarks |
| POST | `/api/move` | Move bookmark |
| POST | `/api/bookmarks/update` | Rename bookmark title/URL |
| POST | `/api/bookmarks/delete` | Delete bookmark |


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


