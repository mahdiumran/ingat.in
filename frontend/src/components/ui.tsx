import { ReactNode, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { tagClass } from '../design/tokens'

/* ---------------------------------------------------------------------------
   Toast — notifikasi ringkas (sukses/gagal/info)
   --------------------------------------------------------------------------- */

export type ToastKind = 'success' | 'error' | 'info' | 'warning'
export type Toast = { id: number; kind: ToastKind; title: string; body?: string }

const toastTone: Record<ToastKind, string> = {
  success: 'border-outline-variant bg-success-container text-on-success-container',
  error: 'border-critical bg-critical-container text-on-critical-container',
  warning: 'border-outline-variant bg-warning-container text-on-warning-container',
  info: 'border-outline-variant bg-info-container text-on-info-container',
}

const toastIcon: Record<ToastKind, string> = {
  success: 'check_circle',
  error: 'error',
  warning: 'warning',
  info: 'info',
}

export function ToastStack({ toasts, onDismiss }: { toasts: Toast[]; onDismiss: (id: number) => void }) {
  if (toasts.length === 0) return null
  return (
    <div className="pointer-events-none fixed right-3 top-3 z-[60] flex w-[min(360px,calc(100vw-24px))] flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          role="status"
          className={`pointer-events-auto flex items-start gap-2.5 rounded-card border p-3 shadow-raised ${toastTone[t.kind]}`}
        >
          <span className="material-symbols-outlined text-[18px] shrink-0">{toastIcon[t.kind]}</span>
          <div className="min-w-0 flex-1">
            <div className="text-body-sm font-semibold">{t.title}</div>
            {t.body && <div className="mt-0.5 text-body-sm opacity-90">{t.body}</div>}
          </div>
          <button
            onClick={() => onDismiss(t.id)}
            className="shrink-0 opacity-70 hover:opacity-100"
            aria-label="Tutup notifikasi"
          >
            <span className="material-symbols-outlined text-[16px]">close</span>
          </button>
        </div>
      ))}
    </div>
  )
}

/* ---------------------------------------------------------------------------
   Badge status & prioritas
   --------------------------------------------------------------------------- */

export function StatusBadge({ status, tone }: { status: string; tone?: string }) {
  const label = status.replace(/_/g, ' ')
  return (
    <span className={`badge ${tone ?? 'bg-surface-container text-text-secondary border-outline-variant'}`}>
      {label}
    </span>
  )
}

export function PriorityBadge({ priority }: { priority: string }) {
  const tone =
    priority === 'critical'
      ? 'bg-critical-container text-on-critical-container border-critical'
      : priority === 'high'
        ? 'bg-warning-container text-on-warning-container border-outline-variant'
        : priority === 'low'
          ? 'bg-surface-container text-text-secondary border-outline-variant'
          : 'bg-info-container text-on-info-container border-outline-variant'
  return <span className={`badge ${tone}`}>{priority}</span>
}

/**
 * StatusSelect — dropdown ubah status untuk section "Rubah Status".
 *
 * Opsi diambil dari `options` (state workflow tipe item). Bila `value` tidak
 * ada di daftar (mis. state legacy), opsi itu tetap ditampilkan sebagai
 * pilihan pertama agar tidak menyesatkan.
 */
export function StatusSelect({
  value,
  options,
  disabled = false,
  busy = false,
  onChange,
}: {
  value: string
  options: string[]
  disabled?: boolean
  busy?: boolean
  onChange: (next: string) => void
}) {
  const all = options.includes(value) ? options : [value, ...options]
  return (
    <select
      className="input h-8 w-auto min-w-[150px] py-0 text-label-md"
      value={value}
      disabled={disabled || busy}
      title="Ubah status"
      onChange={(e) => {
        const next = e.target.value
        if (next !== value) onChange(next)
      }}
    >
      {all.map((s) => (
        <option key={s} value={s}>
          {s.replace(/_/g, ' ')}
        </option>
      ))}
    </select>
  )
}

/* ---------------------------------------------------------------------------
   Tag berwarna
   --------------------------------------------------------------------------- */

