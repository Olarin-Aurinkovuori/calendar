// flagdays.js toggles the "(Recommended Flag Day)" hint next to each flag
// day's "mandatory" checkbox in the configuration UI, so it always shows
// the opposite of the checkbox's current state: visible while unchecked
// (recommended, not mandatory), hidden once checked (mandatory).
(function () {
  "use strict";

  function init() {
    document.querySelectorAll("[data-flagday-mandatory]").forEach(function (checkbox) {
      var hint = checkbox.parentElement.querySelector("[data-flagday-recommended]");
      if (!hint) return;
      checkbox.addEventListener("change", function () {
        hint.hidden = checkbox.checked;
      });
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
