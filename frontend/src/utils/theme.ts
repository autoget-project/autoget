export type ThemeName = "light" | "dark" | "latte" | "frappe" | "macchiato" | "mocha";

export interface ThemeOption {
  id: ThemeName;
  label: string;
  isDark: boolean;
}

export const THEMES: ThemeOption[] = [
  { id: "light", label: "Light", isDark: false },
  { id: "dark", label: "Dark", isDark: true },
  { id: "latte", label: "Catppuccin Latte", isDark: false },
  { id: "frappe", label: "Catppuccin Frappé", isDark: true },
  { id: "macchiato", label: "Catppuccin Macchiato", isDark: true },
  { id: "mocha", label: "Catppuccin Mocha", isDark: true },
];

const THEME_STORAGE_KEY = "autoget-theme";

export function getStoredTheme(): ThemeName {
  if (typeof window === "undefined") {
    return "light";
  }
  const saved = localStorage.getItem(THEME_STORAGE_KEY) as ThemeName | null;
  if (saved && THEMES.some((t) => t.id === saved)) {
    return saved;
  }
  if (window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches) {
    return "dark";
  }
  return "light";
}

export function applyTheme(theme: ThemeName): void {
  if (typeof document === "undefined") {
    return;
  }
  document.documentElement.setAttribute("data-theme", theme);
  const themeMeta = THEMES.find((t) => t.id === theme);
  if (themeMeta?.isDark) {
    document.documentElement.classList.add("dark");
  } else {
    document.documentElement.classList.remove("dark");
  }
  localStorage.setItem(THEME_STORAGE_KEY, theme);
  window.dispatchEvent(new CustomEvent("autoget-theme-changed", { detail: { theme } }));
}

export function initTheme(): ThemeName {
  const current = getStoredTheme();
  applyTheme(current);
  return current;
}
