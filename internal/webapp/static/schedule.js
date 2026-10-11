// schedule.js drives the "Show more" button below the upcoming-schedule
// table: each click fetches the next batch of week rows from
// /schedule/more and appends them to the table body, without a full page
// reload. The next request's "from" date is computed client-side as
// from + step*7 days, since the server always returns exactly `step`
// calendar weeks starting at "from".
(function () {
  "use strict";

  function init() {
    var button = document.getElementById("schedule-show-more");
    var tbody = document.getElementById("schedule-rows");
    if (!button || !tbody) return;

    button.addEventListener("click", function () {
      var from = button.dataset.nextFrom;
      var step = parseInt(button.dataset.step, 10) || 12;
      if (!from) return;

      button.disabled = true;
      var basePath = window.TALONMIESVUOROT_BASE_PATH || "";
      fetch(basePath + "/schedule/more?from=" + encodeURIComponent(from) + "&weeks=" + step)
        .then(function (resp) {
          if (!resp.ok) throw new Error("request failed");
          return resp.text();
        })
        .then(function (html) {
          tbody.insertAdjacentHTML("beforeend", html);
          var next = new Date(from + "T00:00:00");
          next.setDate(next.getDate() + step * 7);
          button.dataset.nextFrom = next.toISOString().slice(0, 10);
        })
        .catch(function () {
          // leave data-next-from unchanged so the user can retry
        })
        .finally(function () {
          button.disabled = false;
        });
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
