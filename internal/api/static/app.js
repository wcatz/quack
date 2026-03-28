let currentFilter = "";

async function fetchDuck() {
  const img = document.getElementById("duck-img");
  const loading = document.getElementById("loading");

  img.classList.remove("loaded");
  loading.classList.remove("hidden");
  loading.textContent = "fetching a duck...";

  let url = "/api/v1/random";
  if (currentFilter === "gif") url = "/api/v1/random/gif";
  else if (currentFilter === "image") url = "/api/v1/random/image";

  try {
    const resp = await fetch(url);
    if (!resp.ok) {
      loading.textContent = "no ducks yet — check back soon 🥚";
      return;
    }
    const data = await resp.json();
    img.onload = () => {
      loading.classList.add("hidden");
      img.classList.add("loaded");
    };
    img.onerror = () => {
      loading.textContent = "this duck got away 💨";
    };
    img.src = data.url;
  } catch {
    loading.textContent = "the pond is empty 🦆";
  }
}

function setFilter(el, filter) {
  currentFilter = filter;
  document.querySelectorAll(".filter").forEach((b) => b.classList.remove("active"));
  el.classList.add("active");
  fetchDuck();
}

async function updateStats() {
  try {
    const resp = await fetch("/api/v1/count");
    if (!resp.ok) return;
    const data = await resp.json();
    document.getElementById("stats").textContent =
      `🪺 ${data.total} ducks — ${data.images} photos, ${data.gifs} gifs`;
  } catch {
    // silent
  }
}

fetchDuck();
updateStats();
