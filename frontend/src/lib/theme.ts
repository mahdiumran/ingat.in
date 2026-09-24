/**
 * Tema (F28) — Light / Dark / System.
 *
 * Nilai warna sebenarnya ada di src/styles.css (`:root` = light, `.dark` = dark).
 * File ini hanya mengatur mode & menerapkan kelas di <html>.
 *
 *  - mode disimpan di localStorage['ingatin_theme']
 *  - default: 'system' (ikut prefers-color-scheme OS)
 *  - anti-FOUC: script inline di index.html menerapkan kelas sebelum React mount
 */
import { useEffect, useState } from 'react'

export type ThemeMode = 'light' | 'dark' | 'system'
export type ResolvedTheme = 'light' | 'dark'

export const THEME_KEY = 'ingatin_theme'
const DARK_MQ = '(prefers-color-scheme: dark)'

/** getStoredTheme membaca mode tersimpan; 'system' bila tidak ada/tidak valid. */
export function getStoredTheme(): ThemeMode {
  if (typeof window === 'undefined') return 'system'
  const raw = window.localStorage.getItem(THEME_KEY)
  return raw === 'light' || raw === 'dark' || raw === 'system' ? raw : 'system'
}

/** systemPrefersDark membaca preferensi OS saat ini. */
export function systemPrefersDark(): boolean {
  if (typeof window === 'undefined' || !window.matchMedia) return false
  return window.matchMedia(DARK_MQ).matches
}

/** resolveTheme mengubah mode menjadi tema konkret ('light' | 'dark'). */
export function resolveTheme(mode: ThemeMode): ResolvedTheme {
  if (mode === 'system') return systemPrefersDark() ? 'dark' : 'light'
  return mode
}

/** applyTheme men-set kelas di <html> + color-scheme (scrollbar/form native). */
export function applyTheme(mode: ThemeMode): ResolvedTheme {
  const resolved = resolveTheme(mode)
  if (typeof document !== 'undefined') {
    const el = document.documentElement
    el.classList.toggle('dark', resolved === 'dark')
    el.classList.toggle('light', resolved === 'light')
    el.style.colorScheme = resolved
  }
  return resolved
}

/** setThemeMode menyimpan mode & menerapkannya. */
export function setThemeMode(mode: ThemeMode): ResolvedTheme {
  if (typeof window !== 'undefined') window.localStorage.setItem(THEME_KEY, mode)
  return applyTheme(mode)
}

/**
 * useTheme mengelola mode tema. Mendengarkan perubahan preferensi OS saat
 * mode = 'system' agar tema ikut berubah tanpa reload.
 */
export function useTheme(): {
  mode: ThemeMode
  resolved: ResolvedTheme
  setMode: (mode: ThemeMode) => void
} {
  const [mode, setMode] = useState<ThemeMode>(() => getStoredTheme())
  const [resolved, setResolved] = useState<ResolvedTheme>(() => resolveTheme(getStoredTheme()))

  // Terapkan saat mode berubah.
  useEffect(() => {
    setResolved(applyTheme(mode))
  }, [mode])

  // Ikuti perubahan OS bila mode = 'system'.
  useEffect(() => {
    if (mode !== 'system' || !window.matchMedia) return
    const mq = window.matchMedia(DARK_MQ)
    const onChange = () => setResolved(applyTheme('system'))
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [mode])

  return {
    mode,
    resolved,
    setMode: (next: ThemeMode) => {
      setThemeMode(next)
      setMode(next)
    },
  }
}
