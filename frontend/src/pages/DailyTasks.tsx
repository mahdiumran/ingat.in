import { useEffect, useMemo, useState } from 'react'
import { ApiUser, dailyTaskApi, WorkItem, workItemsApi } from '../api'
import {
  ConfirmDialog,
  EmptyState,
  ErrorState,
  LoadingBlock,
  Modal,
  PageHeader,
  PriorityBadge,
  TagList,
  formatWIB,
} from '../components/ui'
import { useAsync, useDebounced, useTagColors } from '../hooks'
import { ItemDetail } from './WorkItemDetail'

export type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

type ConfirmState = {
  title: string
  body?: string
  confirmLabel?: string
  tone?: 'primary' | 'danger'
  icon?: string
  run: () => Promise<void>
}

/** todayWIB mengembalikan tanggal hari ini menurut zona WIB (YYYY-MM-DD). */
export function todayWIB(): string {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Jakarta',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date())
  return parts
}

/** shiftDayWIB menggeser tanggal (YYYY-MM-DD) sejumlah hari pada zona WIB. */
function shiftDayWIB(date: string, days: number): string {
  const d = new Date(`${date}T00:00:00+07:00`)
  d.setUTCDate(d.getUTCDate() + days)
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Jakarta',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(d)
}

/** dayLabel menampilkan tanggal harian dalam format Indonesia yang ramah. */
function dayLabel(date: string): string {
  const d = new Date(`${date}T00:00:00+07:00`)
  return new Intl.DateTimeFormat('id-ID', {
    timeZone: 'Asia/Jakarta',
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  }).format(d)
}

/** Kolom Kanban Daily Task (urutan sesuai workflow). */
const COLUMNS: { status: string; label: string; hint: string; icon: string; accent: string }[] = [
  {
    status: 'pending',
    label: 'Belum Selesai',
    hint: 'Task yang belum dikerjakan',
    icon: 'radio_button_unchecked',
    accent: 'border-t-outline-variant',
  },
  {
    status: 'in_progress',
    label: 'Sedang Dikerjakan',
    hint: 'Sedang berjalan',
    icon: 'progress_activity',
    accent: 'border-t-primary',
  },
  {
    status: 'done',
    label: 'Selesai',
    hint: 'Sudah tuntas hari ini',
    icon: 'check_circle',
    accent: 'border-t-success',
  },
]

/**
 * DailyTasks (F17) — checklist harian bergaya todo list.
 *
 * Menampilkan daily task sebagai papan 3 kolom (Belum Selesai / Sedang
 * Dikerjakan / Selesai) dengan penambahan task langsung per kolom. Task belum
 * selesai dari hari sebelumnya ("carry-over") tetap tampil dengan badge
 * tanggal agar tidak terlupa.
 */
