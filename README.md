# Bookmarker

Local-first bookmark manager: a small Go HTTP server that stores bookmarks as Markdown files. Replaces the old weimann.us CGI list.

No GitHub remote yet; push later when ready.

## Requirements

- Go 1.22+ (tested with Go 1.27 on Windows)

On this machine Go lives at `C:\Program Files\Go\bin`. Add that to your PATH, or call `go` with the full path.

## Run

```powershell
cd C:\Users\cweim\dev\bookmarker
go run .
```

`restart.cmd` kills whatever holds port 8787, rebuilds `bookmarker.exe`, and starts it minimized. When `web/` exists in the working directory the server serves those files from disk, so UI tweaks do not need a rebuild. Hard-refresh with Ctrl+F5 if the UI looks stale.

## Running as a service

Example supervisors live under [`contrib/`](contrib/):

| | Path in tree |
|--|----------------|
| daemontools | [`contrib/daemontools/run`](contrib/daemontools/run) + [`log/run`](contrib/daemontools/log/run) |
| FreeBSD rc.d | [`contrib/freebsd/rc.d/bookmarker`](contrib/freebsd/rc.d/bookmarker) → `/usr/local/etc/rc.d/bookmarker` |
| Linux systemd | [`contrib/systemd/bookmarker.service`](contrib/systemd/bookmarker.service) (preferred on modern Linux) |

Typical install: binary `/usr/local/bin/bookmarker`, user `bookmarker`, config `/usr/local/etc/bookmarker.yaml` (FreeBSD/daemontools) or `/etc/bookmarker/bookmarker.yaml` (systemd), data `/var/db/bookmarker` or `/var/lib/bookmarker` with an **absolute** `data:` in the yaml. Enable steps are in [`contrib/README.md`](contrib/README.md).

## Configuration

Settings come from `bookmarker.yaml`. The server uses the first one it finds:

1. `-config <path>` or `BOOKMARKER_CONFIG`
2. `bookmarker.yaml` in the working directory
3. `bookmarker.yaml` next to the executable

With no file it falls back to the built-in defaults below (loopback editors only). `bookmarker.yaml` is gitignored; start from [`bookmarker.example.yaml`](bookmarker.example.yaml), which has this machine's values:

```yaml
listen:
  - 127.0.0.1:8787
  - 10.1.32.219:8787
edit:
  - 127.0.0.1
  - 10.1.32.0/24
view: []
data: data
```

| Key | Meaning | Default |
|-----|---------|---------|
| `listen` | Addresses to listen on, one listener each. `0.0.0.0:8787` only if you list it. An address that cannot be bound (interface down) is logged and skipped; the server exits only if none bind. | `[127.0.0.1:8787]` |
| `edit` | Client IPs or CIDRs that may mutate (Edit mode, Add bookmark, `/add`, API writes). Edit implies view. | `[127.0.0.1, ::1]` |
| `view` | Optional view-only client IPs/CIDRs. May browse; mutating APIs return 403. Entries under `edit` need not be repeated. | `[]` |
| `hosts` | Extra DNS names accepted in the `Host` header. IP literals and `localhost` are always accepted; other names get 421 (blocks DNS rebinding). | `[]` |
| `data` | Bookmark directory. Relative paths resolve against the config file's directory. | `data` |

At least one of `edit` or `view` must be non-empty. Anyone not on either list gets 403.

Unknown keys are an error, so typos fail at startup. Flags override the file: `-addr` (comma-separated, replaces `listen`; env `BOOKMARKER_ADDR`) and `-data` (env `BOOKMARKER_DATA`, relative to the working directory).

```powershell
go run . -addr 127.0.0.1:9000 -data D:\bookmarks
```

Open http://127.0.0.1:8787/ on this machine or http://10.1.32.219:8787/ from the 10.1.32.0/24 LAN.

## Security model

There is no login; access control is the listen addresses plus the **edit** / **view** client lists. Edit clients can mutate; view-only clients can browse (GET listing/bookmarks/UI) but get 403 on `/add` and every mutating API. The UI hides Edit / Add bookmark for view-only clients, and drag/drop is off unless Edit mode is on (editors included). On top of that:

