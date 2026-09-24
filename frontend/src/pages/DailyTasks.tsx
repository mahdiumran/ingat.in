import { useEffect, useMemo, useState } from 'react'
import { api, ApiUser, dailySummaryApi, dailyTaskApi, DailySummarySettings, WorkItem, workItemsApi } from '../api'
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
    status: 'waiting_customer',
    label: 'Menunggu Konfirmasi Pelanggan',
    hint: 'Menunggu balasan pelanggan',
    icon: 'hourglass_top',
    accent: 'border-t-warning',
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
  const [teamFilter, setTeamFilter] = useState('')
  const [carryOver, setCarryOver] = useState(true)
  const [detailID, setDetailID] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)
  const [busyID, setBusyID] = useState<string | null>(null)
  // Dialog konfirmasi "Selesai" + kolom keterangan penyelesaian.
  const [doneTarget, setDoneTarget] = useState<WorkItem | null>(null)
  const [doneNote, setDoneNote] = useState('')
  const [doneBusy, setDoneBusy] = useState(false)
  // F24: hasil penyelesaian — "normal" atau "bermasalah" (+ tiket).
  const [doneResult, setDoneResult] = useState<'normal' | 'bermasalah'>('normal')
  const [doneTicketType, setDoneTicketType] = useState<'incident' | 'request'>('incident')
  const [doneEscalate, setDoneEscalate] = useState(false)
  // F24: daftar jenis Daily Task (master data) untuk dropdown + label badge.
  const [dailyTypes, setDailyTypes] = useState<{ code: string; label: string; canTicket: boolean }[]>([])
  // addingStatus = kolom yang sedang dibuka form tambah cepatnya.
  const [addingStatus, setAddingStatus] = useState<string | null>(null)
  const [quickTitle, setQuickTitle] = useState('')
  const [quickType, setQuickType] = useState('')
  const [quickBusy, setQuickBusy] = useState(false)
  const [showFullCreate, setShowFullCreate] = useState(false)
  // F22: panel ringkasan notifikasi (konfigurasi pengiriman + pratinjau).
  const [showSummary, setShowSummary] = useState(false)

  const tagColors = useTagColors()

  // F31: tim — untuk filter (admin) & pemilih tim saat membuat task.
  const teams = useAsync(() => api.teams(), [])
  const teamList = teams.data?.teams ?? []
  const teamName = (id?: string) => teamList.find((t) => t.id === id)?.name
  const isAdminUser = user?.role === 'admin'

  // F24: muat daftar jenis Daily Task sekali (untuk dropdown + label badge).
  useEffect(() => {
    let alive = true
    dailyTaskApi
      .types()
      .then((t) => {
        if (alive) setDailyTypes(t)
      })
      .catch(() => {
        /* best-effort: dropdown kosong bila master data belum siap */
      })
    return () => {
      alive = false
    }
  }, [])

  const dailyTypeLabel = (code?: string) => dailyTypes.find((t) => t.code === code)?.label ?? code ?? ''

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
        ...(teamFilter ? { teamId: teamFilter } : {}),
        ...(debouncedSearch ? { q: debouncedSearch } : {}),
        carryOver,
      }),
    [date, owner, teamFilter, debouncedSearch, carryOver],
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

  /**
   * canEscalate (F24) — boleh mengekskalasi task menjadi tiket bila admin,
   * pembuat/owner, atau anggota tim yang sama. Kasus "role sama" ditegakkan di
   * server; tombol tetap ditampilkan agar pengguna dengan peran sama bisa
   * mencoba (server memvalidasi).
   */
  function canEscalate(item: WorkItem): boolean {
    if (canManage(item)) return true
    if (user?.team_id && item.team_id && user.team_id === item.team_id) return true
    return canWrite
  }

  const grouped = useMemo(() => {
    const map: Record<string, WorkItem[]> = {
      pending: [],
      in_progress: [],
      waiting_customer: [],
      done: [],
      canceled: [],
    }
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
  const overdueCount = rows.filter(
    (r) => !!r.due_at && r.status !== 'done' && r.status !== 'canceled' && new Date(r.due_at).getTime() < Date.now(),
  ).length

  /** moveStatus memindahkan task ke status lain (tombol cepat / geser kolom). */
  async function moveStatus(item: WorkItem, status: string, note = '') {
    if (item.status === status && !note) return
    setBusyID(item.id)
    try {
      await workItemsApi.changeStatus(item.id, status, note)
      toast('success', 'Status diperbarui', `${item.ref_no} → ${status}`)
      items.reload()
    } catch (err) {
      toast('error', 'Gagal mengubah status', err instanceof Error ? err.message : undefined)
    } finally {
      setBusyID(null)
    }
  }

  /**
   * requestDone membuka dialog konfirmasi + keterangan sebelum menandai task
   * selesai. Keterangan ikut tersimpan dan tampil di spreadsheet.
   */
  function requestDone(item: WorkItem) {
    setDoneTarget(item)
    setDoneNote('')
    setDoneResult('normal')
    setDoneTicketType('incident')
    setDoneEscalate(false)
  }

  /**
   * confirmDone menyelesaikan task. Bila hasil = "bermasalah" dan jenis task
   * mengizinkan pembuatan tiket, task dieskalasi menjadi tiket (incident/request)
   * sekaligus (F24). Keterangan ikut tersimpan + tampil di spreadsheet.
   */
  async function confirmDone() {
    if (!doneTarget) return
    if (doneResult === 'bermasalah' && doneEscalate) {
      setDoneBusy(true)
      try {
        const ticket = await workItemsApi.escalateTicket(doneTarget.id, {
          ticket_type: doneTicketType,
          note: doneNote.trim(),
        })
        toast('success', 'Tiket dibuat dari task', `${doneTarget.ref_no} → ${ticket.ref_no}`)
        setDoneTarget(null)
        setDoneNote('')
        items.reload()
      } catch (err) {
        toast('error', 'Gagal membuat tiket', err instanceof Error ? err.message : undefined)
      } finally {
        setDoneBusy(false)
      }
      return
    }
    setDoneBusy(true)
    try {
      await workItemsApi.changeStatus(
        doneTarget.id,
        'done',
        doneNote.trim(),
        doneResult === 'bermasalah' ? 'bermasalah' : 'normal',
      )
      toast(
        'success',
        doneResult === 'bermasalah' ? 'Task selesai (bermasalah)' : 'Task selesai',
        doneTarget.ref_no,
      )
      setDoneTarget(null)
      setDoneNote('')
      items.reload()
    } catch (err) {
      toast('error', 'Gagal menandai selesai', err instanceof Error ? err.message : undefined)
    } finally {
      setDoneBusy(false)
    }
  }

  /** quickAdd menambah task cepat pada sebuah kolom. */
  async function quickAdd(status: string) {
    const title = quickTitle.trim()
    if (!title) return
    if (!quickType) {
      toast('error', 'Jenis wajib dipilih', 'Pilih jenis Daily Task sebelum menyimpan.')
      return
    }
    setQuickBusy(true)
    try {
      await dailyTaskApi.create({
        title,
        date,
        owner_username: status === 'in_progress' ? (user?.username ?? '') : (user?.username ?? ''),
        target_id: '',
        status,
        daily_task_type: quickType,
        // F31: admin memakai tim yang sedang difilter; non-admin dipaksa server.
        ...(isAdminUser && teamFilter ? { team_id: teamFilter } : {}),
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
              <button className="btn-secondary" onClick={() => setShowSummary(true)}>
                <span className="material-symbols-outlined text-[18px]">notifications_active</span>
                Ringkasan Notifikasi
              </button>
            )}
            {canWrite && (
              <button className="btn-primary" onClick={() => setShowFullCreate(true)}>
                <span className="material-symbols-outlined text-[18px]">add_task</span>
                Daily Task Baru
              </button>
            )}
          </>
        }
      />

      {/* Ringkasan status hari ini */}
      <div className="mb-5 grid grid-cols-3 gap-3">
        <SummaryStatusCard label="Belum Selesai" value={grouped.pending.length} icon="radio_button_unchecked" tone="text-text-secondary" />
        <SummaryStatusCard label="Sedang Dikerjakan" value={grouped.in_progress.length} icon="progress_activity" tone="text-primary" />
        <SummaryStatusCard label="Menunggu Konfirmasi" value={grouped.waiting_customer.length} icon="hourglass_top" tone="text-warning" />
      </div>
      <div className="mb-5 grid grid-cols-2 gap-3">
        <SummaryStatusCard label="Selesai" value={grouped.done.length} icon="check_circle" tone="text-success" />
        <SummaryStatusCard label="Terlambat" value={overdueCount} icon="schedule" tone="text-critical" />
      </div>

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

          {isAdminUser && (
            <div className="min-w-[160px]">
              <label className="label-field" htmlFor="dt-team">
                Tim
              </label>
              <select id="dt-team" className="input" value={teamFilter} onChange={(e) => setTeamFilter(e.target.value)}>
                <option value="">Semua tim</option>
                {teamList.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))}
              </select>
            </div>
          )}

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
                    // Terlambat = lewat tenggat (due_at) dan belum selesai/dibatalkan.
                    const overdue =
                      !!it.due_at &&
                      it.status !== 'done' &&
                      it.status !== 'canceled' &&
                      new Date(it.due_at).getTime() < Date.now()
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
                              {isAdminUser && (
                                <span className="rounded-full bg-surface-container px-1.5 py-0.5 text-[10px] font-semibold uppercase text-text-secondary">
                                  {teamName(it.team_id) ?? 'Tanpa tim'}
                                </span>
                              )}
                              {it.task?.daily_task_type && (
                                <span className="rounded-full bg-primary-container px-1.5 py-0.5 text-[10px] font-bold uppercase text-on-primary-container">
                                  {dailyTypeLabel(it.task.daily_task_type)}
                                </span>
                              )}
                              {it.task?.result_status === 'bermasalah' && (
                                <span className="rounded-full bg-critical-container px-1.5 py-0.5 text-[10px] font-bold uppercase text-on-critical-container">
                                  Bermasalah
                                </span>
                              )}
                              {overdue && (
                                <span
                                  className="rounded-full bg-critical-container px-1.5 py-0.5 text-[10px] font-bold uppercase text-on-critical-container"
                                  title={it.due_at ? `Tenggat ${formatWIB(it.due_at)} sudah lewat` : 'Melewati tenggat'}
                                >
                                  Terlambat
                                </span>
                              )}
                              {!overdue && carried && (
                                <span
                                  className="rounded-full bg-warning-container px-1.5 py-0.5 text-[10px] font-bold uppercase text-on-warning-container"
                                  title={`Dari ${formatWIB(it.start_at)}`}
                                >
                                  Bawa-an
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

                        {it.updated_by_username && (
                          <div
                            className="mt-1 flex items-center gap-1 text-label-sm text-text-secondary"
                            title={`Diperbarui oleh ${it.updated_by_username}${it.updated_at ? ` · ${formatWIB(it.updated_at)}` : ''}`}
                          >
                            <span className="material-symbols-outlined text-[15px]">edit_note</span>
                            Diperbarui oleh {it.updated_by_username}
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
                            {col.status === 'in_progress' && (
                              <button
                                className="btn-ghost h-7 px-1.5 text-label-sm text-warning"
                                disabled={busyID === it.id}
                                title="Tandai menunggu konfirmasi pelanggan"
                                onClick={() => moveStatus(it, 'waiting_customer')}
                              >
                                <span className="material-symbols-outlined text-[16px]">hourglass_top</span>
                                Menunggu Konfirmasi Pelanggan
                              </button>
                            )}
                            {col.status === 'waiting_customer' && (
                              <button
                                className="btn-ghost h-7 px-1.5 text-label-sm"
                                disabled={busyID === it.id}
                                title="Kembali dikerjakan"
                                onClick={() => moveStatus(it, 'in_progress')}
                              >
                                <span className="material-symbols-outlined text-[16px]">play_arrow</span>
                                Lanjut Kerjakan
                              </button>
                            )}
                            {col.status !== 'done' && (
                              <button
                                className="btn-ghost h-7 px-1.5 text-label-sm text-success"
                                disabled={busyID === it.id}
                                title="Tandai selesai"
                                onClick={() => requestDone(it)}
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
                          className="flex flex-wrap items-center gap-1.5"
                          onSubmit={(e) => {
                            e.preventDefault()
                            void quickAdd(col.status)
                          }}
                        >
                          <select
                            className="input h-9 py-0"
                            value={quickType}
                            onChange={(e) => setQuickType(e.target.value)}
                            title="Jenis task"
                          >
                            <option value="">Jenis…</option>
                            {dailyTypes.map((t) => (
                              <option key={t.code} value={t.code}>
                                {t.label}
                              </option>
                            ))}
                          </select>
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
                            disabled={quickBusy || !quickTitle.trim() || !quickType}
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
                              setQuickType('')
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
                            setQuickType('')
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
          types={dailyTypes}
          user={user}
          teams={teamList}
          onClose={() => setShowFullCreate(false)}
          onSaved={() => {
            setShowFullCreate(false)
            toast('success', 'Daily task dibuat', 'Notifikasi dikirim ke target.')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membuat task', msg)}
        />
      )}

      {/* Konfirmasi penyelesaian + keterangan (keterangan ikut ke spreadsheet). */}
      {showSummary && <DailySummaryModal toast={toast} onClose={() => setShowSummary(false)} />}

      {doneTarget && (
        <Modal
          title="Tandai Selesai"
          onClose={() => setDoneTarget(null)}
          footer={
            <>
              <button className="btn-secondary" onClick={() => setDoneTarget(null)} disabled={doneBusy}>
                Batal
              </button>
              <button
                className={doneResult === 'bermasalah' ? 'btn-danger' : 'btn-primary'}
                onClick={confirmDone}
                disabled={doneBusy || (doneResult === 'bermasalah' && doneEscalate && !doneNote.trim())}
              >
                {doneBusy
                  ? 'Menyimpan…'
                  : doneResult === 'bermasalah' && doneEscalate
                    ? 'Selesai & Buat Tiket'
                    : 'Ya, Selesai'}
              </button>
            </>
          }
        >
          <div className="space-y-3.5">
            <div className="flex items-start gap-3 rounded-control border border-border bg-surface-container-low p-3">
              <span className="material-symbols-outlined text-[22px] text-success">check_circle</span>
              <div className="min-w-0">
                <div className="mono text-label-sm text-text-secondary">{doneTarget.ref_no}</div>
                <p className="font-medium">{doneTarget.title}</p>
              </div>
            </div>

            {/* F24: pilih hasil — Normal atau Bermasalah (memicu tiket). */}
            <div>
              <span className="label-field">Hasil penyelesaian</span>
              <div className="mt-1 grid grid-cols-2 gap-2">
                <button
                  type="button"
                  className={`flex items-center gap-2 rounded-control border px-3 py-2 text-left text-body-md ${
                    doneResult === 'normal'
                      ? 'border-success bg-success-container text-on-success-container'
                      : 'border-border bg-surface-container-lowest'
                  }`}
                  onClick={() => setDoneResult('normal')}
                >
                  <span className="material-symbols-outlined text-[20px] text-success">check_circle</span>
                  Normal
                </button>
                <button
                  type="button"
                  className={`flex items-center gap-2 rounded-control border px-3 py-2 text-left text-body-md ${
                    doneResult === 'bermasalah'
                      ? 'border-critical bg-critical-container text-on-critical-container'
                      : 'border-border bg-surface-container-lowest'
                  }`}
                  onClick={() => setDoneResult('bermasalah')}
                >
                  <span className="material-symbols-outlined text-[20px] text-critical">report</span>
                  Bermasalah (Gangguan)
                </button>
              </div>
            </div>

            <div>
              <label className="label-field" htmlFor="dt-done-note">
                {doneResult === 'bermasalah' ? 'Catatan gangguan' : 'Keterangan penyelesaian'}
              </label>
              <textarea
                id="dt-done-note"
                className="input h-24 py-2"
                autoFocus
                placeholder={
                  doneResult === 'bermasalah'
                    ? 'mis. Link down sejak 10:00, sudah dicek ke router pelanggan.'
                    : 'mis. Sudah dicek, gangguan clear. Tindak lanjut: monitor 1x24 jam.'
                }
                value={doneNote}
                onChange={(e) => setDoneNote(e.target.value)}
              />
              <p className="mt-1 text-label-sm text-text-secondary">
                {doneResult === 'bermasalah'
                  ? 'Catatan ini menjadi deskripsi tiket sekaligus keterangan penyelesaian task.'
                  : 'Opsional. Keterangan ini tersimpan pada task dan ikut tercatat di spreadsheet.'}
              </p>
            </div>

            {/* F24: opsi membuat tiket bila hasil = bermasalah. */}
            {doneResult === 'bermasalah' && canEscalate(doneTarget) && (
              <div className="rounded-control border border-critical/40 bg-critical-container/40 p-3">
                <label className="flex items-center gap-2 text-body-md font-medium">
                  <input
                    type="checkbox"
                    className="h-4 w-4"
                    checked={doneEscalate}
                    onChange={(e) => setDoneEscalate(e.target.checked)}
                  />
                  Buat tiket dari task ini
                </label>
                {doneEscalate && (
                  <div className="mt-2">
                    <label className="label-field" htmlFor="dt-done-ticket-type">
                      Tipe tiket
                    </label>
                    <select
                      id="dt-done-ticket-type"
                      className="input"
                      value={doneTicketType}
                      onChange={(e) => setDoneTicketType(e.target.value as 'incident' | 'request')}
                    >
                      <option value="incident">Incident (gangguan)</option>
                      <option value="request">Request (permintaan)</option>
                    </select>
                  </div>
                )}
              </div>
            )}
          </div>
        </Modal>
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
  types,
  user,
  teams = [],
  onClose,
  onSaved,
  onError,
}: {
  date: string
  types: { code: string; label: string; canTicket: boolean }[]
  user?: ApiUser | null
  teams?: { id: string; name: string }[]
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [priority, setPriority] = useState('normal')
  const [owner, setOwner] = useState('')
  const [dailyType, setDailyType] = useState('')
  const [teamId, setTeamId] = useState(user?.team_id ?? '')
  const [tags, setTags] = useState('')
  const [busy, setBusy] = useState(false)
  const isAdmin = user?.role === 'admin'

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!dailyType) {
      onError('Jenis Daily Task wajib dipilih.')
      return
    }
    setBusy(true)
    try {
      await dailyTaskApi.create({
        title,
        description,
        priority,
        owner_username: owner,
        date,
        daily_task_type: dailyType,
        ...(isAdmin && teamId ? { team_id: teamId } : {}),
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
          <button
            className="btn-primary"
            onClick={submit}
            disabled={busy || !title.trim() || !dailyType}
          >
            {busy ? 'Menyimpan…' : 'Buat Task'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <div>
          <label className="label-field" htmlFor="dtf-type">
            Jenis <span className="text-critical">*</span>
          </label>
          <select
            id="dtf-type"
            className="input"
            required
            value={dailyType}
            onChange={(e) => setDailyType(e.target.value)}
          >
            <option value="">— Pilih jenis —</option>
            {types.map((t) => (
              <option key={t.code} value={t.code}>
                {t.label}
                {t.canTicket ? ' (dapat membuat tiket)' : ''}
              </option>
            ))}
          </select>
        </div>
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
        {isAdmin && (
          <div>
            <label className="label-field" htmlFor="dtf-team">
              Tim
            </label>
            <select id="dtf-team" className="input" value={teamId} onChange={(e) => setTeamId(e.target.value)}>
              <option value="">— Tanpa tim —</option>
              {teams.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
            <p className="mt-1 text-label-sm text-text-secondary">
              Daily task hanya terlihat oleh tim yang dipilih (admin melihat semua).
            </p>
          </div>
        )}
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

/** SummaryStatusCard menampilkan jumlah tugas per status pada ringkasan atas. */
function SummaryStatusCard({
  label,
  value,
  icon,
  tone,
}: {
  label: string
  value: number
  icon: string
  tone: string
}) {
  return (
    <div className="card flex items-center gap-3 px-4 py-3">
      <span className={`material-symbols-outlined text-[24px] ${tone}`}>{icon}</span>
      <div className="min-w-0">
        <div className="kicker truncate">{label}</div>
        <div className="font-headline text-xl font-semibold">{value}</div>
      </div>
    </div>
  )
}

/**
 * DailySummaryModal mengatur pengiriman ringkasan tugas harian ke notifikasi
 * (Telegram/WhatsApp): mode pemicu (on-change / interval / off) + pratinjau.
 */
function DailySummaryModal({ toast, onClose }: { toast: Toast; onClose: () => void }) {
  const [mode, setMode] = useState<DailySummarySettings['mode']>('interval')
  const [interval, setInterval] = useState(60)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    dailySummaryApi
      .get()
      .then((s) => {
        setMode(s.mode)
        setInterval(s.interval_minutes || 60)
      })
      .catch(() => toast('error', 'Gagal memuat pengaturan ringkasan'))
      .finally(() => setLoading(false))
  }, [toast])

  async function save() {
    setBusy(true)
    try {
      await dailySummaryApi.save({ mode, interval_minutes: interval })
      toast('success', 'Pengaturan disimpan', 'Ringkasan tugas harian diperbarui.')
      onClose()
    } catch (err) {
      toast('error', 'Gagal menyimpan', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  async function preview() {
    setBusy(true)
    try {
      const res = await dailySummaryApi.preview()
      if (res.added > 0) {
        toast('success', 'Ringkasan dikirim', 'Cek kanal notifikasi (Telegram/WhatsApp).')
      } else {
        toast('warning', 'Tidak ada pesan baru', 'Binding aktif mungkin sudah menerima ringkasan ini.')
      }
    } catch (err) {
      toast('error', 'Gagal mengirim ringkasan', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  const MODES: { value: DailySummarySettings['mode']; label: string; note: string }[] = [
    { value: 'on_change', label: 'Saat ada perubahan', note: 'Kirim tiap kali status task berubah (pending/kerja/selesai).' },
    { value: 'interval', label: 'Berkala', note: 'Kirim otomatis setiap N menit.' },
    { value: 'off', label: 'Nonaktif', note: 'Tidak mengirim ringkasan otomatis.' },
  ]

  return (
    <Modal
      title="Ringkasan Notifikasi Tugas"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={preview} disabled={busy || loading}>
            <span className="material-symbols-outlined text-[18px]">send</span>
            Kirim Pratinjau
          </button>
          <button className="btn-primary" onClick={save} disabled={busy || loading}>
            {busy ? 'Menyimpan…' : 'Simpan'}
          </button>
        </>
      }
    >
      {loading ? (
        <LoadingBlock />
      ) : (
        <div className="space-y-3.5">
          <p className="text-body-sm text-text-secondary">
            Ringkasan berisi daftar tugas hari ini: <strong>belum selesai</strong>,{' '}
            <strong>sedang dikerjakan</strong>, dan <strong>selesai</strong> — dikirim ke kanal notifikasi
            (Telegram/WhatsApp) melalui target default.
          </p>
          <div className="space-y-2">
            {MODES.map((m) => (
              <label
                key={m.value}
                className={`flex cursor-pointer items-start gap-3 rounded-control border p-3 ${
                  mode === m.value ? 'border-primary bg-primary-container/20' : 'border-border'
                }`}
              >
                <input
                  type="radio"
                  className="mt-1"
                  name="ds-mode"
                  checked={mode === m.value}
                  onChange={() => setMode(m.value)}
                />
                <div>
                  <div className="font-medium">{m.label}</div>
                  <p className="text-label-sm text-text-secondary">{m.note}</p>
                </div>
              </label>
            ))}
          </div>
          {mode === 'interval' && (
            <div>
              <label className="label-field" htmlFor="ds-interval">
                Interval (menit)
              </label>
              <input
                id="ds-interval"
                type="number"
                min={5}
                max={1440}
                className="input mono"
                value={interval}
                onChange={(e) => setInterval(parseInt(e.target.value, 10) || 60)}
              />
              <p className="mt-1 text-label-sm text-text-secondary">Minimal 5, maksimal 1440 (24 jam).</p>
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}
