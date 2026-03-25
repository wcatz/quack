let currentFilter = "";

async function fetchDuck() {
  const img = document.getElementById("duck-img");
  const loading = document.getElementById("loading");

  img.classList.remove("loaded");
  loading.classList.remove("hidden");

  let url = "/api/v1/random";
  if (currentFilter === "gif") url = "/api/v1/random/gif";
  else if (currentFilter === "image") url = "/api/v1/random/image";

  try {
    const resp = await fetch(url);
    if (!resp.ok) {
      loading.textContent = "no ducks yet — check back soon";
      return;
    }
    const data = await resp.json();
    img.onload = () => {
      loading.classList.add("hidden");
      img.classList.add("loaded");
    };
    img.onerror = () => {
      loading.textContent = "failed to load duck image";
    };
    img.src = data.url;
  } catch {
    loading.textContent = "failed to fetch duck";
  }
}

function setFilter(el, filter) {
  currentFilter = filter;
  document.querySelectorAll(".filter").forEach((b) => b.classList.remove("active"));
  el.classList.add("active");
  fetchDuck();
}

async function findDucks() {
  const btn = document.getElementById("btn-find");
  btn.disabled = true;
  btn.textContent = "Searching...";

  try {
    const resp = await fetch("/api/v1/scrape", { method: "POST" });
    if (!resp.ok) {
      btn.textContent = "Error";
      setTimeout(() => { btn.textContent = "Find Ducks"; btn.disabled = false; }, 2000);
      return;
    }
    const data = await resp.json();
    btn.textContent = `+${data.new} new`;
    updateStats();
    if (data.new > 0) fetchDuck();
    setTimeout(() => { btn.textContent = "Find Ducks"; btn.disabled = false; }, 2000);
  } catch {
    btn.textContent = "Find Ducks";
    btn.disabled = false;
  }
}

async function updateStats() {
  try {
    const resp = await fetch("/api/v1/count");
    if (!resp.ok) return;
    const data = await resp.json();
    document.getElementById("stats").textContent =
      `${data.total} ducks — ${data.images} images, ${data.gifs} gifs`;
  } catch {
    // silent
  }
}

fetchDuck();
updateStats();
