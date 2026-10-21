// Command palette: keep focus in the search input while ArrowUp/ArrowDown move
// the active option through aria-activedescendant. Enter activates the active
// option (or the first visible one). Datastar owns the filtering signal; this
// file only owns the keyboard cursor, so there is no second state model.
(function () {
  "use strict";

  function visibleLinks(list) {
    return Array.from(list.querySelectorAll("a[data-command]")).filter(function (link) {
      return link.offsetParent !== null;
    });
  }

  function init() {
    var dialog = document.getElementById("command-palette");
    var input = document.getElementById("command-palette-input");
    var list = document.getElementById("command-palette-list");
    if (!dialog || !input || !list) return;

    var active = -1;

    function clearActive() {
      active = -1;
      input.removeAttribute("aria-activedescendant");
      list.querySelectorAll("a[data-command]").forEach(function (link) {
        link.classList.remove("command-active");
        link.setAttribute("aria-selected", "false");
      });
    }

    function setActive(index) {
      var links = visibleLinks(list);
      if (!links.length) {
        clearActive();
        return;
      }
      active = (index + links.length) % links.length;
      links.forEach(function (link, i) {
        var on = i === active;
        link.classList.toggle("command-active", on);
        link.setAttribute("aria-selected", on ? "true" : "false");
        if (on && !link.id) link.id = "command-option-" + i;
      });
      input.setAttribute("aria-activedescendant", links[active].id);
      links[active].scrollIntoView({ block: "nearest" });
    }

    input.addEventListener("input", clearActive);

    input.addEventListener("keydown", function (event) {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setActive(active + 1);
      } else if (event.key === "ArrowUp") {
        event.preventDefault();
        setActive(active < 0 ? visibleLinks(list).length - 1 : active - 1);
      } else if (event.key === "Enter") {
        var links = visibleLinks(list);
        var target = active >= 0 ? links[active] : links[0];
        if (target) {
          event.preventDefault();
          target.click();
        }
      }
    });

    dialog.addEventListener("toggle", function () {
      if (dialog.open) {
        clearActive();
        input.value = "";
        input.dispatchEvent(new Event("input", { bubbles: true }));
        setTimeout(function () {
          input.focus();
        }, 0);
      } else {
        clearActive();
      }
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init, { once: true });
  } else {
    init();
  }
})();
