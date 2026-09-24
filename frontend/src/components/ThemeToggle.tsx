/**
 * ThemeToggle (F28) — pemilih tema Light / Dark / System.
 *
 * Ditempatkan di topbar, di sebelah kanan tombol notifikasi.
 * Nilai default & persistensi diatur oleh lib/theme.ts.
 */
import { useEffect, useRef, useState } from 'react'
import { ThemeMode, useTheme } from '../lib/theme'

const OPTIONS: { value: ThemeMode; label: string; icon: string }[] = [
  { value: 'light', label: 'Terang', icon: 'light_mode' },
  { value: 'dark', label: 'Gelap', icon: 'dark_mode' },
  { value: 'system', label: 'Sistem', icon: 'brightness_auto' },
]

export function ThemeToggle() {
  const { mode, resolved, setMode } = useTheme()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement | null>(null)

  // Tutup saat klik di luar / Escape.
  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const current = OPTIONS.find((o) => o.value === mode) ?? OPTIONS[2]
  const icon = mode === 'system' ? current.icon : resolved === 'dark' ? 'dark_mode' : 'light_mode'

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="btn-ghost h-9 w-9 px-0"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`Tema: ${current.label}`}
        title={`Tema: ${current.label}`}
      >
        <span className="material-symbols-outlined text-[20px]">{icon}</span>
      </button>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-11 z-40 w-40 overflow-hidden rounded-card border border-border bg-surface-container-lowest py-1 shadow-modal"
        >
          <div className="kicker px-3 pb-1 pt-1.5">Tema</div>
          {OPTIONS.map((o) => {
            const active = o.value === mode
            return (
              <button
                key={o.value}
                role="menuitemradio"
                aria-checked={active}
                onClick={() => {
                  setMode(o.value)
                  setOpen(false)
                }}
                className={`flex w-full items-center gap-2.5 px-3 py-1.5 text-body-sm ${
                  active
                    ? 'bg-primary-container/40 font-semibold text-on-primary-container'
                    : 'text-text-secondary hover:bg-surface-container hover:text-text-primary'
                }`}
              >
                <span className="material-symbols-outlined text-[18px]">{o.icon}</span>
                <span className="flex-1 text-left">{o.label}</span>
                {active && <span className="material-symbols-outlined text-[16px]">check</span>}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
