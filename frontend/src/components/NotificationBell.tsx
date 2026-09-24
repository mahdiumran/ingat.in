import { useEffect, useRef, useState } from 'react'
import { NotificationFeedItem, notificationsApi } from '../api'
import { formatWIB } from './ui'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

const TYPE_ICON: Record<string, string> = {
  task: 'task_alt',
  reminder: 'alarm',
  rfs: 'event_available',
  incident: 'report',
  request: 'support_agent',
  change: 'build',
}

const EVENT_LABEL: Record<string, string> = {
  created: 'dibuat',
  updated: 'diperbarui',
  status_changed: 'berubah status',
  commented: 'komentar baru',
  notified: 'notifikasi dikirim',
  notify_failed: 'notifikasi gagal',
  closed: 'ditutup',
  resolved: 'selesai',
  reopened: 'dibuka ulang',
  cancelled: 'dibatalkan',
}

/** warnaIkon memilih warna titik sesuai jenis event. */
function iconTone(eventType: string): string {
  if (eventType.includes('fail') || eventType === 'cancelled') return 'text-critical'
  if (eventType.includes('breach') || eventType === 'reopened') return 'text-warning'
  if (eventType === 'created') return 'text-primary'
  if (eventType === 'resolved' || eventType === 'closed') return 'text-success'
  return 'text-text-secondary'
}

/**
 * NotificationBell (F13) — lonceng pada topbar dengan panel daftar aktivitas.
 *
 * Sumber data adalah feed work_item_events (satu-satunya catatan append-only
 * yang mencakup pembuatan, perubahan status, komentar, dan kegagalan kirim).
 * Posisi baca terakhir disimpan per-pengguna di server sehingga jumlah "belum
 * dibaca" konsisten antar perangkat.
 *
 * URL/posisi baca disimpan idempoten: panel di-fetch saat dibuka dan berkala
 * setiap 30 detik agar tetap segar tanpa membebani server.
 */
