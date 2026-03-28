let adminToken = localStorage.getItem("quack-admin-token") || "";
let galleryOffset = 0;
let galleryTotal = 0;
const PAGE_SIZE = 20;

function adminHeaders() {
  return { Authorization: "Bearer " + adminToken };
}

function adminLogin() {
  adminToken = document.getElementById("admin-token").value.trim();
  if (!adminToken) return;
  localStorage.setItem("quack-admin-token", adminToken);
  document.getElementById("admin-login").classList.add("hidden");
  document.getElementById("admin-content").classList.remove("hidden");
}

async function loadGallery(offset) {
  if (offset === undefined) offset = 0;
  galleryOffset = offset;

  const btn = document.getElementById("btn-gallery");
  const grid = document.getElementById("gallery-grid");
  const pager = document.getElementById("gallery-pager");
  btn.disabled = true;
  btn.textContent = "Loading...";
  grid.classList.remove("hidden");
  pager.classList.remove("hidden");
  grid.innerHTML = "";

  try {
    const resp = await fetch(`/api/v1/admin/gallery?offset=${offset}&limit=${PAGE_SIZE}`, {
      headers: adminHeaders(),
    });
    if (resp.status === 401) { adminAuthFailed(); return; }
    const data = await resp.json();
    galleryTotal = data.total;

    for (const item of data.items) {
      const card = document.createElement("div");
      card.className = "gallery-card";
      card.dataset.key = item.key;
      card.innerHTML = `
        <img src="${item.url}" loading="lazy" />
        <div class="gallery-info">
          <span class="gallery-type">${item.type}</span>
          <button class="gallery-delete" onclick="deleteImage('${item.key}', this)">🗑️</button>
        </div>
      `;
      grid.appendChild(card);
    }

    updatePager();
    btn.textContent = `🖼️ Gallery`;
  } catch {
    grid.innerHTML = '<p style="color:#888">Failed to load gallery</p>';
    btn.textContent = "🖼️ Gallery";
  }
  btn.disabled = false;
}

function updatePager() {
  const page = Math.floor(galleryOffset / PAGE_SIZE) + 1;
  const totalPages = Math.ceil(galleryTotal / PAGE_SIZE);
  const hasPrev = galleryOffset > 0;
  const hasNext = galleryOffset + PAGE_SIZE < galleryTotal;

  const html = `
    <button ${hasPrev ? "" : "disabled"} onclick="loadGallery(0)">&#171;</button>
    <button ${hasPrev ? "" : "disabled"} onclick="loadGallery(${galleryOffset - PAGE_SIZE})">&#8249; Prev</button>
    <span>${page} / ${totalPages} (${galleryTotal} total)</span>
    <button ${hasNext ? "" : "disabled"} onclick="loadGallery(${galleryOffset + PAGE_SIZE})">Next &#8250;</button>
    <button ${hasNext ? "" : "disabled"} onclick="loadGallery(${(totalPages - 1) * PAGE_SIZE})">&#187;</button>
  `;
  document.getElementById("gallery-pager").innerHTML = html;
  const bottom = document.getElementById("gallery-pager-bottom");
  if (bottom) {
    bottom.classList.remove("hidden");
    bottom.innerHTML = html;
  }
}

async function runScrape() {
  const btn = document.getElementById("btn-scrape");
  btn.disabled = true;
  btn.textContent = "Searching...";
  try {
    const resp = await fetch("/api/v1/admin/scrape", {
      method: "POST",
      headers: adminHeaders(),
    });
    if (resp.status === 401) { adminAuthFailed(); return; }
    const data = await resp.json();
    btn.textContent = `+${data.new} ducks!`;
    setTimeout(() => { btn.textContent = "🔍 Find Ducks"; btn.disabled = false; }, 2000);
  } catch {
    btn.textContent = "🔍 Find Ducks";
    btn.disabled = false;
  }
}

async function deleteImage(key, btn) {
  btn.disabled = true;
  btn.textContent = "...";
  try {
    const resp = await fetch("/api/v1/admin/images/" + key, {
      method: "DELETE",
      headers: adminHeaders(),
    });
    if (resp.status === 401) { adminAuthFailed(); return; }
    if (resp.ok) {
      const card = btn.closest(".gallery-card");
      card.classList.add("deleted");
      setTimeout(() => card.remove(), 300);
      galleryTotal--;
      updatePager();
    } else {
      btn.textContent = "err";
    }
  } catch {
    btn.textContent = "err";
  }
}

async function runCleanup(dryRun) {
  const btnId = dryRun ? "btn-cleanup" : "btn-cleanup-run";
  const btn = document.getElementById(btnId);
  const result = document.getElementById("cleanup-result");
  btn.disabled = true;
  btn.textContent = dryRun ? "Scanning..." : "Purging...";
  result.classList.remove("hidden");
  result.innerHTML = "";

  try {
    const resp = await fetch("/api/v1/admin/cleanup?dry_run=" + dryRun, {
      method: "POST",
      headers: adminHeaders(),
    });
    if (resp.status === 401) { adminAuthFailed(); return; }
    const data = await resp.json();

    let html = `<p><strong>${dryRun ? "Scan" : "Purge"} complete</strong> — `;
    html += `${data.found} oversized (>${(data.max_size_bytes / 1024 / 1024).toFixed(0)}MB)`;
    if (!dryRun) html += `, ${data.deleted} deleted`;
    html += `</p>`;

    if (data.oversized && data.oversized.length > 0) {
      html += '<div class="cleanup-list">';
      for (const item of data.oversized) {
        html += `<div class="cleanup-item"><code>${item.key}</code> <span>${item.size}</span></div>`;
      }
      html += "</div>";
    } else {
      html += "<p>All clean 🦆</p>";
    }
    result.innerHTML = html;
  } catch {
    result.innerHTML = '<p style="color:#e74c3c">Cleanup request failed</p>';
  }
  btn.textContent = dryRun ? "🔍 Scan Oversized" : "🗑️ Purge Oversized";
  btn.disabled = false;
}

function adminAuthFailed() {
  adminToken = "";
  localStorage.removeItem("quack-admin-token");
  document.getElementById("admin-login").classList.remove("hidden");
  document.getElementById("admin-content").classList.add("hidden");
  const input = document.getElementById("admin-token");
  input.value = "";
  input.placeholder = "Invalid token — try again";
}

// Auto-unlock if token saved
if (adminToken) {
  document.getElementById("admin-login").classList.add("hidden");
  document.getElementById("admin-content").classList.remove("hidden");
}
