// OpenTepache: mejoras opcionales (specs/006-repo-website, FR-018). Sin este
// script el sitio funciona igual: los botones de copiar y de tema no aparecen.
(function () {
  "use strict";
  var root = document.documentElement;
  var KEY = "opentepache-theme";

  // El tema elegido se aplica antes de pintar, para no parpadear.
  try {
    var saved = localStorage.getItem(KEY);
    if (saved === "light" || saved === "dark") root.setAttribute("data-theme", saved);
  } catch (e) { /* almacenamiento bloqueado: se sigue al sistema */ }

  function current() {
    var t = root.getAttribute("data-theme");
    if (t) return t;
    return window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }

  function ready() {
    var toggle = document.querySelector("button.theme");
    if (toggle) {
      var sync = function () { toggle.setAttribute("aria-pressed", current() === "dark" ? "true" : "false"); };
      toggle.hidden = false;
      sync();
      toggle.addEventListener("click", function () {
        var next = current() === "dark" ? "light" : "dark";
        root.setAttribute("data-theme", next);
        try { localStorage.setItem(KEY, next); } catch (e) { /* sin persistencia */ }
        sync();
      });
    }

    if (!navigator.clipboard) return;
    var buttons = document.querySelectorAll("button.copy[data-copy]");
    for (var i = 0; i < buttons.length; i++) {
      (function (btn) {
        var code = document.getElementById(btn.getAttribute("data-copy"));
        if (!code) return;
        var label = btn.textContent;
        btn.hidden = false;
        btn.addEventListener("click", function () {
          navigator.clipboard.writeText(code.textContent).then(function () {
            btn.textContent = btn.getAttribute("data-done") || label;
            setTimeout(function () { btn.textContent = label; }, 2000);
          });
        });
      })(buttons[i]);
    }
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", ready);
  else ready();
})();
