(() => {
  const categoryList = document.getElementById("category-list");
  const bookmarkList = document.getElementById("bookmark-list");
  const listTitle = document.getElementById("list-title");
  const empty = document.getElementById("empty");
  const newCategoryForm = document.getElementById("new-category");
  const inboxBtn = document.querySelector('[data-slug="inbox"]');
  const editToggle = document.getElementById("edit-toggle");

  let current = "inbox";
  let categories = [];
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
    // Simple key icon
    span.innerHTML =
      '<svg viewBox="0 0 16 16" fill="currentColor" xmlns="http://www.w3.org/2000/svg">' +
      '<path d="M8.5 2a3.5 3.5 0 0 0-1.37 6.72L3 12.85V14h1.5l.5-.5.5.5H7v-1.5l.5-.5.5.5H9v-1.15l1.78-1.78A3.5 3.5 0 1 0 8.5 2Zm0 2a1.5 1.5 0 1 1 0 3 1.5 1.5 0 0 1 0-3Z"/>' +
      "</svg>";
    return span;
  }

  function setEditMode(on) {
    editMode = !!on;
    document.body.classList.toggle("edit-mode", editMode);
    editToggle.setAttribute("aria-pressed", editMode ? "true" : "false");
    editToggle.textContent = editMode ? "Done" : "Edit";
  }

  function setActive(slug) {
    current = slug;
    document.querySelectorAll(".nav-item").forEach((el) => {
      el.classList.toggle("active", el.dataset.slug === slug);
    });
    if (slug === "inbox") {
      listTitle.textContent = "Inbox";
    } else {
      const cat = categories.find((c) => c.slug === slug);
      listTitle.textContent = cat ? cat.title : slug;
    }
  }

  function wireDropTarget(el) {
    el.addEventListener("dragover", (e) => {
      e.preventDefault();
      el.classList.add("drop-target");
    });
    el.addEventListener("dragleave", () => el.classList.remove("drop-target"));
    el.addEventListener("drop", async (e) => {
      e.preventDefault();
      el.classList.remove("drop-target");
      const id = e.dataTransfer.getData("text/bookmark-id");
      const from = e.dataTransfer.getData("text/bookmark-from");
      const to = el.dataset.slug;
      if (!id || from === to) return;
      try {
        await api("/api/move", {
          method: "POST",
          body: JSON.stringify({ id, from, to }),
        });
        await loadBookmarks();
      } catch (err) {
        alert(err.message);
      }
    });
  }

  function renderCategories() {
    categoryList.innerHTML = "";
    categories.forEach((cat) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "nav-item" + (current === cat.slug ? " active" : "");
      btn.dataset.slug = cat.slug;
      btn.textContent = cat.title;
      btn.addEventListener("click", () => {
        setActive(cat.slug);
        loadBookmarks();
      });
      wireDropTarget(btn);
      categoryList.appendChild(btn);
    });
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
      li.dataset.proto = proto;

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
            body: JSON.stringify({ id: b.id, category: current }),
          });
          await loadBookmarks();
        } catch (err) {
          alert(err.message);
        }
      });

      li.appendChild(link);
      li.appendChild(host);
      li.appendChild(del);

      li.addEventListener("dragstart", (e) => {
        if (e.target.closest && e.target.closest(".delete-x")) {
          e.preventDefault();
          return;
        }
        e.dataTransfer.setData("text/bookmark-id", b.id);
        e.dataTransfer.setData("text/bookmark-from", current);
        e.dataTransfer.effectAllowed = "move";
      });

      bookmarkList.appendChild(li);
    });
  }

  async function loadCategories() {
    categories = await api("/api/categories");
    renderCategories();
  }

  async function loadBookmarks() {
    const q = current === "inbox" ? "inbox" : encodeURIComponent(current);
    const items = await api("/api/bookmarks?category=" + q);
    renderBookmarks(items);
  }

  editToggle.addEventListener("click", () => setEditMode(!editMode));

  inboxBtn.addEventListener("click", () => {
    setActive("inbox");
    loadBookmarks();
  });
  wireDropTarget(inboxBtn);

  newCategoryForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    const input = document.getElementById("category-title");
    const title = input.value.trim();
    if (!title) return;
    try {
      const cat = await api("/api/categories", {
        method: "POST",
        body: JSON.stringify({ title }),
      });
      input.value = "";
      await loadCategories();
      setActive(cat.slug);
      await loadBookmarks();
    } catch (err) {
      alert(err.message);
    }
  });

  Promise.all([loadCategories(), loadBookmarks()]).catch((err) => {
    alert("Failed to load: " + err.message);
  });
})();
