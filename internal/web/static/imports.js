(function () {
  "use strict";

  var csrfMeta = document.querySelector('meta[name="csrf-token"]');
  var csrf = csrfMeta ? csrfMeta.content : "";

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) { node.className = className; }
    if (text !== undefined && text !== null) { node.textContent = String(text); }
    return node;
  }

  function parseError(body, fallback) {
    if (body && body.error) {
      if (body.error.fields) {
        var parts = [];
        Object.keys(body.error.fields).forEach(function (key) {
          parts.push(key + ": " + body.error.fields[key]);
        });
        if (parts.length) { return parts.join(" "); }
      }
      if (body.error.message) { return body.error.message; }
    }
    return fallback;
  }

  function request(url, options) {
    options = options || {};
    options.headers = options.headers || {};
    if (csrf) { options.headers["X-CSRF-Token"] = csrf; }
    options.headers.Accept = "application/json";
    return fetch(url, options).then(function (response) {
      return response.json().catch(function () { return {}; }).then(function (body) {
        if (!response.ok) {
          var error = new Error(parseError(body, "The request failed."));
          error.status = response.status;
          error.body = body;
          throw error;
        }
        return body;
      });
    });
  }

  var uploadForm = document.getElementById("import-upload-form");
  if (uploadForm) {
    var uploadStatus = document.getElementById("upload-status");
    uploadForm.addEventListener("submit", function (event) {
      if (!window.fetch || !window.FormData) { return; }
      event.preventDefault();
      var button = uploadForm.querySelector("button[type=submit]");
      var input = uploadForm.querySelector("input[type=file]");
      if (!input || !input.files || input.files.length === 0) {
        uploadStatus.className = "test-result error";
        uploadStatus.textContent = "Choose an export file first.";
        return;
      }
      button.disabled = true;
      uploadStatus.className = "test-result";
      uploadStatus.textContent = "Uploading…";
      request("/api/v1/imports", { method: "POST", body: new FormData(uploadForm) })
        .then(function (body) {
          var batch = body.data && body.data.batch;
          if (batch && batch.id) {
            window.location.href = "/admin/imports/" + batch.id + "?uploaded=1";
            return;
          }
          throw new Error("The upload response did not include a batch.");
        })
        .catch(function (error) {
          uploadStatus.className = "test-result error";
          uploadStatus.textContent = error.message;
          button.disabled = false;
        });
    });
  }

  var main = document.getElementById("import-detail");
  if (!main) { return; }
  var batchId = main.getAttribute("data-batch-id");
  var status = main.getAttribute("data-status");
  var editable = status === "Uploaded";
  if (!batchId || batchId === "0") { return; }

  var summaryNode = document.getElementById("import-summary");
  var locationListNode = document.getElementById("location-list");
  var rowBody = document.getElementById("row-body");
  var pagerNode = document.getElementById("row-pager");
  var statusNode = document.getElementById("import-status");
  var filterForm = document.getElementById("row-filters");
  var locationFilter = document.getElementById("filter-location");
  var selectedFilter = document.getElementById("filter-selected");
  var searchFilter = document.getElementById("filter-search");
  var commitButton = document.getElementById("commit-button");
  var selectAllButton = document.getElementById("select-all");
  var clearAllButton = document.getElementById("clear-all");

  var selected = {};
  var lastSaved = {};
  var locations = [];
  var locationIDs = {};
  var summary = null;
  var currentPage = 1;
  var pageSize = parseInt(main.getAttribute("data-page-size"), 10) || 50;
  var totalRows = 0;
  var inFlight = false;
  var pendingSave = null;
  var saveTimer = null;
  var saveGeneration = 0;
  var saveAgain = false;

  function cloneSelection() {
    var copy = {};
    Object.keys(selected).forEach(function (key) { copy[key] = true; });
    return copy;
  }
  function selectionCount() { return Object.keys(selected).length; }
  function isSelected(id) { return selected[String(id)] === true; }
  function setSelected(id, value) {
    if (value) { selected[String(id)] = true; } else { delete selected[String(id)]; }
  }

  function showStatus(message, isError) {
    if (!statusNode) { return; }
    statusNode.className = "test-result " + (isError ? "error" : "ok");
    statusNode.textContent = message;
  }

  function api(path, options) { return request("/api/v1/imports/" + batchId + path, options); }

  function renderSummary() {
    if (!summaryNode || !summary) { return; }
    summaryNode.textContent = "";
    var cards = [
      ["Selected rows", summary.selectedRows + " of " + summary.stagedRows],
      ["Locations", summary.selectedLocations + " of " + summary.totalLocations],
      ["Item rows", summary.itemRows + " rows / " + summary.itemUnits + " units"],
      ["Spell rows", summary.spellRows + " rows / qty " + summary.spellQuantity],
      ["Spells", summary.newSpells + " new / " + summary.mergedSpells + " merged"],
      ["Records to create", summary.inventoryRecordsToCreate],
      ["Records to import", summary.totalRecordsToImport],
      ["Rows with warnings", summary.warningRows]
    ];
    cards.forEach(function (card) {
      var box = el("div", "summary-card");
      box.appendChild(el("span", "summary-label", card[0]));
      box.appendChild(el("strong", "summary-value", card[1]));
      summaryNode.appendChild(box);
    });
    if (commitButton) {
      commitButton.disabled = inFlight || summary.selectedRows === 0 || !editable;
    }
  }

  function updateLocationCheckboxes() {
    if (!locationListNode) { return; }
    var boxes = locationListNode.querySelectorAll("input[type=checkbox]");
    Array.prototype.forEach.call(boxes, function (box) {
      var rowCount = parseInt(box.getAttribute("data-row-count"), 10) || 0;
      var selectedCount = parseInt(box.getAttribute("data-selected-count"), 10) || 0;
      box.checked = rowCount > 0 && selectedCount === rowCount;
      box.indeterminate = selectedCount > 0 && selectedCount < rowCount;
    });
  }

  function renderLocations() {
    if (!locationListNode) { return; }
    locationListNode.textContent = "";
    if (locationFilter) {
      var current = locationFilter.value;
      locationFilter.textContent = "";
      var all = el("option", null, "All locations");
      all.value = "";
      locationFilter.appendChild(all);
      var seen = {};
      locations.forEach(function (location) {
        if (!seen[location.group]) {
          seen[location.group] = true;
          var option = el("option", null, location.group);
          option.value = location.group;
          locationFilter.appendChild(option);
        }
      });
      locationFilter.value = current;
    }
    locations.forEach(function (location) {
      var item = el("div", "location-item");
      var label = el("label", "location-toggle");
      var checkbox = document.createElement("input");
      checkbox.type = "checkbox";
      checkbox.setAttribute("data-location", location.group);
      checkbox.setAttribute("data-row-count", location.rowCount);
      checkbox.setAttribute("data-selected-count", location.selectedCount);
      if (editable) {
        checkbox.addEventListener("change", function () {
          toggleLocation(location.group, checkbox.checked);
        });
      } else {
        checkbox.disabled = true;
      }
      label.appendChild(checkbox);
      label.appendChild(document.createTextNode(" " + location.group));
      item.appendChild(label);
      item.appendChild(el("span", "help", location.selectedCount + " / " + location.rowCount + " selected"));
      locationListNode.appendChild(item);
    });
    updateLocationCheckboxes();
  }

  function warningText(warnings) {
    if (!warnings || warnings.length === 0) { return ""; }
    return warnings.map(function (warning) { return warning.code; }).join(", ");
  }
  function warningDetail(warnings) {
    if (!warnings || warnings.length === 0) { return ""; }
    return warnings.map(function (warning) {
      return warning.code + (warning.detail ? ": " + warning.detail : "");
    }).join("\n");
  }

  function renderRows(rows) {
    if (!rowBody) { return; }
    rowBody.textContent = "";
    if (!rows.length) {
      var empty = el("tr");
      var cell = el("td", "muted", "No staged rows match these filters.");
      cell.colSpan = editable ? 8 : 7;
      empty.appendChild(cell);
      rowBody.appendChild(empty);
      return;
    }
    rows.forEach(function (row) {
      var tr = el("tr");
      if (editable) {
        var selectCell = el("td");
        var checkbox = document.createElement("input");
        checkbox.type = "checkbox";
        checkbox.checked = isSelected(row.importId);
        checkbox.setAttribute("data-import-id", row.importId);
        checkbox.setAttribute("aria-label", "Select " + row.name + " in " + row.locationGroup);
        checkbox.addEventListener("change", function () {
          toggleRow(row.importId, checkbox.checked);
        });
        selectCell.appendChild(checkbox);
        tr.appendChild(selectCell);
      }
      tr.appendChild(el("td", null, row.sourceLine || ""));
      tr.appendChild(el("td", "mono", row.itemType));
      tr.appendChild(el("td", null, row.name));
      tr.appendChild(el("td", "mono", row.equid));
      tr.appendChild(el("td", null, row.quantity));
      var locationCell = el("td");
      locationCell.appendChild(el("span", null, row.locationGroup));
      if (row.locationRaw && row.locationRaw !== row.locationGroup) {
        locationCell.appendChild(el("span", "help", row.locationRaw));
      }
      tr.appendChild(locationCell);
      var warningCell = el("td", "warnings");
      var text = warningText(row.warnings);
      if (text) {
        var badge = el("span", "warning-badge", text);
        badge.title = warningDetail(row.warnings);
        warningCell.appendChild(badge);
      }
      tr.appendChild(warningCell);
      rowBody.appendChild(tr);
    });
  }

  function renderPager() {
    if (!pagerNode) { return; }
    pagerNode.textContent = "";
    var pages = Math.max(1, Math.ceil(totalRows / pageSize));
    var previous = el("button", "secondary", "Previous");
    previous.disabled = currentPage <= 1;
    previous.addEventListener("click", function () { loadRows(currentPage - 1); });
    var next = el("button", "secondary", "Next");
    next.disabled = currentPage >= pages;
    next.addEventListener("click", function () { loadRows(currentPage + 1); });
    pagerNode.appendChild(previous);
    pagerNode.appendChild(el("span", "help", "Page " + currentPage + " of " + pages + " · " + totalRows + " rows"));
    pagerNode.appendChild(next);
  }

  function loadRows(page) {
    currentPage = Math.max(1, page || 1);
    var params = ["page=" + encodeURIComponent(currentPage), "pageSize=" + encodeURIComponent(pageSize)];
    if (locationFilter && locationFilter.value) {
      params.push("locationGroup=" + encodeURIComponent(locationFilter.value));
    }
    if (selectedFilter && selectedFilter.value) {
      params.push("selected=" + encodeURIComponent(selectedFilter.value));
    }
    if (searchFilter && searchFilter.value.trim()) {
      params.push("q=" + encodeURIComponent(searchFilter.value.trim()));
    }
    return api("/rows?" + params.join("&")).then(function (body) {
      totalRows = body.data.total;
      renderRows(body.data.rows);
      renderPager();
    }).catch(function (error) {
      showStatus(error.message, true);
    });
  }

  function refreshSummary() {
    return api("/summary").then(function (body) {
      summary = body.data;
      renderSummary();
      return summary;
    });
  }

  function refreshLocations() {
    return api("/locations").then(function (body) {
      locations = body.data.locations || [];
      renderLocations();
    });
  }

  function saveSelection() {
    if (pendingSave) { return pendingSave; }
    inFlight = true;
    if (commitButton) { commitButton.disabled = true; }
    var generation = ++saveGeneration;
    var snapshot = cloneSelection();
    var ids = Object.keys(selected).map(function (key) { return parseInt(key, 10); });
    pendingSave = api("/selection", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ selectedImportIds: ids })
    }).then(function (body) {
      if (generation === saveGeneration) {
        summary = body.data.summary;
        lastSaved = snapshot;
        renderSummary();
        showStatus("Selection saved.", false);
        return refreshLocations();
      }
    }).catch(function (error) {
      if (generation === saveGeneration) {
        selected = lastSaved;
        showStatus(error.message, true);
        loadRows(currentPage);
      }
    }).then(function () {
      pendingSave = null;
      inFlight = false;
      if (saveAgain) {
        saveAgain = false;
        scheduleSave();
        return;
      }
      if (commitButton) {
        commitButton.disabled = !editable || selectionCount() === 0;
      }
    });
    return pendingSave;
  }

  function scheduleSave() {
    if (!editable) { return; }
    if (pendingSave) {
      saveAgain = true;
      return;
    }
    if (saveTimer) { window.clearTimeout(saveTimer); }
    saveTimer = window.setTimeout(function () {
      saveTimer = null;
      saveSelection();
    }, 300);
  }

  function toggleRow(id, value) {
    setSelected(id, value);
    scheduleSave();
  }

  function applyLocationIDs(group, ids, value) {
    ids.forEach(function (id) { setSelected(id, value); });
    var boxes = locationListNode.querySelectorAll("input[type=checkbox]");
    Array.prototype.forEach.call(boxes, function (box) {
      if (box.getAttribute("data-location") === group) {
        box.checked = value;
        box.indeterminate = false;
      }
    });
    loadRows(currentPage);
    scheduleSave();
  }

  function toggleLocation(group, value) {
    showStatus("Updating " + group + "…", false);
    if (locationIDs[group]) {
      applyLocationIDs(group, locationIDs[group], value);
      return;
    }
    api("/row-ids?locationGroup=" + encodeURIComponent(group)).then(function (body) {
      locationIDs[group] = body.data.importIds || [];
      applyLocationIDs(group, locationIDs[group], value);
    }).catch(function (error) {
      showStatus(error.message, true);
      refreshLocations();
    });
  }

  function loadAll(mode) {
    api("/row-ids").then(function (body) {
      var ids = body.data.importIds || [];
      if (ids.length > 1000 && !window.confirm((mode === "all" ? "Select" : "Clear") + " " + ids.length + " rows?")) {
        return;
      }
      ids.forEach(function (id) { setSelected(id, mode === "all"); });
      locationIDs = {};
      loadRows(currentPage);
      scheduleSave();
      refreshLocations();
    }).catch(function (error) {
      showStatus(error.message, true);
    });
  }

  if (filterForm) {
    filterForm.addEventListener("submit", function (event) {
      event.preventDefault();
      loadRows(1);
    });
  }
  if (selectAllButton) {
    selectAllButton.addEventListener("click", function () { loadAll("all"); });
  }
  if (clearAllButton) {
    clearAllButton.addEventListener("click", function () { loadAll("none"); });
  }
  if (commitButton) {
    commitButton.addEventListener("click", function () {
      if (!summary || inFlight) { return; }
      var message = "Import " + summary.inventoryRecordsToCreate + " inventory records from " +
        summary.selectedRows + " selected staged rows?";
      if (!window.confirm(message)) { return; }
      inFlight = true;
      commitButton.disabled = true;
      commitButton.textContent = "Committing…";
      showStatus("Committing the selected rows…", false);
      api("/commit", { method: "POST" }).then(function (body) {
        var result = body.data;
        showStatus("Committed " + result.totalRecordsAffected + " inventory records.", false);
        window.setTimeout(function () { window.location.reload(); }, 500);
      }).catch(function (error) {
        inFlight = false;
        commitButton.disabled = false;
        commitButton.textContent = "Commit selected";
        if (error.status === 409) {
          showStatus("This batch has already been committed or is no longer editable.", true);
        } else if (error.status === 422 && error.body && error.body.error && error.body.error.code === "selection_empty") {
          showStatus("Nothing is selected. Select at least one row before committing.", true);
        } else if (error.status === 503) {
          showStatus("The guild database is busy or unavailable. Retry the commit.", true);
        } else {
          showStatus(error.message, true);
        }
      });
    });
  }

  Promise.all([api("/selection"), api("/locations"), api("/summary")])
    .then(function (results) {
      (results[0].data.selectedImportIds || []).forEach(function (id) {
        selected[String(id)] = true;
      });
      lastSaved = cloneSelection();
      locations = results[1].data.locations || [];
      summary = results[2].data;
      renderLocations();
      renderSummary();
      return loadRows(1);
    })
    .catch(function (error) {
      showStatus(error.message, true);
    });
}());