export function NotificationBell({
  onOpenItem,
  toast,
}: {
  onOpenItem: (itemId: string) => void
  toast: Toast
}) {
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<NotificationFeedItem[]>([])
  const [lastSeen, setLastSeen] = useState<number>(() => {
    const raw = localStorage.getItem('ingatin_notif_last_seen')
    return raw ? Number(raw) || 0 : 0
  })
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const wrapRef = useRef<HTMLDivElement>(null)

  async function load() {
    setLoading(true)
    setError('')
    try {
      const res = await notificationsApi.list({ limit: '40' })
      setItems(res.notifications)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Gagal memuat notifikasi')
    } finally {
      setLoading(false)
    }
  }

  // Muat saat pertama & berkala.
  useEffect(() => {
    load()
    const timer = window.setInterval(load, 30_000)
    return () => window.clearInterval(timer)
  }, [])

  // Tutup bila klik di luar.
  useEffect(() => {
    function onClick(e: MouseEvent) {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) setOpen(false)
    }
    if (open) document.addEventListener('mousedown', onClick)
    return () => document.removeEventListener('mousedown', onClick)
  }, [open])

  const unread = items.filter((n) => n.id > lastSeen).length

  async function toggle() {
    const next = !open
    setOpen(next)
    if (next) {
      await load()
      // Tandai seluruh yang tampil sebagai sudah dibaca.
      const maxID = items.reduce((m, n) => Math.max(m, n.id), 0)
      if (maxID > lastSeen) {
        try {
          await notificationsApi.markRead(maxID)
        } catch {
          /* penandaan bersifat best-effort */
        }
        setLastSeen(maxID)
        localStorage.setItem('ingatin_notif_last_seen', String(maxID))
      }
    }
  }

  function handleClick(n: NotificationFeedItem) {
    setOpen(false)
    onOpenItem(n.work_item_id)
  }

  async function markAllRead() {
    const maxID = items.reduce((m, n) => Math.max(m, n.id), 0)
    if (maxID <= 0) return
    try {
      await notificationsApi.markRead(maxID)
      setLastSeen(maxID)
      localStorage.setItem('ingatin_notif_last_seen', String(maxID))
      toast('success', 'Semua notifikasi ditandai dibaca')
    } catch (err) {
      toast('error', 'Gagal menandai', err instanceof Error ? err.message : undefined)
    }
  }

  return (
    <div className="relative" ref={wrapRef}>
      <button
        onClick={toggle}
        className="btn-ghost relative h-9 w-9 px-0"
        title="Notifikasi"
        aria-label={`Notifikasi${unread > 0 ? `, ${unread} belum dibaca` : ''}`}
      >
        <span className="material-symbols-outlined text-[20px]">
          {unread > 0 ? 'notifications_active' : 'notifications'}
        </span>
        {unread > 0 && (
          <span className="absolute -right-0.5 -top-0.5 flex h-4 min-w-[16px] items-center justify-center rounded-full bg-error px-1 text-[10px] font-bold text-on-error">
            {unread > 99 ? '99+' : unread}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute right-0 top-11 z-40 w-[min(400px,calc(100vw-24px))] overflow-hidden rounded-modal border border-border bg-surface-container-lowest shadow-modal">
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <div>
              <div className="font-headline text-body-md font-semibold">Notifikasi</div>
              <div className="text-label-sm text-text-secondary">
                {unread > 0 ? `${unread} belum dibaca` : 'semua sudah dibaca'}
              </div>
            </div>
            <div className="flex gap-1">
              <button className="btn-ghost h-7 w-7 px-0" title="Muat ulang" onClick={load}>
                <span className="material-symbols-outlined text-[16px]">refresh</span>
              </button>
              <button className="btn-ghost h-7 px-2 text-label-sm" onClick={markAllRead}>
                Tandai dibaca
              </button>
            </div>
          </div>

          <div className="max-h-[420px] overflow-y-auto">
            {loading && items.length === 0 && (
              <div className="flex items-center justify-center gap-2 py-10 text-body-sm text-text-secondary">
                <span className="material-symbols-outlined animate-spin text-[18px]">progress_activity</span>
                Memuat…
              </div>
            )}
            {error && <div className="px-4 py-6 text-center text-body-sm text-critical">{error}</div>}
            {!loading && !error && items.length === 0 && (
              <div className="px-4 py-10 text-center text-body-sm text-text-secondary">
                Belum ada aktivitas.
              </div>
            )}

            {items.map((n) => {
              const isUnread = n.id > lastSeen
              return (
                <button
                  key={n.id}
                  onClick={() => handleClick(n)}
                  className={`flex w-full items-start gap-3 border-b border-border px-4 py-2.5 text-left transition hover:bg-surface-container ${
                    isUnread ? 'bg-primary-container/10' : ''
                  }`}
                >
                  <span className={`material-symbols-outlined mt-0.5 text-[18px] ${iconTone(n.event_type)}`}>
                    {TYPE_ICON[n.item_type] ?? 'notifications'}
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="mono text-label-sm text-text-secondary">{n.ref_no}</span>
                      <span className="badge border-outline-variant bg-surface-container text-[10px] text-text-secondary">
                        {EVENT_LABEL[n.event_type] ?? n.event_type.replace(/_/g, ' ')}
                      </span>
                      {isUnread && <span className="h-2 w-2 rounded-full bg-primary" aria-label="belum dibaca" />}
                    </div>
                    <div className="mt-0.5 truncate text-body-sm font-medium">{n.title}</div>
                    <div className="mono text-label-sm text-text-secondary">
                      {formatWIB(n.created_at)} · {n.actor_username || 'sistem'}
                    </div>
                  </div>
                </button>
              )
            })}
          </div>

          <div className="border-t border-border px-4 py-2 text-center text-label-sm text-text-secondary">
            Aktivitas todo, reminder, RFS & tiket
          </div>
        </div>
      )}
    </div>
  )
}
