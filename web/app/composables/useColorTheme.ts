import { parseTheme, resolveTheme, THEME_KEY } from '~/lib/theme'
import type { ThemeName } from '~/lib/theme'

function stored(): string | null {
  try {
    return localStorage.getItem(THEME_KEY)
  }
  catch {
    return null
  }
}

/**
 * Light/dark theme. The page is server-rendered, so the theme is only known after mount: `restore` reads the stored
 * choice or the system preference. Without an explicit choice, m3-tokens.css follows the system through CSS and the
 * data-theme attribute stays unset. The toggle writes the choice to the html element and to localStorage.
 */
export function useColorTheme() {
  const theme = useState<ThemeName>('grus.theme', () => 'light')
  const isDark = computed(() => theme.value === 'dark')

  function apply(next: ThemeName) {
    theme.value = next
    document.documentElement.dataset.theme = next
  }
  function toggle() {
    const next: ThemeName = isDark.value ? 'light' : 'dark'
    apply(next)
    try {
      localStorage.setItem(THEME_KEY, next)
    }
    catch { /* storage unavailable: the choice just does not persist */ }
  }
  /** Called once after mount, in the browser only. */
  function restore() {
    const choice = parseTheme(stored())
    if (choice) {
      apply(choice)
      return
    }
    theme.value = resolveTheme(null, window.matchMedia('(prefers-color-scheme: dark)').matches)
  }
  return { theme, isDark, toggle, restore }
}
