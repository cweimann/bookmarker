(() => {
  const navColumns = document.getElementById("nav-columns");
  const bookmarkList = document.getElementById("bookmark-list");
  const listTitle = document.getElementById("list-title");
  const empty = document.getElementById("empty");
  const editToggle = document.getElementById("edit-toggle");

  let maxColumns = 3;
  /** @type {string[]} path for each visible column; col0 always "" */
  let columnPaths = [""];
  /** selected folder slug within each column (opens next col) */
  let selectedFolderInCol = [];
  /** currently viewed category: { path, slug, title } */
  let view = { path: "", slug: "inbox", title: "Inbox" };
  /** listings cache by path */
  let listings = {};
  let editMode = false;

  async function api(path, opts) {
    const res = await fetch(path, {
      headers: { "Content-Type": "application/json", ...(opts && opts.headers) },
      ...opts,
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
      throw new Error(data.error || res.statusText);
    }
    return data;
  }

  function hostOf(url) {
    try {
      return new URL(url).hostname.replace(/^www\./, "");
    } catch {
      return "";
    }
  }

  function protocolOf(url) {
    try {
      return (new URL(url).protocol || "").replace(/:$/, "").toLowerCase();
    } catch {
      const m = String(url || "").match(/^([a-zA-Z][a-zA-Z0-9+.-]*):/);
      return m ? m[1].toLowerCase() : "";
    }
  }

  function sshGlyph() {
    const span = document.createElement("span");
    span.className = "proto-glyph";
    span.setAttribute("aria-hidden", "true");
    span.title = "SSH";
    span.innerHTML =
      '<svg viewBox="0 0 16 16" fill="currentColor" xmlns="http://www.w3.org/2000/svg">' +
      '<path d="M8.5 2a3.5 3.5 0 0 0-1.37 6.72L3 12.85V14h1.5l.5-.5.5.5H7v-1.5l.5-.5.5.5H9v-1.15l1.78-1.78A3.5 3.5 0 1 0 8.5 2Zm0 2a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3Z"/>' +
      "</svg>";
    return span;
  }

  function pencilBtn(ariaLabel, onClick) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "edit-pencil";
    btn.setAttribute("aria-label", ariaLabel);
    btn.title = ariaLabel;
    btn.textContent = "✎";
    btn.addEventListener("mousedown", (e) => e.stopPropagation());
    btn.addEventListener("click", (e) => {
      e.preventDefault();
      e.stopPropagation();
      onClick(e);
    });
    return btn;
  }

  function setEditMode(on) {
    editMode = !!on;
    document.body.classList.toggle("edit-mode", editMode);
    editToggle.setAttribute("aria-pressed", editMode ? "true" : "false");
    editToggle.textContent = editMode ? "Done" : "Edit";
  }

  function updateListTitle() {
    const parts = [];
    if (view.path) {
      parts.push(view.path.split("/").map(humanize).join(" / "));
    } else {
      parts.push("Root");
    }
    parts.push(view.title || view.slug);
    listTitle.textContent = parts.join(" · ");
  }

  function humanize(slug) {
    return String(slug || "")
      .split("-")
      .map((p) => (p ? p[0].toUpperCase() + p.slice(1) : ""))
      .join(" ");
  }

  async function fetchListing(path) {
    const key = path || "";
    const listing = await api(
      "/api/listing?path=" + encodeURIComponent(key)
    );
    listings[key] = listing;
    return listing;
  }

  function wireFolderDrop(el, folderPath) {
    el.addEventListener("dragover", (e) => {
      const types = e.dataTransfer.types;
      if (
        types.includes("text/category-slug") ||
        types.includes("text/bookmark-id")
      ) {
        e.preventDefault();
        el.classList.add("drop-target");
      }
    });
    el.addEventListener("dragleave", () => el.classList.remove("drop-target"));
    el.addEventListener("drop", async (e) => {
      e.preventDefault();
      e.stopPropagation();
      el.classList.remove("drop-target");
      const catSlug = e.dataTransfer.getData("text/category-slug");
      const fromPath = e.dataTransfer.getData("text/category-path");
      if (catSlug && fromPath !== undefined) {
        if (catSlug === "inbox") return;
        try {
          await api("/api/categories/move", {
            method: "POST",
            body: JSON.stringify({
              slug: catSlug,
              fromPath,
              toPath: folderPath,
            }),
          });
          if (view.path === fromPath && view.slug === catSlug) {
            view = { path: fromPath, slug: "inbox", title: "Inbox" };
          }
          await refreshVisible();
          await loadBookmarks();
        } catch (err) {
          alert(err.message);
        }
        return;
      }
      const id = e.dataTransfer.getData("text/bookmark-id");
      const bmFromPath = e.dataTransfer.getData("text/bookmark-path");
      const bmFromCat = e.dataTransfer.getData("text/bookmark-category");
      if (id) {
        try {
          await api("/api/move", {
            method: "POST",
            body: JSON.stringify({
              id,
              fromPath: bmFromPath,
              fromCategory: bmFromCat,
              toPath: folderPath,
              toCategory: "inbox",
            }),
          });
          await loadBookmarks();
        } catch (err) {
          alert(err.message);
        }
      }
    });
  }

  function wireCategoryDrop(el, folderPath, categorySlug) {
    el.addEventListener("dragover", (e) => {
      if (e.dataTransfer.types.includes("text/bookmark-id")) {
        e.preventDefault();
        el.classList.add("drop-target");
      }
    });
    el.addEventListener("dragleave", () => el.classList.remove("drop-target"));
    el.addEventListener("drop", async (e) => {
      e.preventDefault();
      e.stopPropagation();
      el.classList.remove("drop-target");
      const id = e.dataTransfer.getData("text/bookmark-id");
      const fromPath = e.dataTransfer.getData("text/bookmark-path");
      const fromCategory = e.dataTransfer.getData("text/bookmark-category");
      if (!id) return;
      try {
        await api("/api/move", {
          method: "POST",
          body: JSON.stringify({
            id,
            fromPath,
            fromCategory,
            toPath: folderPath,
            toCategory: categorySlug,
          }),
        });
        await loadBookmarks();
      } catch (err) {
        alert(err.message);
      }
    });
  }

  async function renameFolder(path, currentTitle) {
    const title = prompt("Rename folder:", currentTitle);
    if (title === null) return;
    const trimmed = title.trim();
    if (!trimmed || trimmed === currentTitle) return;
    try {
      const updated = await api("/api/folders/rename", {
        method: "POST",
        body: JSON.stringify({ path, title: trimmed }),
      });
      // Fix column paths if this folder (or ancestor) was renamed
      columnPaths = columnPaths.map((p) => {
        if (p === path) return updated.path;
        if (path && p.startsWith(path + "/")) {
          return updated.path + p.slice(path.length);
        }
        return p;
      });
      if (view.path === path) {
        view.path = updated.path;
      } else if (path && view.path.startsWith(path + "/")) {
        view.path = updated.path + view.path.slice(path.length);
      }
      await refreshVisible();
      updateListTitle();
    } catch (err) {
      alert(err.message);
    }
  }

  async function renameCategory(path, slug, currentTitle) {
    if (slug === "inbox") {
      alert("Inbox cannot be renamed.");
      return;
    }
    const title = prompt("Rename category:", currentTitle);
    if (title === null) return;
    const trimmed = title.trim();
    if (!trimmed || trimmed === currentTitle) return;
    try {
      const updated = await api("/api/categories/rename", {
        method: "POST",
        body: JSON.stringify({ path, slug, title: trimmed }),
      });
      if (view.path === path && view.slug === slug) {
        view.slug = updated.slug;
        view.title = updated.title;
      }
      await refreshVisible();
      await loadBookmarks();
    } catch (err) {
      alert(err.message);
    }
  }

  async function renameBookmark(b) {
    const title = prompt("Bookmark title:", b.title || "");
    if (title === null) return;
    const url = prompt("Bookmark URL:", b.url || "");
    if (url === null) return;
    const newTitle = title.trim();
    const newURL = url.trim();
    if (!newTitle && !newURL) return;
    if (newTitle === b.title && newURL === b.url) return;
    try {
      await api("/api/bookmarks/update", {
        method: "POST",
        body: JSON.stringify({
          id: b.id,
          path: view.path,
          category: view.slug,
          title: newTitle || b.title,
          url: newURL || b.url,
        }),
      });
      await loadBookmarks();
    } catch (err) {
      alert(err.message);
    }
  }

  function selectCategory(path, slug, title) {
    view = { path, slug, title };
    renderAllColumns();
    loadBookmarks();
  }

  function selectFolder(colIndex, folder) {
    // Keep columns up to this one, then open next
    columnPaths = columnPaths.slice(0, colIndex + 1);
    selectedFolderInCol = selectedFolderInCol.slice(0, colIndex);
    selectedFolderInCol[colIndex] = folder.slug;
    if (colIndex + 1 < maxColumns) {
      columnPaths[colIndex + 1] = folder.path;
      selectedFolderInCol = selectedFolderInCol.slice(0, colIndex + 1);
    }
    // Default view: that folder's inbox
    view = { path: folder.path, slug: "inbox", title: "Inbox" };
    refreshVisible().then(() => loadBookmarks());
  }

  function renderColumn(colIndex, path) {
    const listing = listings[path || ""] || {
      title: path ? humanize(path) : "Root",
      folders: [],
      categories: [],
    };
    const col = document.createElement("aside");
    col.className = "col";
    col.dataset.path = path || "";

    const header = document.createElement("header");
    header.className = "col-header";
    if (colIndex === 0) {
      header.classList.add("brand");
      header.innerHTML = "<h1>Bookmarker</h1>";
    } else {
      const h2 = document.createElement("h2");
      h2.textContent = listing.title || "Folder";
      header.appendChild(h2);
      if (path) {
        header.appendChild(
          pencilBtn("Rename folder", () => renameFolder(path, listing.title))
        );
        header.style.display = "flex";
        header.style.alignItems = "center";
        header.style.justifyContent = "space-between";
        header.style.gap = "0.35rem";
      }
    }
    col.appendChild(header);

    const nav = document.createElement("nav");
    nav.className = "nav";

    // Inbox + categories
    const catLabel = document.createElement("div");
    catLabel.className = "nav-section";
    catLabel.textContent = "Categories";
    nav.appendChild(catLabel);

    (listing.categories || []).forEach((c) => {
      const row = document.createElement("div");
      const active =
        view.path === (path || "") && view.slug === c.slug;
      row.className =
        "nav-row" + (c.inbox ? " inbox" : "") + (active ? " active" : "");
      row.dataset.slug = c.slug;

      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "nav-item" + (c.inbox ? " inbox" : "");
      btn.textContent = c.title;
      btn.draggable = !c.inbox;
      btn.addEventListener("click", () =>
        selectCategory(path || "", c.slug, c.title)
      );
      if (!c.inbox) {
        btn.addEventListener("dragstart", (e) => {
          e.dataTransfer.setData("text/category-slug", c.slug);
          e.dataTransfer.setData("text/category-path", path || "");
          e.dataTransfer.effectAllowed = "move";
        });
      }
      wireCategoryDrop(row, path || "", c.slug);
      row.appendChild(btn);
      if (!c.inbox) {
        row.appendChild(
          pencilBtn("Rename category", () =>
            renameCategory(path || "", c.slug, c.title)
          )
        );
      }
      nav.appendChild(row);
    });

    // Folders
    if ((listing.folders || []).length) {
      const folderLabel = document.createElement("div");
      folderLabel.className = "nav-section";
      folderLabel.textContent = "Folders";
      nav.appendChild(folderLabel);
    }
    (listing.folders || []).forEach((f) => {
      const row = document.createElement("div");
      const open = selectedFolderInCol[colIndex] === f.slug;
      row.className = "nav-row" + (open ? " active" : "");
      row.dataset.folder = f.slug;
      row.dataset.path = f.path;

      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "nav-item folder";
      btn.textContent = f.title;
      btn.addEventListener("click", () => selectFolder(colIndex, f));
      wireFolderDrop(row, f.path);
      row.appendChild(btn);
      row.appendChild(
        pencilBtn("Rename folder", () => renameFolder(f.path, f.title))
      );
      nav.appendChild(row);
    });

    col.appendChild(nav);

    // Add folder or category
    const canAddFolder = colIndex < maxColumns - 1;
    const add = document.createElement("form");
    add.className = "add-bar";
    add.innerHTML =
      '<div class="row">' +
      '<input type="text" name="title" placeholder="Name" required autocomplete="off">' +
      "</div>" +
      '<div class="row">' +
      '<select name="kind">' +
      '<option value="category">Category</option>' +
      (canAddFolder ? '<option value="folder">Folder</option>' : "") +
      "</select>" +
      '<button type="submit">Add</button>' +
      "</div>";
    add.addEventListener("submit", async (e) => {
      e.preventDefault();
      const title = add.title.value.trim();
      const kind = add.kind.value;
      if (!title) return;
      try {
        if (kind === "folder") {
          const f = await api("/api/folders", {
            method: "POST",
            body: JSON.stringify({ parentPath: path || "", title }),
          });
          add.title.value = "";
          await refreshVisible();
          selectFolder(colIndex, f);
        } else {
          const c = await api("/api/categories", {
            method: "POST",
            body: JSON.stringify({ path: path || "", title }),
          });
          add.title.value = "";
          await refreshVisible();
          selectCategory(path || "", c.slug, c.title);
        }
      } catch (err) {
        alert(err.message);
      }
    });
    // Also allow dropping onto the column itself (folder path = this column)
    wireFolderDrop(col, path || "");
    col.appendChild(add);

    return col;
  }

  function renderAllColumns() {
    navColumns.innerHTML = "";
    columnPaths.forEach((path, i) => {
      navColumns.appendChild(renderColumn(i, path));
    });
    updateListTitle();
  }

  async function refreshVisible() {
    for (const path of columnPaths) {
      await fetchListing(path);
    }
    renderAllColumns();
  }

  function renderBookmarks(items) {
    bookmarkList.innerHTML = "";
    empty.classList.toggle("hidden", items.length > 0);
    items.forEach((b) => {
      const proto = protocolOf(b.url);
      const li = document.createElement("li");
      li.className = "bookmark proto-" + (proto || "other");
      li.draggable = true;
      li.dataset.id = b.id;

      if (proto === "ssh") {
        li.appendChild(sshGlyph());
      }

      const link = document.createElement("a");
      link.className = "title";
      link.href = b.url;
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      link.textContent = b.title || b.url;
      link.draggable = false;

      const host = document.createElement("span");
      host.className = "host";
      host.textContent = hostOf(b.url);

      const rename = pencilBtn("Rename bookmark", () => renameBookmark(b));

      const del = document.createElement("button");
      del.type = "button";
      del.className = "delete-x";
      del.setAttribute("aria-label", "Delete bookmark");
      del.textContent = "×";
      del.addEventListener("mousedown", (e) => e.stopPropagation());
      del.addEventListener("click", async (e) => {
        e.preventDefault();
        e.stopPropagation();
        if (!confirm("Delete this bookmark?")) return;
        try {
          await api("/api/bookmarks/delete", {
            method: "POST",
            body: JSON.stringify({
              id: b.id,
              path: view.path,
              category: view.slug,
            }),
          });
          await loadBookmarks();
        } catch (err) {
          alert(err.message);
        }
      });

      li.appendChild(link);
      li.appendChild(host);
      li.appendChild(rename);
      li.appendChild(del);

      li.addEventListener("dragstart", (e) => {
        if (
          e.target.closest &&
          (e.target.closest(".delete-x") || e.target.closest(".edit-pencil"))
        ) {
          e.preventDefault();
          return;
        }
        e.dataTransfer.setData("text/bookmark-id", b.id);
        e.dataTransfer.setData("text/bookmark-path", view.path);
        e.dataTransfer.setData("text/bookmark-category", view.slug);
        e.dataTransfer.effectAllowed = "move";
      });

      bookmarkList.appendChild(li);
    });
    updateListTitle();
  }

  async function loadBookmarks() {
    const items = await api(
      "/api/bookmarks?path=" +
        encodeURIComponent(view.path || "") +
        "&category=" +
        encodeURIComponent(view.slug || "inbox")
    );
    renderBookmarks(items);
  }

  editToggle.addEventListener("click", () => setEditMode(!editMode));

  (async () => {
    try {
      const cfg = await api("/api/config");
      if (cfg.maxColumns) maxColumns = cfg.maxColumns;
      await fetchListing("");
      renderAllColumns();
      await loadBookmarks();
    } catch (err) {
      alert("Failed to load: " + err.message);
    }
  })();
})();