export default function DailyTasks({
  user,
  toast,
  focusItemId,
  onFocusConsumed,
}: {
  user: ApiUser | null
  toast: Toast
  focusItemId?: string | null
  onFocusConsumed?: () => void
}) {
  const [date, setDate] = useState<string>(() => todayWIB())
  const [search, setSearch] = useState('')
  const [owner, setOwner] = useState('')
  const [carryOver, setCarryOver] = useState(true)
  const [detailID, setDetailID] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)
  const [busyID, setBusyID] = useState<string | null>(null)
  // addingStatus = kolom yang sedang dibuka form tambah cepatnya.
  const [addingStatus, setAddingStatus] = useState<string | null>(null)
  const [quickTitle, setQuickTitle] = useState('')
  const [quickBusy, setQuickBusy] = useState(false)
  const [showFullCreate, setShowFullCreate] = useState(false)

  const tagColors = useTagColors()

  useEffect(() => {
    if (focusItemId) {
      setDetailID(focusItemId)
      onFocusConsumed?.()
    }
  }, [focusItemId, onFocusConsumed])

  const debouncedSearch = useDebounced(search)
  const isToday = date === todayWIB()

  const items = useAsync(
    () =>
      dailyTaskApi.listForDay(date, {
        ...(owner ? { owner } : {}),
        ...(debouncedSearch ? { q: debouncedSearch } : {}),
        carryOver,
      }),
    [date, owner, debouncedSearch, carryOver],
  )

  const rows = items.data?.items ?? []
  const canWrite = !!user && user.role !== 'viewer'
  const isAdmin = user?.role === 'admin'

  function canManage(item: WorkItem): boolean {
    if (isAdmin) return true
    const u = (user?.username ?? '').toLowerCase()
    if (!u) return false
    return item.created_by?.toLowerCase() === u || item.owner_username?.toLowerCase() === u
  }

  const grouped = useMemo(() => {
    const map: Record<string, WorkItem[]> = { pending: [], in_progress: [], done: [], canceled: [] }
    for (const it of rows) {
      if (map[it.status]) map[it.status].push(it)
      else map.pending.push(it)
    }
    // Prioritas lebih tinggi tampil lebih dulu dalam kolom.
    const rank: Record<string, number> = { critical: 0, high: 1, normal: 2, low: 3 }
    for (const k of Object.keys(map)) {
      map[k].sort((a, b) => (rank[a.priority] ?? 9) - (rank[b.priority] ?? 9))
    }
    return map
  }, [rows])

  const counts = {
    total: rows.filter((r) => r.status !== 'canceled').length,
    done: grouped.done.length,
  }
  const progressPct = counts.total > 0 ? Math.round((counts.done / counts.total) * 100) : 0

  /** moveStatus memindahkan task ke status lain (tombol cepat / geser kolom). */
  async function moveStatus(item: WorkItem, status: string) {
    if (item.status === status) return
    setBusyID(item.id)
    try {
      await workItemsApi.changeStatus(item.id, status)
      toast('success', 'Status diperbarui', `${item.ref_no} → ${status}`)
      items.reload()
    } catch (err) {
      toast('error', 'Gagal mengubah status', err instanceof Error ? err.message : undefined)
    } finally {
      setBusyID(null)
    }
  }

  /** quickAdd menambah task cepat pada sebuah kolom. */
  async function quickAdd(status: string) {
    const title = quickTitle.trim()
    if (!title) return
    setQuickBusy(true)
    try {
      await dailyTaskApi.create({
        title,
        date,
        owner_username: status === 'in_progress' ? (user?.username ?? '') : (user?.username ?? ''),
        target_id: '',
        status,
      })
      setQuickTitle('')
      toast('success', 'Daily task ditambahkan', title)
      items.reload()
    } catch (err) {
      toast('error', 'Gagal menambah task', err instanceof Error ? err.message : undefined)
    } finally {
      setQuickBusy(false)
    }
  }

  function handleDelete(item: WorkItem) {
    setConfirm({
      title: 'Hapus daily task?',
      body: `Task ${item.ref_no} — "${item.title}" akan dihapus. Tindakan ini tidak dapat dibatalkan.`,
      tone: 'danger',
      confirmLabel: 'Hapus',
      run: async () => {
        await workItemsApi.remove(item.id)
        toast('success', 'Task dihapus', item.ref_no)
        items.reload()
      },
    })
  }

  if (detailID) {
    return (
      <ItemDetail
        id={detailID}
        user={user}
        toast={toast}
        onBack={() => {
          setDetailID(null)
          items.reload()
        }}
      />
    )
  }

  return (
    <div>
      <PageHeader
        kicker="Operasional"
        title="Daily Task"
        description="Daftar task yang dikerjakan hari ini — tambah langsung dari kolom, tandai selesai, otomatis membawa task lama yang belum selesai."
        actions={
          <>
            <button className="btn-secondary" onClick={items.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            {canWrite && (
              <button className="btn-primary" onClick={() => setShowFullCreate(true)}>
                <span className="material-symbols-outlined text-[18px]">add_task</span>
                Daily Task Baru
              </button>
            )}
          </>
        }
      />

      {/* Kontrol hari + filter */}
      <section className="card mb-5 p-4">
        <div className="flex flex-wrap items-end gap-3">
          <div className="flex items-center gap-2">
            <button
              className="btn-secondary h-10 w-10 px-0"
              title="Hari sebelumnya"
              onClick={() => setDate((d) => shiftDayWIB(d, -1))}
            >
              <span className="material-symbols-outlined text-[18px]">chevron_left</span>
            </button>
            <div>
              <label className="label-field" htmlFor="dt-date">
                Tanggal
              </label>
              <input
                id="dt-date"
                type="date"
                className="input"
                value={date}
                onChange={(e) => setDate(e.target.value || todayWIB())}
              />
            </div>
            <button
              className="btn-secondary h-10 w-10 px-0"
              title="Hari berikutnya"
              onClick={() => setDate((d) => shiftDayWIB(d, 1))}
            >
              <span className="material-symbols-outlined text-[18px]">chevron_right</span>
            </button>
            {!isToday && (
              <button className="btn-secondary self-end" onClick={() => setDate(todayWIB())}>
                Hari ini
              </button>
            )}
          </div>

          <div className="min-w-[180px] flex-1">
            <label className="label-field" htmlFor="dt-search">
              Cari
            </label>
            <input
              id="dt-search"
              className="input"
              placeholder="judul, ref…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>

          <div className="min-w-[160px]">
            <label className="label-field" htmlFor="dt-owner">
              Owner
            </label>
            <input
              id="dt-owner"
              className="input mono"
              placeholder="username"
              value={owner}
              onChange={(e) => setOwner(e.target.value)}
            />
          </div>

          <label className="flex items-center gap-2 self-end pb-2 text-label-md text-text-secondary">
            <input
              type="checkbox"
              className="h-4 w-4"
              checked={carryOver}
              onChange={(e) => setCarryOver(e.target.checked)}
            />
            Bawa task belum selesai (carry-over)
          </label>
        </div>

        <div className="mt-3 flex flex-wrap items-center gap-3 border-t border-border pt-3">
          <div className="kicker">{dayLabel(date)}{isToday ? ' • HARI INI' : ''}</div>
          <div className="flex flex-1 items-center gap-2">
            <div className="h-2 min-w-[80px] flex-1 overflow-hidden rounded-full bg-surface-container">
              <div className="h-full rounded-full bg-success transition-all" style={{ width: `${progressPct}%` }} />
            </div>
            <span className="whitespace-nowrap text-label-sm font-semibold text-text-secondary">
              {counts.done}/{counts.total} selesai ({progressPct}%)
            </span>
          </div>
        </div>
      </section>

      {items.loading && <LoadingBlock label="Memuat daily task…" />}
      {items.error && (
        <div className="p-4">
          <ErrorState message={items.error} onRetry={items.reload} />
        </div>
      )}

      {!items.loading && !items.error && (
        <div className="grid gap-4 lg:grid-cols-3">
          {COLUMNS.map((col) => {
            const list = grouped[col.status] ?? []
            const adding = addingStatus === col.status
            return (
              <section key={col.status} className={`card flex flex-col border-t-4 ${col.accent}`}>
                <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
                  <div className="flex min-w-0 items-center gap-2">
                    <span className="material-symbols-outlined text-[20px] text-text-secondary">{col.icon}</span>
                    <div className="min-w-0">
                      <h3 className="truncate font-headline text-base font-semibold">{col.label}</h3>
                      <p className="truncate text-label-sm text-text-secondary">{col.hint}</p>
                    </div>
                  </div>
                  <span className="rounded-full bg-surface-container px-2 py-0.5 text-label-sm font-bold text-text-secondary">
                    {list.length}
                  </span>
                </div>

                <div className="flex flex-1 flex-col gap-2 p-3">
                  {list.length === 0 && !adding && (
                    <p className="px-1 py-4 text-center text-label-sm text-text-secondary">
                      {carryOver && !isToday ? 'Tidak ada task.' : 'Belum ada task di kolom ini.'}
                    </p>
                  )}

                  {list.map((it) => {
                    const carried = !!it.start_at && it.start_at.slice(0, 10) < date
                    return (
                      <article
                        key={it.id}
                        className="group rounded-control border border-border bg-surface-container-lowest p-3 transition-shadow hover:shadow-sm"
                      >
                        <div className="flex items-start justify-between gap-2">
                          <button
                            className="flex-1 text-left"
                            onClick={() => setDetailID(it.id)}
                            title="Buka detail"
                          >
                            <div className="flex items-center gap-2">
                              <span className="mono text-label-sm text-text-secondary">{it.ref_no}</span>
                              {carried && (
                                <span
                                  className="rounded-full bg-warning-container px-1.5 py-0.5 text-[10px] font-bold uppercase text-on-warning-container"
                                  title={`Dari ${formatWIB(it.start_at)}`}
                                >
                                  Terlambat
                                </span>
                              )}
                            </div>
                            <p className={`mt-1 text-body-md ${it.status === 'done' ? 'text-text-secondary line-through' : ''}`}>
                              {it.title}
                            </p>
                            {it.device_ref && (
                              <span className="mono text-label-sm text-text-secondary">{it.device_ref}</span>
                            )}
                          </button>
                          <PriorityBadge priority={it.priority} />
                        </div>

                        {it.tags?.length > 0 && (
                          <div className="mt-2">
                            <TagList tags={it.tags} colors={tagColors} />
                          </div>
                        )}

                        {it.owner_username && (
                          <div className="mt-2 flex items-center gap-1 text-label-sm text-text-secondary">
                            <span className="material-symbols-outlined text-[15px]">person</span>
                            {it.owner_username}
                          </div>
                        )}

                        {canWrite && (
                          <div className="mt-3 flex flex-wrap items-center gap-1 border-t border-border pt-2">
                            {col.status !== 'pending' && (
                              <button
                                className="btn-ghost h-7 px-1.5 text-label-sm"
                                disabled={busyID === it.id}
                                title="Kembalikan ke Belum Selesai"
                                onClick={() => moveStatus(it, 'pending')}
                              >
                                <span className="material-symbols-outlined text-[16px]">undo</span>
                                Belum
                              </button>
                            )}
                            {col.status !== 'in_progress' && col.status !== 'done' && (
                              <button
                                className="btn-ghost h-7 px-1.5 text-label-sm"
                                disabled={busyID === it.id}
                                title="Tandai sedang dikerjakan"
                                onClick={() => moveStatus(it, 'in_progress')}
                              >
                                <span className="material-symbols-outlined text-[16px]">play_arrow</span>
                                Kerjakan
                              </button>
                            )}
                            {col.status !== 'done' && (
                              <button
                                className="btn-ghost h-7 px-1.5 text-label-sm text-success"
                                disabled={busyID === it.id}
                                title="Tandai selesai"
                                onClick={() => moveStatus(it, 'done')}
                              >
                                <span className="material-symbols-outlined text-[16px]">check_circle</span>
                                Selesai
                              </button>
                            )}
                            {canManage(it) && (
                              <button
                                className="btn-ghost ml-auto h-7 w-7 px-0 text-critical"
                                title="Hapus"
                                onClick={() => handleDelete(it)}
                              >
                                <span className="material-symbols-outlined text-[16px]">delete</span>
                              </button>
                            )}
                          </div>
                        )}
                      </article>
                    )
                  })}

                  {/* Tambah task cepat langsung dari kolom */}
                  {canWrite && col.status !== 'done' && (
                    <div className="mt-auto pt-1">
                      {adding ? (
                        <form
                          className="flex items-center gap-1.5"
                          onSubmit={(e) => {
                            e.preventDefault()
                            void quickAdd(col.status)
                          }}
                        >
                          <input
                            className="input h-9 flex-1 py-0"
                            autoFocus
                            placeholder="Judul task…"
                            value={quickTitle}
                            onChange={(e) => setQuickTitle(e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === 'Escape') {
                                setAddingStatus(null)
                                setQuickTitle('')
                              }
                            }}
                          />
                          <button
                            type="submit"
                            className="btn-primary h-9 w-9 px-0"
                            disabled={quickBusy || !quickTitle.trim()}
                            title="Simpan"
                          >
                            <span className="material-symbols-outlined text-[18px]">
                              {quickBusy ? 'progress_activity' : 'check'}
                            </span>
                          </button>
                          <button
                            type="button"
                            className="btn-ghost h-9 w-9 px-0"
                            onClick={() => {
                              setAddingStatus(null)
                              setQuickTitle('')
                            }}
                            title="Batal"
                          >
                            <span className="material-symbols-outlined text-[18px]">close</span>
                          </button>
                        </form>
                      ) : (
                        <button
                          className="flex w-full items-center justify-center gap-1.5 rounded-control border border-dashed border-outline-variant py-2 text-label-md text-text-secondary hover:border-primary hover:text-primary"
                          onClick={() => {
                            setAddingStatus(col.status)
                            setQuickTitle('')
                          }}
                        >
                          <span className="material-symbols-outlined text-[18px]">add</span>
                          Tambah task
                        </button>
                      )}
                    </div>
                  )}
                </div>
              </section>
            )
          })}
        </div>
      )}

      {!items.loading && !items.error && counts.total === 0 && (
        <div className="mt-4">
          <EmptyState
            icon="checklist"
            title="Belum ada daily task"
            body={
              debouncedSearch || owner
                ? 'Tidak ada task yang cocok dengan filter.'
                : 'Tambahkan task dari kolom, atau buat lewat tombol "Daily Task Baru".'
            }
          />
        </div>
      )}

      {showFullCreate && (
        <DailyTaskForm
          date={date}
          onClose={() => setShowFullCreate(false)}
          onSaved={() => {
            setShowFullCreate(false)
            toast('success', 'Daily task dibuat', 'Notifikasi dikirim ke target.')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membuat task', msg)}
        />
      )}

      <ConfirmDialog
        open={!!confirm}
        title={confirm?.title ?? ''}
        body={confirm?.body}
        confirmLabel={confirm?.confirmLabel}
        tone={confirm?.tone}
        icon={confirm?.icon}
        busy={confirmBusy}
        onCancel={() => setConfirm(null)}
        onConfirm={async () => {
          if (!confirm) return
          setConfirmBusy(true)
          try {
            await confirm.run()
            setConfirm(null)
          } catch (err) {
            toast('error', 'Aksi gagal', err instanceof Error ? err.message : undefined)
            setConfirm(null)
          } finally {
            setConfirmBusy(false)
          }
        }}
      />
    </div>
  )
}

/* ------------------------------------------------------------------------- */

/** DailyTaskForm — form lengkap pembuatan daily task (judul, owner, prioritas). */
function DailyTaskForm({
  date,
  onClose,
  onSaved,
  onError,
}: {
  date: string
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [priority, setPriority] = useState('normal')
  const [owner, setOwner] = useState('')
  const [tags, setTags] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await dailyTaskApi.create({
        title,
        description,
        priority,
        owner_username: owner,
        date,
        tags: tags
          ? tags
              .split(',')
              .map((t) => t.trim())
              .filter(Boolean)
          : [],
      })
      onSaved()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Terjadi kesalahan')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title="Daily Task Baru"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !title.trim()}>
            {busy ? 'Menyimpan…' : 'Buat Task'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <div>
          <label className="label-field" htmlFor="dtf-title">
            Judul
          </label>
          <input
            id="dtf-title"
            className="input"
            required
            autoFocus
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="mis. Cek tiket gangguan pelanggan"
          />
        </div>
        <div>
          <label className="label-field" htmlFor="dtf-desc">
            Deskripsi
          </label>
          <textarea
            id="dtf-desc"
            className="input h-20 py-2"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Konteks atau langkah tambahan…"
          />
        </div>
        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="dtf-priority">
              Prioritas
            </label>
            <select
              id="dtf-priority"
              className="input"
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
            >
              <option value="low">Low</option>
              <option value="normal">Normal</option>
              <option value="high">High</option>
              <option value="critical">Critical</option>
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="dtf-owner">
              Owner
            </label>
            <input
              id="dtf-owner"
              className="input mono"
              value={owner}
              onChange={(e) => setOwner(e.target.value)}
              placeholder="username"
            />
          </div>
        </div>
        <div>
          <label className="label-field" htmlFor="dtf-tags">
            Tag
          </label>
          <input
            id="dtf-tags"
            className="input"
            value={tags}
            onChange={(e) => setTags(e.target.value)}
            placeholder="piket, monitoring (pisahkan dengan koma)"
          />
          <p className="mt-1 text-label-sm text-text-secondary">Tanggal: {dayLabel(date)}</p>
        </div>
      </form>
    </Modal>
  )
}