- API writes must be same-origin JSON: `Content-Type: application/json` is required, and a foreign `Origin` or `Sec-Fetch-Site: cross-site` is refused. Other websites cannot drive the API from your browser.
- `/add` stays a plain GET for the bookmarklet but refuses subresource loads (`Sec-Fetch-Dest` other than `document`), so a page cannot plant bookmarks with an `<img>` tag.
- `javascript:`, `data:`, `vbscript:`, and `blob:` URLs are rejected; a URL with no scheme gets `https://`.
- Folder and category names are validated slugs, so API parameters cannot reach files outside `data/`.
- Responses carry a Content-Security-Policy, `X-Frame-Options: DENY`, and `nosniff`. Request bodies are capped at 1 MB and connections have read/write timeouts.

## Chrome bookmarklet

Create a bookmark whose URL is (adjust host/port to one of your `listen` addresses):

```javascript
javascript:(function(){var u=encodeURIComponent(location.href);var t=encodeURIComponent(document.title);location.href='http://127.0.0.1:8787/add?url='+u+'&title='+t+'&source=bookmarklet';})();
```

`GET` or `POST` `/add?url=&title=&source=` always appends to the **root Inbox** (`data/inbox.md`) and then redirects back to the saved page (http/https URLs). Add `&noredirect=1`, or save a non-web URL such as `ssh://`, to get a short confirmation page instead.

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

Folder and category file names must be lowercase slugs (`a-z`, `0-9`, single hyphens), which is what the UI creates. A hand-made `My Notes.md` shows in the listing but cannot be opened until renamed. Reading never creates folders; only the create/move/rename actions write.

UI column cap is `store.MaxColumns` (default 3). Legacy `data/folders/…` (including Unfiled) migrates once into this tree (`internal/store/migrate.go`).

### Entry format

Each list file starts with a `# Title` heading. Every bookmark is an `##` section with `id`, `url`, `source`, `saved` (RFC3339). Fields are single-line; newlines in titles collapse to spaces. Writes go to a temp file and replace the list with one rename. Every write regenerates the whole file, so hand-added notes or extra fields in a list file are dropped the next time the UI changes that list.

Deleting a folder promotes its contents one level up (same-named categories and folders merge). It refuses if the folder holds anything other than `.md` lists.

## UI

- Up to **3 columns**: root → selected folder → nested folder. Each column lists Inbox, categories, and child folders; Add creates a category or folder.
- Main pane: one-line bookmarks (ssh stripe/glyph, Edit pencils/×). Sticky title / Add bookmark / Edit; side columns do not scroll with the list.
- Drag bookmarks onto categories/folders (and categories onto folders) only while Edit mode is on.

## API (used by the UI)

POSTs take JSON bodies and must send `Content-Type: application/json`. Errors are `{"error": "..."}` with 400 (invalid input), 404 (not found), 409 (already exists), 415 (wrong content type), 403 (refused), or 500.

| Method | Path | Purpose |
|--------|------|---------|
| GET/POST | `/add` | Save to root Inbox (bookmarklet) |
| GET | `/api/config` | `{ maxColumns, canEdit }` |
| GET | `/api/listing?path=` | Folders + categories at path (`""` = root) |
| POST | `/api/folders` | `{ parentPath, title }` |
| POST | `/api/folders/rename` | `{ path, title }` |
| POST | `/api/folders/delete` | `{ path }` (promote contents, merge) |
| POST | `/api/categories` | `{ path, title }` |
| POST | `/api/categories/rename` | `{ path, slug, title }` |
| POST | `/api/categories/move` | `{ slug, fromPath, toPath }` |
| GET | `/api/bookmarks?path=&category=` | List bookmarks |
| POST | `/api/bookmarks/create` | `{ path, category, title, url, source }` |
| POST | `/api/bookmarks/update` | `{ id, path, category, title, url }` |
| POST | `/api/bookmarks/delete` | `{ id, path, category }` |
| POST | `/api/move` | `{ id, fromPath, fromCategory, toPath, toCategory }` |

## Importers

`scripts/import_homepage.py` (Homepage startpage) and `scripts/import_showlinks.py` (old CGI list) were written for the pre-tree `data/categories/` layout. They now refuse to run against the current tree and need updating before they are used again.

## Later

- Push this repo to GitHub when Christopher asks
- Further categorize the large inbox with agent help
- Optional Chrome extension (bookmarklet is enough for now)
- Optional login if the allowlist ever needs to widen
