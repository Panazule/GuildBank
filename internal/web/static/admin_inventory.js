// Name-first item identity for the admin inventory create form.
//
// The server is authoritative: this script only fills or explains the EQUID
// field so the officer sees the outcome before submitting. Without it the same
// resolution runs server-side (unique match, candidate choice, or the
// unresolved-identity placeholder).
(function () {
  var nameInput = document.getElementById('item-name');
  var equidInput = document.getElementById('item-equid');
  var result = document.getElementById('name-lookup-result');
  if (!nameInput || !result) {
    return;
  }

  var timer = null;
  var lastQuery = '';

  function setResult(text, cls) {
    result.textContent = text;
    result.className = cls || 'help';
  }

  function candidatesText(candidates) {
    var parts = [];
    for (var i = 0; i < candidates.length; i++) {
      parts.push(candidates[i].equid + ' — ' + candidates[i].name);
    }
    return parts.join('; ');
  }

  function lookup(name) {
    if (!name || name === lastQuery) {
      return;
    }
    lastQuery = name;
    var url = '/api/v1/admin/item-names?name=' + encodeURIComponent(name);
    window.fetch(url, { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
      .then(function (response) {
        if (!response.ok) {
          throw new Error('lookup failed');
        }
        return response.json();
      })
      .then(function (body) {
        var data = (body && body.data) || {};
        if (data.status === 'unique' && data.item) {
          if (equidInput && !equidInput.value) {
            equidInput.value = data.item.equid;
          }
          setResult('Matched ' + data.item.name + ' (EQUID ' + data.item.equid + ').', 'help');
          return;
        }
        if (data.status === 'ambiguous') {
          setResult('This name matches several items (' + candidatesText(data.candidates || []) +
            '). Submit to choose one, or supply an EQUID.', 'help field-error');
          return;
        }
        setResult('No item metadata matches this name. The record will be stored with an ' +
          'unresolved identity and this name is authoritative.', 'help');
      })
      .catch(function () {
        setResult('Name lookup is unavailable; the server will still validate this name.', 'help');
      });
  }

  nameInput.addEventListener('blur', function () {
    var name = nameInput.value.trim();
    window.clearTimeout(timer);
    timer = window.setTimeout(function () {
      lookup(name);
    }, 250);
  });
})();
