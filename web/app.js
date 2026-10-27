(() => {
  const folderList = document.getElementById("folder-list");
  const categoryList = document.getElementById("category-list");
  const bookmarkList = document.getElementById("bookmark-list");
  const listTitle = document.getElementById("list-title");
  const folderHeading = document.getElementById("folder-heading");
  const empty = document.getElementById("empty");
  const newFolderForm = document.getElementById("new-folder");
  const newCategoryForm = document.getElementById("new-category");
  const editToggle = document.getElementById("edit-toggle");

  let folders = [];
  let categories = [];
  let currentFolder = "";
  let currentCategory = "inbox";
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

  function folderTitle(slug) {
    const f = folders.find((x) => x.slug === slug);
    return f ? f.title : slug;
  }

  function categoryTitle(slug) {
    const c = categories.find((x) => x.slug === slug);
    return c ? c.title : slug;
  }

  function updateListTitle() {
    const ft = folderTitle(currentFolder);
    const ct = currentCategory === "inbox" ? "Inbox" : categoryTitle(currentCategory);
    listTitle.textContent = ft + " · " + ct;
    folderHeading.textContent = ft;
  }

  function wireFolderDrop(el) {
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
      el.classList.remove("drop-target");
      const toFolder = el.dataset.folder;
      const catSlug = e.dataTransfer.getData("text/category-slug");
      const fromFolder = e.dataTransfer.getData("text/category-folder");
      if (catSlug && fromFolder) {
        if (catSlug === "inbox") return;
        if (fromFolder === toFolder) return;
        try {
          await api("/api/categories/move", {
            method: "POST",
            body: JSON.stringify({
              slug: catSlug,
              fromFolder,
              toFolder,
            }),
          });
          if (currentFolder === fromFolder || currentFolder === toFolder) {
            await loadCategories();
            if (currentFolder === fromFolder && currentCategory === catSlug) {
              currentCategory = "inbox";
            }
            await loadBookmarks();
          }
        } catch (err) {
          alert(err.message);
        }
        return;
      }
      const id = e.dataTransfer.getData("text/bookmark-id");
      const bmFromFolder = e.dataTransfer.getData("text/bookmark-folder");
      const bmFromCat = e.dataTransfer.getData("text/bookmark-category");
      if (id) {
        try {
          await api("/api/move", {
            method: "POST",
            body: JSON.stringify({
              id,
              fromFolder: bmFromFolder,
              fromCategory: bmFromCat,
              toFolder,
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

  function wireCategoryDrop(el) {
    el.addEventListener("dragover", (e) => {
      if (e.dataTransfer.types.includes("text/bookmark-id")) {
        e.preventDefault();
        el.classList.add("drop-target");
      }
    });
    el.addEventListener("dragleave", () => el.classList.remove("drop-target"));
    el.addEventListener("drop", async (e) => {
      e.preventDefault();
      el.classList.remove("drop-target");
      const id = e.dataTransfer.getData("text/bookmark-id");
      const fromFolder = e.dataTransfer.getData("text/bookmark-folder");
      const fromCategory = e.dataTransfer.getData("text/bookmark-category");
      const toCategory = el.dataset.slug;
      if (!id) return;
      try {
        await api("/api/move", {
          method: "POST",
          body: JSON.stringify({
            id,
            fromFolder,
            fromCategory,
            toFolder: currentFolder,
            toCategory,
          }),
        });
        await loadBookmarks();
      } catch (err) {
        alert(err.message);
      }
    });
  }

  async function renameFolder(f) {
    const title = prompt("Rename folder:", f.title);
    if (title === null) return;
    const trimmed = title.trim();
    if (!trimmed || trimmed === f.title) return;
    try {
      const updated = await api("/api/folders/rename", {
        method: "POST",
        body: JSON.stringify({ slug: f.slug, title: trimmed }),
      });
      if (currentFolder === f.slug) {
        currentFolder = updated.slug;
      }
      await loadFolders();
      renderFolders();
      await loadCategories();
      await loadBookmarks();
    } catch (err) {
      alert(err.message);
    }
  }

  async function renameCategory(c) {
    if (c.inbox) return;
    const title = prompt("Rename category:", c.title);
    if (title === null) return;
    const trimmed = title.trim();
    if (!trimmed || trimmed === c.title) return;
    try {
      const updated = await api("/api/categories/rename", {
        method: "POST",
        body: JSON.stringify({
          folder: currentFolder,
          slug: c.slug,
          title: trimmed,
        }),
      });
      if (currentCategory === c.slug) {
        currentCategory = updated.slug;
      }
      await loadCategories();
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
          folder: currentFolder,
          category: currentCategory,
          title: newTitle || b.title,
          url: newURL || b.url,
        }),
      });
      await loadBookmarks();
    } catch (err) {
      alert(err.message);
    }
  }

  function renderFolders() {
    folderList.innerHTML = "";
    folders.forEach((f) => {
      const row = document.createElement("div");
      row.className =
        "nav-row" + (currentFolder === f.slug ? " active" : "");
      row.dataset.folder = f.slug;

      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "nav-item";
      btn.dataset.folder = f.slug;
      btn.textContent = f.title;
      btn.addEventListener("click", async () => {
        currentFolder = f.slug;
        currentCategory = "inbox";
        renderFolders();
        await loadCategories();
        await loadBookmarks();
      });
      wireFolderDrop(btn);
      wireFolderDrop(row);

      row.appendChild(btn);
      row.appendChild(
        pencilBtn("Rename folder", () => renameFolder(f))
      );
      folderList.appendChild(row);
    });
  }

  function renderCategories() {
    categoryList.innerHTML = "";
    categories.forEach((c) => {
      const row = document.createElement("div");
      row.className =
        "nav-row" +
        (c.inbox ? " inbox" : "") +
        (currentCategory === c.slug ? " active" : "");
      row.dataset.slug = c.slug;

      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "nav-item" + (c.inbox ? " inbox" : "");
      btn.dataset.slug = c.slug;
      btn.textContent = c.title;
      btn.draggable = !c.inbox;
      btn.addEventListener("click", () => {
        currentCategory = c.slug;
        renderCategories();
        loadBookmarks();
      });
      if (!c.inbox) {
        btn.addEventListener("dragstart", (e) => {
          e.dataTransfer.setData("text/category-slug", c.slug);
          e.dataTransfer.setData("text/category-folder", currentFolder);
          e.dataTransfer.effectAllowed = "move";
        });
      }
      wireCategoryDrop(btn);
      wireCategoryDrop(row);

      row.appendChild(btn);
      if (!c.inbox) {
        row.appendChild(
          pencilBtn("Rename category", () => renameCategory(c))
        );
      }
      categoryList.appendChild(row);
    });
    updateListTitle();
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
              folder: currentFolder,
              category: currentCategory,
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
        e.dataTransfer.setData("text/bookmark-folder", currentFolder);
        e.dataTransfer.setData("text/bookmark-category", currentCategory);
        e.dataTransfer.effectAllowed = "move";
      });

      bookmarkList.appendChild(li);
    });
    updateListTitle();
  }

  async function loadFolders() {
    folders = await api("/api/folders");
    if (!currentFolder && folders.length) {
      const unfiled = folders.find((f) => f.slug === "unfiled");
      currentFolder = unfiled ? unfiled.slug : folders[0].slug;
    } else if (
      currentFolder &&
      folders.length &&
      !folders.some((f) => f.slug === currentFolder)
    ) {
      currentFolder = folders[0].slug;
      currentCategory = "inbox";
    }
    renderFolders();
  }

  async function loadCategories() {
    if (!currentFolder) return;
    categories = await api(
      "/api/categories?folder=" + encodeURIComponent(currentFolder)
    );
    if (!categories.some((c) => c.slug === currentCategory)) {
      currentCategory = "inbox";
    }
    renderCategories();
  }

  async function loadBookmarks() {
    if (!currentFolder) return;
    const items = await api(
      "/api/bookmarks?folder=" +
        encodeURIComponent(currentFolder) +
        "&category=" +
        encodeURIComponent(currentCategory)
    );
    renderBookmarks(items);
  }

  editToggle.addEventListener("click", () => setEditMode(!editMode));

  newFolderForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const input = document.getElementById("folder-title");
    const title = input.value.trim();
    if (!title) return;
    try {
      const f = await api("/api/folders", {
        method: "POST",
        body: JSON.stringify({ title }),
      });
      input.value = "";
      await loadFolders();
      currentFolder = f.slug;
      currentCategory = "inbox";
      renderFolders();
      await loadCategories();
      await loadBookmarks();
    } catch (err) {
      alert(err.message);
    }
  });

  newCategoryForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const input = document.getElementById("category-title");
    const title = input.value.trim();
    if (!title || !currentFolder) return;
    try {
      const cat = await api("/api/categories", {
        method: "POST",
        body: JSON.stringify({ folder: currentFolder, title }),
      });
      input.value = "";
      await loadCategories();
      currentCategory = cat.slug;
      renderCategories();
      await loadBookmarks();
    } catch (err) {
      alert(err.message);
    }
  });

  Promise.all([loadFolders()])
    .then(() => loadCategories())
    .then(() => loadBookmarks())
    .catch((err) => {
      alert("Failed to load: " + err.message);
    });
})();