/**
 * TagBadge menampilkan tag dengan warna. `colors` memetakan nama tag ke warna
 * (dari Master Data kind `tag_color`); tag tanpa pemetaan memakai warna abu.
 */
export function TagBadge({ tag, colors }: { tag: string; colors?: Record<string, string> }) {
  const color = colors?.[tag] ?? colors?.[tag.toLowerCase()] ?? 'gray'
  return (
    <span className={`badge border ${tagClass(color)}`} title={`tag: ${tag}`}>
      {tag}
    </span>
  )
}

/** TagList menampilkan daftar tag berwarna. */
export function TagList({
  tags,
  colors,
  className = '',
}: {
  tags: string[]
  colors?: Record<string, string>
  className?: string
}) {
  if (!tags || tags.length === 0) return null
  return (
    <div className={`flex flex-wrap gap-1.5 ${className}`}>
      {tags.map((t) => (
        <TagBadge key={t} tag={t} colors={colors} />
      ))}
    </div>
  )
}

/* ---------------------------------------------------------------------------
   Modal konfirmasi aksi (profesional)
   --------------------------------------------------------------------------- */

/**
 * ConfirmDialog — konfirmasi sebelum aksi berisiko (hapus, tutup, tandai aktif).
 *
 * Mengikuti DESIGN.md §7: judul ringkas, penjelasan konsekuensi, tombol Batal
 * dan tombol aksi (merah untuk aksi merusak).
 */
