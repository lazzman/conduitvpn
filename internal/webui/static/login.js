"use strict";

(() => {
  window.ConduitTheme.initControls();
  const form = document.getElementById("login_form");
  const btn = document.getElementById("submit_btn");
  const err = document.getElementById("login_err");
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    btn.disabled = true;
    err.hidden = true;
    try {
      const response = await fetch("./api/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username: form.username.value, password: form.password.value }),
      });
      const data = await response.json().catch(() => ({}));
      if (data.ok) {
        location.href = "./";
        return;
      }
      err.textContent = window.ConduitI18n.errorMessage(data);
      err.hidden = false;
    } catch (_) {
      err.textContent = window.ConduitI18n.t("errors.network");
      err.hidden = false;
    } finally {
      btn.disabled = false;
    }
  });
})();
