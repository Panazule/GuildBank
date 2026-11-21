(function () {
  "use strict";

  var form = document.getElementById("setup-form");
  if (!form) {
    return;
  }
  var steps = Array.prototype.slice.call(form.querySelectorAll(".wizard-step"));
  var labels = Array.prototype.slice.call(document.querySelectorAll(".steps li"));
  var current = 0;

  function show(index) {
    current = Math.max(0, Math.min(index, steps.length - 1));
    steps.forEach(function (step, i) {
      step.hidden = i !== current;
    });
    labels.forEach(function (label, i) {
      label.classList.toggle("active", i === current);
    });
    var first = steps[current].querySelector("input:not([type=hidden])");
    if (first) {
      first.focus();
    }
  }

  function validStep() {
    var inputs = steps[current].querySelectorAll("input");
    for (var i = 0; i < inputs.length; i += 1) {
      if (!inputs[i].checkValidity()) {
        inputs[i].reportValidity();
        return false;
      }
    }
    return true;
  }

  form.querySelectorAll("[data-next]").forEach(function (button) {
    button.addEventListener("click", function () {
      if (validStep()) {
        show(current + 1);
      }
    });
  });
  form.querySelectorAll("[data-back]").forEach(function (button) {
    button.addEventListener("click", function () {
      show(current - 1);
    });
  });

  var testButton = document.getElementById("test-database");
  var testResult = document.getElementById("database-test-result");
  if (testButton && testResult) {
    testButton.addEventListener("click", function () {
      testButton.disabled = true;
      testResult.className = "test-result";
      testResult.textContent = "Testing connection…";
      fetch("/setup/database/test", {
        method: "POST",
        headers: { "X-CSRF-Token": document.querySelector('meta[name="csrf-token"]').content },
        body: new URLSearchParams(new FormData(form))
      })
        .then(function (response) {
          return response.json().then(function (body) {
            return { ok: response.ok, body: body };
          });
        })
        .then(function (result) {
          if (result.ok) {
            testResult.className = "test-result ok";
            testResult.textContent = "MariaDB connection verified.";
            return;
          }
          testResult.className = "test-result error";
          if (result.body && result.body.error) {
            if (result.body.error.fields) {
              testResult.textContent = Object.keys(result.body.error.fields).map(function (key) {
                return key + ": " + result.body.error.fields[key];
              }).join(" ");
            } else {
              testResult.textContent = result.body.error.message;
            }
          } else {
            testResult.textContent = "The connection test failed.";
          }
        })
        .catch(function () {
          testResult.className = "test-result error";
          testResult.textContent = "The connection test could not be completed.";
        })
        .then(function () {
          testButton.disabled = false;
        });
    });
  }

  show(0);
}());