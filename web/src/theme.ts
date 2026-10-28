// Console colour theme. Dark is the default (payment-admin UI kit); the light
// theme mirrors it. The choice is remembered per browser.
export type ConsoleTheme = "dark" | "light";

const storageKey = "goepay-console-theme";

export function storedTheme(
  storage: Pick<Storage, "getItem"> | undefined = browserStorage(),
): ConsoleTheme {
  try {
    return storage?.getItem(storageKey) === "light" ? "light" : "dark";
  } catch {
    return "dark";
  }
}

export function applyTheme(
  theme: ConsoleTheme,
  root: HTMLElement | undefined = typeof document === "undefined"
    ? undefined
    : document.documentElement,
): void {
  if (root) root.dataset.theme = theme;
}

export function saveTheme(
  theme: ConsoleTheme,
  storage: Pick<Storage, "setItem"> | undefined = browserStorage(),
): void {
  try {
    storage?.setItem(storageKey, theme);
  } catch {
    // Storage can be unavailable (private mode, policy); the theme still applies.
  }
}

function browserStorage(): Storage | undefined {
  try {
    return typeof localStorage === "undefined" ? undefined : localStorage;
  } catch {
    return undefined;
  }
}
