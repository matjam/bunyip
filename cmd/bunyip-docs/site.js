// Sidebar toggle for narrow screens and a symbol search over symbols.json.
(function () {
  var root = document.currentScript.dataset.root || "";
  var menu = document.querySelector(".menu");
  var side = document.querySelector(".side");
  if (menu) menu.addEventListener("click", function () { side.classList.toggle("open"); });

  var input = document.getElementById("search");
  var results = document.getElementById("results");
  if (!input) return;
  var symbols = null, hot = -1;

  function load(cb) {
    if (symbols) return cb();
    fetch(root + "symbols.json").then(function (r) { return r.json(); }).then(function (s) { symbols = s; cb(); });
  }
  function show(list) {
    results.innerHTML = "";
    hot = -1;
    if (!list.length) { results.hidden = true; return; }
    list.slice(0, 30).forEach(function (s) {
      var a = document.createElement("a");
      a.href = root + s.url;
      a.innerHTML = '<span class="kind">' + s.kind + "</span><span>" + s.name + '</span><span class="pkg">' + s.pkg + "</span>";
      results.appendChild(a);
    });
    results.hidden = false;
  }
  function rank(q) {
    q = q.toLowerCase();
    var out = [];
    symbols.forEach(function (s) {
      var n = s.name.toLowerCase(), full = (s.pkg + "." + s.name).toLowerCase();
      var score = n === q ? 0 : n.startsWith(q) ? 1 : n.indexOf(q) >= 0 ? 2 : full.indexOf(q) >= 0 ? 3 : -1;
      if (score >= 0) out.push({ score: score, s: s });
    });
    out.sort(function (a, b) { return a.score - b.score || a.s.name.length - b.s.name.length; });
    return out.map(function (o) { return o.s; });
  }
  input.addEventListener("input", function () {
    var q = input.value.trim();
    if (!q) { results.hidden = true; return; }
    load(function () { show(rank(q)); });
  });
  input.addEventListener("keydown", function (e) {
    var items = results.querySelectorAll("a");
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (!items.length) return;
      hot = (hot + (e.key === "ArrowDown" ? 1 : items.length - 1)) % items.length;
      items.forEach(function (a, i) { a.classList.toggle("hot", i === hot); });
      items[hot].scrollIntoView({ block: "nearest" });
    } else if (e.key === "Enter" && items.length) {
      window.location = items[hot >= 0 ? hot : 0].href;
    } else if (e.key === "Escape") {
      results.hidden = true;
    }
  });
  document.addEventListener("click", function (e) {
    if (!e.target.closest(".search")) results.hidden = true;
  });
})();
