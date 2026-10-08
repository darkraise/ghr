;(function () {
  var root = document.documentElement
  var mode = null
  try {
    var store = window.localStorage
    // The old theme switcher saved every axis; a saved axis would override
    // the pins in theme.config.ts, so they are cleared once.
    if (!store.getItem("ghr-theme-v2")) {
      for (var i = store.length - 1; i >= 0; i--) {
        var key = store.key(i)
        if (key && key.indexOf("theme-") === 0) store.removeItem(key)
      }
      store.setItem("ghr-theme-v2", "1")
    }
    mode = store.getItem("mode")
  } catch (e) {
    mode = null
  }
  var dark =
    mode === "system" ? window.matchMedia("(prefers-color-scheme: dark)").matches : mode !== "light"
  root.setAttribute("data-mode", dark ? "dark" : "light")

  // The attribute-driven pins from theme.config.ts that change the first
  // paint; ThemeProvider sets the rest when React mounts.
  var pinned = {
    preset: "default",
    "background-style": "solid",
    density: "compact",
    "font-size": "medium",
    radius: "subtle",
    elevation: "low",
    "button-elevation": "flat",
    "control-depth": "flush",
    "shell-style": "classic",
    "sidebar-active-bar": "ring",
  }
  for (var axis in pinned) root.setAttribute("data-" + axis, pinned[axis])
})()
