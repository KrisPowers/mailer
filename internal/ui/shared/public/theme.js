// The look picked in Settings, applied before the page draws.
try {
  ["theme", "motion", "density"].forEach(function (key) {
    var value = localStorage.getItem(key);
    if (value) document.documentElement.dataset[key] = value;
  });
} catch (e) {}
