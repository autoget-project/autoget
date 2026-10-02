// Applies the stored theme before the app renders, so the first paint already
// uses the right theme. It is an external file (not an inline script) so the
// Content-Security-Policy can forbid inline scripts entirely.
(function () {
  try {
    const darkThemes = ["dark", "frappe", "macchiato", "mocha"];
    const validThemes = ["light", "dark", "latte", "frappe", "macchiato", "mocha"];
    let theme = localStorage.getItem("autoget-theme");
    if (!theme || !validThemes.includes(theme)) {
      theme =
        window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches
          ? "dark"
          : "light";
    }
    document.documentElement.setAttribute("data-theme", theme);
    if (darkThemes.includes(theme)) {
      document.documentElement.classList.add("dark");
    } else {
      document.documentElement.classList.remove("dark");
    }
  } catch {
    // Ignore: a blocked localStorage must not stop the app from rendering.
  }
})();
