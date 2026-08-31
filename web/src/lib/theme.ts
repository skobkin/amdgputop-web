export type ThemePreference = 'auto' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

export const THEME_STORAGE_KEY = 'amdgputop-web:theme';

// Keep the pre-paint script in index.html in sync with these rules.
export function isValidTheme(value: string | null | undefined): value is ThemePreference {
  return value === 'auto' || value === 'light' || value === 'dark';
}

export function resolveTheme(pref: ThemePreference): ResolvedTheme {
  if (pref !== 'auto') {
    return pref;
  }
  if (typeof window === 'undefined' || !window.matchMedia) {
    return 'dark';
  }
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

export function applyTheme(pref: ThemePreference): ResolvedTheme {
  const resolved = resolveTheme(pref);
  document.documentElement.dataset.theme = resolved;
  return resolved;
}

export function readStoredTheme(): ThemePreference {
  if (typeof window === 'undefined') {
    return 'auto';
  }
  try {
    const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
    if (isValidTheme(stored)) {
      return stored;
    }
  } catch {
    // Ignore storage errors and use fallback.
  }
  return 'auto';
}

// Canvas-rendered content (uPlot) cannot reference CSS variables directly, so
// resolve the current value at draw time and re-render on theme changes.
export function getCssVar(name: string, fallback = ''): string {
  if (typeof window === 'undefined' || !window.getComputedStyle) {
    return fallback;
  }
  const value = window.getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return value || fallback;
}
