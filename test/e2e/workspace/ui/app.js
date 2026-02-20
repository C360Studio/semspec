async function loadGreeting() {
  const response = await fetch("/hello");
  const data = await response.json();
  document.getElementById("greeting").textContent = data.message;
}

loadGreeting();
