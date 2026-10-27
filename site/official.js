/* Shell behaviour: copy buttons and the mobile nav. The tabs are CSS-only. */
(function () {
  "use strict";

  function copyText(text, done) {
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).then(done, fallback);
    } else {
      fallback();
    }
    function fallback() {
      var ta = document.createElement("textarea");
      ta.value = text;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand("copy"); } catch (e) {}
      document.body.removeChild(ta);
      done();
    }
  }

  document.querySelectorAll(".install-block pre, .mcp-tabs .panel pre").forEach(function (pre) {
    var code = pre.querySelector("code");
    if (!code) return;
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "copy-btn";
    btn.textContent = "copy";
    btn.setAttribute("aria-label", "Copy command to clipboard");
    btn.setAttribute("aria-live", "polite");
    btn.addEventListener("click", function () {
      copyText(code.innerText, function () {
        btn.textContent = "copied";
        btn.setAttribute("aria-label", "Copied to clipboard");
        btn.classList.add("copied");
        setTimeout(function () {
          btn.textContent = "copy";
          btn.setAttribute("aria-label", "Copy command to clipboard");
          btn.classList.remove("copied");
        }, 1400);
      });
    });
    pre.appendChild(btn);
  });

  document.querySelectorAll(".nav-menu").forEach(function (details) {
    var summary = details.querySelector("summary");
    details.addEventListener("click", function (event) {
      if (event.target.closest("a")) details.open = false;
    });
    document.addEventListener("click", function (event) {
      if (details.open && !details.contains(event.target)) details.open = false;
    });
    document.addEventListener("keydown", function (event) {
      if (event.key === "Escape" && details.open) {
        details.open = false;
        if (summary) summary.focus();
      }
    });
  });
})();
