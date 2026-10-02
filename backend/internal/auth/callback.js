// Runs on the OAuth callback page. The tokens and the post-login target are
// carried in data attributes on #tokens, so this file is static and can be
// served as a normal same-origin script (the CSP forbids inline scripts).
(function () {
  var el = document.getElementById("tokens");
  if (!el) {
    return;
  }
  var raw = el.dataset.tokens;
  try {
    JSON.parse(raw); // reject anything that is not valid JSON before storing it
    localStorage.setItem("autoget_auth", raw);
  } catch {
    /* fall through: go to the redirect target regardless */
  }
  window.location.replace(el.dataset.redirect || "/");
})();
