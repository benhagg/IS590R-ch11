(function () {
  const statusEl = document.getElementById("status");
  const resultsEl = document.getElementById("results");
  const listEl = document.getElementById("item-list");

  const itemIds = Array.isArray(window.ITEM_IDS) ? window.ITEM_IDS : [];
  const apiBaseUrl = window.APP_CONFIG && window.APP_CONFIG.apiBaseUrl ? window.APP_CONFIG.apiBaseUrl : "";

  function setStatus(text) {
    statusEl.textContent = text;
  }

  function renderItemIds(ids) {
    listEl.innerHTML = "";
    ids.forEach(function (id) {
      const li = document.createElement("li");
      li.textContent = id;
      listEl.appendChild(li);
    });
  }

  async function fetchItems() {
    if (!apiBaseUrl) {
      resultsEl.textContent = "Missing API base URL. Set apiBaseUrl in config.js.";
      setStatus("Missing config");
      return;
    }

    if (!itemIds.length) {
      resultsEl.textContent = "No ItemIDs provided in index.html.";
      setStatus("No ItemIDs");
      return;
    }

    renderItemIds(itemIds);
    setStatus("Loading...");

    const params = new URLSearchParams({ itemIds: itemIds.join(",") });
    const url = apiBaseUrl.replace(/\/$/, "") + "/items?" + params.toString();

    try {
      const response = await fetch(url, {
        method: "GET",
        headers: { "Accept": "application/json" }
      });

      if (!response.ok) {
        const text = await response.text();
        resultsEl.textContent = "Request failed (" + response.status + "): " + text;
        setStatus("Error");
        return;
      }

      const data = await response.json();
      resultsEl.textContent = JSON.stringify(data, null, 2);
      setStatus("Success");
    } catch (error) {
      resultsEl.textContent = "Network error: " + error.message;
      setStatus("Error");
    }
  }

  document.addEventListener("DOMContentLoaded", fetchItems);
})();