export function ConfirmDialog({
  open,
  title,
  body,
  confirmLabel = 'Lanjutkan',
  cancelLabel = 'Batal',
  tone = 'primary',
  icon,
  busy = false,
  onConfirm,
  onCancel,
}: {
  open: boolean
  title: string
  body?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  tone?: 'primary' | 'danger'
  icon?: string
  busy?: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  if (!open) return null
  const iconName = icon ?? (tone === 'danger' ? 'delete' : 'help')
  const iconWrap =
    tone === 'danger'
      ? 'bg-critical-container text-on-critical-container'
      : 'bg-info-container text-on-info-container'

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center p-3">
      <div className="absolute inset-0 bg-slate-950/60 backdrop-blur-sm" onClick={busy ? undefined : onCancel} aria-hidden />
      <div
        role="alertdialog"
        aria-modal="true"
        aria-label={title}
        className="relative w-full max-w-[440px] rounded-modal border border-border bg-surface-container-lowest shadow-modal"
      >
        <div className="flex items-start gap-3 p-5">
          <span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-full ${iconWrap}`}>
            <span className="material-symbols-outlined text-[22px]">{iconName}</span>
          </span>
          <div className="min-w-0">
            <h2 className="font-headline text-lg font-semibold">{title}</h2>
            {body && <div className="mt-1 text-body-sm text-text-secondary">{body}</div>}
          </div>
        </div>
        <div className="flex justify-end gap-2 border-t border-border px-5 py-3.5">
          <button className="btn-secondary" onClick={onCancel} disabled={busy}>
            {cancelLabel}
          </button>
          <button
            className={tone === 'danger' ? 'btn-danger' : 'btn-primary'}
            onClick={onConfirm}
            disabled={busy}
          >
            {busy ? 'Memproses…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}

/* ---------------------------------------------------------------------------
   State: loading / empty / error
   --------------------------------------------------------------------------- */

export function LoadingBlock({ label = 'Memuat…' }: { label?: string }) {
  return (
    <div className="flex items-center justify-center gap-2 py-12 text-body-sm text-text-secondary">
      <span className="material-symbols-outlined animate-spin text-[20px]">progress_activity</span>
      {label}
    </div>
  )
}

export function SkeletonRows({ rows = 5 }: { rows?: number }) {
  return (
    <div className="space-y-2 p-4">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="skeleton h-9 w-full" />
      ))}
    </div>
  )
}

export function EmptyState({
  icon = 'inbox',
  title,
  body,
  action,
}: {
  icon?: string
  title: string
  body?: string
  action?: ReactNode
}) {
  return (
    <div className="flex flex-col items-center gap-2.5 px-6 py-14 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-xl border border-accent-soft bg-success-container/40">
        <span className="material-symbols-outlined text-[24px] text-primary">{icon}</span>
      </div>
      <h3 className="font-headline text-lg font-semibold">{title}</h3>
      {body && <p className="max-w-md text-body-sm text-text-secondary">{body}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div
      role="alert"
      className="flex flex-col items-center gap-2.5 rounded-card border border-critical/30 bg-critical-container/50 px-6 py-10 text-center"
    >
      <span className="material-symbols-outlined text-[26px] text-critical">error</span>
      <div className="font-medium text-on-critical-container">{message}</div>
      {onRetry && (
        <button onClick={onRetry} className="btn-secondary mt-1">
          <span className="material-symbols-outlined text-[17px]">refresh</span>
          Coba lagi
        </button>
      )}
    </div>
  )
}

/* ---------------------------------------------------------------------------
   Modal
   --------------------------------------------------------------------------- */

export function Modal({
  title,
  onClose,
  children,
  footer,
  width = 'md',
}: {
  title: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  width?: 'sm' | 'md' | 'lg'
}) {
  const widthClass = width === 'sm' ? 'max-w-[420px]' : width === 'lg' ? 'max-w-[760px]' : 'max-w-[560px]'
  return (
    <div className="fixed inset-0 z-[55] flex items-start justify-center overflow-y-auto p-3 sm:items-center">
      <div className="absolute inset-0 bg-slate-950/60 backdrop-blur-sm" onClick={onClose} aria-hidden />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className={`relative w-full ${widthClass} rounded-modal border border-border bg-surface-container-lowest shadow-modal`}
      >
        <div className="flex items-center justify-between gap-3 border-b border-border px-5 py-3.5">
          <h2 className="font-headline text-lg font-semibold">{title}</h2>
          <button onClick={onClose} className="btn-ghost h-8 w-8 px-0" aria-label="Tutup">
            <span className="material-symbols-outlined text-[20px]">close</span>
          </button>
        </div>
        <div className="px-5 py-4">{children}</div>
        {footer && (
          <div className="flex justify-end gap-2 border-t border-border px-5 py-3.5">{footer}</div>
        )}
      </div>
    </div>
  )
}

/* ---------------------------------------------------------------------------
   Kartu statistik
   --------------------------------------------------------------------------- */

export function StatCard({
  icon,
  label,
  value,
  note,
  toneClass = 'bg-surface-container text-text-secondary border-outline-variant',
}: {
  icon: string
  label: string
  value: string | number
  note?: string
  toneClass?: string
}) {
  return (
    <div className="card card-pad">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="kicker">{label}</div>
          <div className="mt-1 truncate font-headline text-headline-md font-semibold">{value}</div>
          {note && <div className="mt-0.5 truncate text-body-sm text-text-secondary">{note}</div>}
        </div>
        <span className={`badge ${toneClass} shrink-0`}>
          <span className="material-symbols-outlined text-[14px]">{icon}</span>
        </span>
      </div>
    </div>
  )
}

/* ---------------------------------------------------------------------------
   Page header
   --------------------------------------------------------------------------- */

export function PageHeader({
  kicker,
  title,
  description,
  actions,
}: {
  kicker: string
  title: string
  description?: string
  actions?: ReactNode
}) {
  return (
    <div className="mb-5 flex flex-wrap items-end justify-between gap-3">
      <div className="min-w-0">
        <div className="kicker">{kicker}</div>
        <h2 className="mt-0.5 font-headline text-headline-md font-semibold">{title}</h2>
        {description && <p className="mt-1 text-body-sm text-text-secondary">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  )
}

export type ActionMenuItem = {
  label: string
  icon: string
  onClick: () => void
  disabled?: boolean
  tone?: 'default' | 'warning' | 'success' | 'danger'
  /** Tooltip opsional, mis. alasan sebuah aksi dinonaktifkan. */
  title?: string
}

export function ActionMenu({ items, label = 'Penanganan' }: { items: ActionMenuItem[]; label?: string }) {
  const [open, setOpen] = useState(false)
  const [position, setPosition] = useState({ top: 0, left: 0, maxHeight: 320 })
  const ref = useRef<HTMLDivElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return

    const updatePosition = () => {
      const trigger = ref.current?.getBoundingClientRect()
      if (!trigger) return
      const menuWidth = Math.max(190, menuRef.current?.offsetWidth ?? 190)
      const menuHeight = menuRef.current?.offsetHeight ?? Math.min(items.length * 40 + 8, 320)
      const margin = 8
      const gap = 4
      const spaceBelow = window.innerHeight - trigger.bottom - margin
      const spaceAbove = trigger.top - margin
      const placeAbove = spaceBelow < Math.min(menuHeight, 180) && spaceAbove > spaceBelow
      const maxHeight = Math.max(120, placeAbove ? spaceAbove - gap : spaceBelow - gap)
      const top = placeAbove
        ? Math.max(margin, trigger.top - Math.min(menuHeight, maxHeight) - gap)
        : trigger.bottom + gap
      const left = Math.min(
        Math.max(margin, trigger.right - menuWidth),
        window.innerWidth - menuWidth - margin,
      )
      setPosition({ top, left, maxHeight })
    }

    const closeOutside = (event: MouseEvent) => {
      const target = event.target as Node
      if (!ref.current?.contains(target) && !menuRef.current?.contains(target)) setOpen(false)
    }
    const closeEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }

    updatePosition()
    const frame = window.requestAnimationFrame(updatePosition)
    document.addEventListener('mousedown', closeOutside)
    document.addEventListener('keydown', closeEscape)
    window.addEventListener('resize', updatePosition)
    window.addEventListener('scroll', updatePosition, true)
    return () => {
      window.cancelAnimationFrame(frame)
      document.removeEventListener('mousedown', closeOutside)
      document.removeEventListener('keydown', closeEscape)
      window.removeEventListener('resize', updatePosition)
      window.removeEventListener('scroll', updatePosition, true)
    }
  }, [open, items.length])

  const tones = {
    default: 'text-text-secondary hover:bg-surface-container hover:text-text-primary',
    warning: 'text-warning hover:bg-warning-container/40',
    success: 'text-success hover:bg-success-container/40',
    danger: 'text-critical hover:bg-critical-container/40',
  }

  const menu = open ? (
    <div
      ref={menuRef}
      role="menu"
      style={{ top: position.top, left: position.left, maxHeight: position.maxHeight }}
      className="fixed z-[80] min-w-[190px] overflow-y-auto rounded-card border border-border bg-surface-container-lowest py-1 shadow-modal"
    >
      {items.map((item, index) => (
        <button
          key={`${item.label}-${index}`}
          type="button"
          role="menuitem"
          disabled={item.disabled}
          title={item.title}
          onClick={() => {
            setOpen(false)
            item.onClick()
          }}
          className={`flex w-full items-center gap-2.5 px-3 py-2 text-left text-body-sm disabled:cursor-not-allowed disabled:opacity-45 ${tones[item.tone ?? 'default']}`}
        >
          <span className="material-symbols-outlined text-[18px]">{item.icon}</span>
          <span>{item.label}</span>
        </button>
      ))}
    </div>
  ) : null

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        className="btn-secondary h-8 gap-1 px-2 text-label-sm"
        onClick={() => setOpen((value) => !value)}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        {label}
        <span className="material-symbols-outlined text-[17px]">arrow_drop_down</span>
      </button>
      {menu && createPortal(menu, document.body)}
    </div>
  )
}

/* ---------------------------------------------------------------------------
   Format waktu (WIB)
   --------------------------------------------------------------------------- */

const WIB = 'Asia/Jakarta'

export function formatWIB(value?: string | null, withSeconds = false): string {
  if (!value) return '—'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return String(value)
  return new Intl.DateTimeFormat('id-ID', {
    timeZone: WIB,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    ...(withSeconds ? { second: '2-digit' } : {}),
    hour12: false,
  }).format(d)
}
