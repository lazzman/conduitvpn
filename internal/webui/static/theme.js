/* Shared three-way theme control for the login and management pages. */
"use strict";

(() => {
  const storageKey = "conduit-theme";
  const themes = ["system", "dark", "light"];
  const titles = { system: "主题：跟随系统", dark: "主题：深色", light: "主题：浅色" };
  const darkQuery = window.matchMedia("(prefers-color-scheme: dark)");
  let theme = localStorage.getItem(storageKey) || "system";
  if (!themes.includes(theme)) theme = "system";

  function effectiveTheme() {
    return theme === "system" ? (darkQuery.matches ? "dark" : "light") : theme;
  }

  function applyTheme() {
    const effective = effectiveTheme();
    document.documentElement.dataset.theme = effective;
    const colorScheme = document.querySelector('meta[name="color-scheme"]');
    if (colorScheme) colorScheme.content = theme === "system" ? "dark light" : effective;
  }

  function updateControl(control) {
    const button = control.querySelector("[data-theme-button]");
    if (!button) return;
    button.title = titles[theme];
    control.querySelectorAll("[data-theme-icon]").forEach((icon) => {
      icon.hidden = icon.dataset.themeIcon !== theme;
    });
    control.querySelectorAll(".theme-option").forEach((option) => {
      const selected = option.dataset.theme === theme;
      option.classList.toggle("active", selected);
      option.setAttribute("aria-checked", String(selected));
    });
  }

  function closeMenu(control) {
    const button = control.querySelector("[data-theme-button]");
    const menu = control.querySelector("[data-theme-menu]");
    if (!button || !menu) return;
    menu.hidden = true;
    button.setAttribute("aria-expanded", "false");
  }

  function notifyThemeChange() {
    document.dispatchEvent(new Event("conduit-theme-change"));
  }

  function initControls() {
    const control = document.querySelector("[data-theme-control]");
    if (!control || control.dataset.themeReady === "true") return;
    const button = control.querySelector("[data-theme-button]");
    const menu = control.querySelector("[data-theme-menu]");
    if (!button || !menu) return;

    control.dataset.themeReady = "true";
    updateControl(control);
    button.addEventListener("click", (event) => {
      event.stopPropagation();
      menu.hidden = !menu.hidden;
      button.setAttribute("aria-expanded", String(!menu.hidden));
    });
    menu.addEventListener("click", (event) => {
      const option = event.target.closest(".theme-option");
      if (!option || !control.contains(option)) return;
      theme = option.dataset.theme;
      localStorage.setItem(storageKey, theme);
      applyTheme();
      updateControl(control);
      closeMenu(control);
      notifyThemeChange();
    });
    document.addEventListener("click", (event) => {
      if (!control.contains(event.target)) closeMenu(control);
    });
    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape") closeMenu(control);
    });
    darkQuery.addEventListener("change", () => {
      if (theme !== "system") return;
      applyTheme();
      updateControl(control);
      notifyThemeChange();
    });
  }

  window.ConduitTheme = { initControls };
  applyTheme();
})();
