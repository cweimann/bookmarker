(() => {
  const categoryList = document.getElementById("category-list");
  const bookmarkList = document.getElementById("bookmark-list");
  const listTitle = document.getElementById("list-title");
  const empty = document.getElementById("empty");
  const newCategoryForm = document.getElementById("new-category");
  const inboxBtn = document.querySelector('[data-slug="inbox"]');

  let current = "inbox";
  let categories = [];

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
      const li = document.createElement("li");
      li.className = "bookmark";
      li.draggable = true;
      li.dataset.id = b.id;

      const link = document.createElement("a");
      link.href = b.url;
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      link.textContent = b.title || b.url;

      const meta = document.createElement("div");
      meta.className = "meta";
      meta.textContent = [b.source, b.saved].filter(Boolean).join(" · ");

      const actions = document.createElement("div");
      actions.className = "actions";
      const del = document.createElement("button");
      del.type = "button";
      del.textContent = "Delete";
      del.addEventListener("click", async () => {
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
      actions.appendChild(del);

      li.appendChild(link);
      li.appendChild(meta);
      li.appendChild(actions);

      li.addEventListener("dragstart", (e) => {
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
