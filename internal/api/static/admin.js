let adminToken = localStorage.getItem("quack-admin-token") || "";

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

async function loadGallery() {
  const btn = document.getElementById("btn-gallery");
  const grid = document.getElementById("gallery-grid");
  btn.disabled = true;
  btn.textContent = "Loading...";
  grid.classList.remove("hidden");
  grid.innerHTML = "";

  try {
    const resp = await fetch("/api/v1/admin/gallery", { headers: adminHeaders() });
    if (resp.status === 401) { adminAuthFailed(); return; }
    const items = await resp.json();
    for (const item of items) {
      const card = document.createElement("div");
      card.className = "gallery-card";
      card.innerHTML = `
        <img src="${item.url}" loading="lazy" />
        <div class="gallery-info">
          <span class="gallery-type">${item.type}</span>
          <button class="gallery-delete" onclick="deleteImage('${item.key}', this)">🗑️</button>
        </div>
      `;
      grid.appendChild(card);
    }
    btn.textContent = `🖼️ Gallery (${items.length})`;
  } catch {
    grid.innerHTML = '<p style="color:#888">Failed to load gallery</p>';
    btn.textContent = "🖼️ Gallery";
  }
  btn.disabled = false;
}

async function deleteImage(key, btn) {
  if (!confirm("Delete " + key + "?")) return;
  btn.disabled = true;
  btn.textContent = "...";
  try {
    const resp = await fetch("/api/v1/admin/images/" + key, {
      method: "DELETE",
      headers: adminHeaders(),
    });
    if (resp.status === 401) { adminAuthFailed(); return; }
    if (resp.ok) {
      btn.closest(".gallery-card").remove();
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
