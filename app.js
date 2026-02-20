
const API_ENDPOINT = window.APP_CONFIG?.apiBaseUrl || "http://localhost:8080";

async function fetchAndShowAllDonuts() {
  try {
    const response = await fetch(`http://localhost:8080/all_donuts`);
    const donuts = await response.json();
    const donutList = document.getElementById("donut-list");
    donutList.innerHTML = "";
    donuts.forEach(donut => {
      const li = document.createElement("li");
      li.innerHTML = `<div><span>${donut.name}</span><br>
        <img src="images/donut_${donut.itemId}.jpg" alt="${donut.name}" onerror="this.onerror=null;this.src='images/default.jpg';" style="width:100px;height:100px;" />
      </div>`;
      donutList.appendChild(li);
    });
  } catch (error) {
    console.error("Failed to fetch all donuts:", error);
  }
}

fetchAndShowAllDonuts();