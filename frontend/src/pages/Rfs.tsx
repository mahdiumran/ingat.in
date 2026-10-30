import { useEffect, useState } from 'react'
import { ApiUser, api, MasterDataEntry, masterDataApi, rfsSummaryApi, RFSSummarySettings, workItemsApi, WorkItem } from '../api'
import {
  ActionMenu,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  LoadingBlock,
  Modal,
  PageHeader,
  StatusBadge,
  TagList,
  formatWIB,
} from '../components/ui'
import { statusLabel, statusTone } from '../design/tokens'
import { useAsync, useDebounced, useTagColors } from '../hooks'
import { rfsStartGate } from '../lib/rfs'
import { ItemDetail } from './WorkItemDetail'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

/** Konfirmasi aksi berisiko (ConfirmDialog). */
type ConfirmState = {
  title: string
  body?: string
  confirmLabel?: string
  tone?: 'primary' | 'danger'
  icon?: string
  run: () => Promise<void>
}

/** Hitung mundur ke tanggal RFS. Tidak berjalan lagi setelah ticket resolved
 *  (status activated/cancelled maupun tahap instalasi activated/cancelled). */
function countdown(rfsAt?: string, status?: string, installStage?: string): { text: string; tone: string } {
  const isClosed =
    status === 'activated' ||
    status === 'cancelled' ||
    installStage === 'activated' ||
    installStage === 'cancelled'
  if (!rfsAt || isClosed) return { text: '—', tone: 'text-text-secondary' }
  const ms = new Date(rfsAt).getTime() - Date.now()
  const late = ms < 0
  const abs = Math.abs(ms)
  const days = Math.floor(abs / 86_400_000)
  const hours = Math.floor((abs % 86_400_000) / 3_600_000)

  let text: string
  if (days > 0) text = `${days} hari`
  else if (hours > 0) text = `${hours} jam`
  else text = `${Math.floor(abs / 60_000)} menit`
  if (late) text += ' lewat'

  const tone = late ? 'text-critical font-semibold' : days <= 1 ? 'text-warning font-semibold' : 'text-success'
  return { text, tone }
}

/**
 * Rfs (F23) — menu "Aktivasi / EWO" (item_type = rfs).
 *
 * Field utama: Nama Customer, Product (jenis paket dari Master Data),
 * Priority, Bandwidth, Tanggal RFS, dan Tag (preset Change Service / Upgrade /
 * New / Request / Urgent). Aksi: In Progress, Troubleshoot (pending_troubleshoot
 * + catatan penanganan), Close Ticket (activated), Cancel, Delete (ber-alasan).
 */
export default function Rfs({
  user,
  toast,
  focusItemId,
  onFocusConsumed,
  autoCreateNonce,
  onAutoCreateConsumed,
}: {
  user: ApiUser | null
  toast: Toast
  focusItemId?: string | null
  onFocusConsumed?: () => void
  autoCreateNonce?: number | null
  onAutoCreateConsumed?: () => void
}) {
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [stage, setStage] = useState('')
  const [onlyNotActivated, setOnlyNotActivated] = useState(false)
  const [bucket, setBucket] = useState<'' | 'soon' | 'overdue' | 'closed'>('')
  const [offset, setOffset] = useState(0)
  const [showCreate, setShowCreate] = useState(false)
  const [showSummary, setShowSummary] = useState(false)
  const [editItem, setEditItem] = useState<string | null>(null)
  const [detailID, setDetailID] = useState<string | null>(null)
  const [notifyingID, setNotifyingID] = useState<string | null>(null)
  const [joiningID, setJoiningID] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)
  // F23: modal Troubleshoot (status + catatan penanganan).
  const [troubleTarget, setTroubleTarget] = useState<{ id: string; refNo: string } | null>(null)
  // F23: modal Cancel (alasan) & Delete (alasan wajib).
  const [cancelTarget, setCancelTarget] = useState<{ id: string; refNo: string } | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; refNo: string } | null>(null)
  const [busyID, setBusyID] = useState<string | null>(null)

  const tagColors = useTagColors()

  const isAdmin = user?.role === 'admin'
  const isSuper = !!user?.is_super || isAdmin
  const isManager = user?.role === 'manager'
  const isSales = user?.role === 'sales'

  /** canManage mengikuti aturan backend: admin, pembuat, atau owner. */
  function canManage(item: { created_by?: string; owner_username?: string }): boolean {
    if (isAdmin) return true
    const u = (user?.username ?? '').toLowerCase()
    if (!u) return false
    return item.created_by?.toLowerCase() === u || item.owner_username?.toLowerCase() === u
  }

  const myName = (user?.username ?? '').toLowerCase()

  /** isHandledByMe: saya sudah owner atau sudah menjadi penangan RFS. */
  function isHandledByMe(item: WorkItem): boolean {
    if (!myName) return false
    if (item.owner_username?.toLowerCase() === myName) return true
    return (item.collaborators ?? []).some((c) => c.username.toLowerCase() === myName)
  }

  /** canCancel mengikuti aturan backend: admin/manager, atau sales pembuat. */
  function canCancel(item: { created_by?: string }): boolean {
    if (isSuper || isManager) return true
    if (isSales) return (item.created_by ?? '').toLowerCase() === (user?.username ?? '').toLowerCase()
    return false
  }

  useEffect(() => {
    if (focusItemId) {
      setDetailID(focusItemId)
      onFocusConsumed?.()
    }
  }, [focusItemId, onFocusConsumed])

  // Buka form pembuatan saat Dashboard meminta (Quick Access).
  useEffect(() => {
    if (autoCreateNonce) {
      setShowCreate(true)
      onAutoCreateConsumed?.()
    }
  }, [autoCreateNonce, onAutoCreateConsumed])

  const debouncedSearch = useDebounced(search)
  const limit = 50

  const items = useAsync(
    () =>
      workItemsApi.list({
        type: 'rfs',
        ...(status ? { status } : {}),
        ...(debouncedSearch ? { q: debouncedSearch } : {}),
        order: 'expire_at',
        desc: 'false',
        limit: String(limit),
        offset: String(offset),
      }),
    [status, debouncedSearch, offset],
  )

  const all = items.data?.items ?? []
  // Data tambahan (maks. 500) untuk statistik lintas halaman.
  const statItems = useAsync(
    () => workItemsApi.list({ type: 'rfs', limit: '500', order: 'expire_at', desc: 'false' }),
    [items.data],
  )
  // RFS dianggap tertutup bila status ATAU tahap instalasinya activated/cancelled.
  const isClosedStatus = (s: string) => s === 'activated' || s === 'cancelled'
  const isClosedItem = (i: { status: string; rfs?: { install_stage?: string } }) =>
    isClosedStatus(i.status) ||
    i.rfs?.install_stage === 'activated' ||
    i.rfs?.install_stage === 'cancelled'
  // Completed/closed = activated, baik dari status maupun tahap instalasi.
  const isCompletedRFS = (i: { status: string; rfs?: { install_stage?: string } }) =>
    i.status === 'activated' || i.rfs?.install_stage === 'activated'

  const rows = all.filter((i) => {
    if (stage && i.rfs?.install_stage !== stage) return false
    if (onlyNotActivated && isClosedItem(i)) return false
    if (bucket === 'soon') {
      if (!i.expire_at || isClosedItem(i)) return false
      const diff = new Date(i.expire_at).getTime() - Date.now()
      if (diff < 0 || diff > 7 * 86_400_000) return false
    }
    if (bucket === 'overdue') {
      if (!i.expire_at || isClosedItem(i) || new Date(i.expire_at).getTime() >= Date.now()) return false
    }
    if (bucket === 'closed' && !isCompletedRFS(i)) return false
    return true
  })
  const total = items.data?.total ?? 0
  const canWrite = !!user && user.role !== 'viewer'
  const page = Math.floor(offset / limit) + 1
  const maxPage = Math.max(1, Math.ceil(total / limit))

  // Statistik dihitung dari seluruh RFS (bukan hanya halaman yang tampil),
  // agar angka "7 hari", "Terlewat", dan "Closed" tetap akurat.
  const statAll = statItems.data?.items ?? all
  const soon = statAll.filter((i) => {
    if (!i.expire_at || isClosedItem(i)) return false
    const diff = new Date(i.expire_at).getTime() - Date.now()
    return diff >= 0 && diff <= 7 * 86_400_000
  }).length

  const overdue = statAll.filter((i) => {
    if (!i.expire_at || isClosedItem(i)) return false
    return new Date(i.expire_at).getTime() < Date.now()
  }).length

  async function handleNotify(id: string, refNo: string) {
    setNotifyingID(id)
    try {
      const res = await workItemsApi.triggerNotification(id, { offset_label: 'MANUAL' })
      if (res.added > 0) toast('success', 'Notifikasi dikirim', `${refNo}: ${res.message}`)
      else toast('warning', 'Tidak ada pesan baru', 'Binding aktif mungkin sudah menerima pesan ini.')
      items.reload()
    } catch (err) {
      toast('error', 'Gagal mengirim notifikasi', err instanceof Error ? err.message : undefined)
    } finally {
      setNotifyingID(null)
    }
  }

  async function handleJoinRFS(id: string, refNo: string, ownerUsername?: string) {
    if (!user?.username) return
    setJoiningID(id)
    try {
      if (!ownerUsername) {
        await workItemsApi.assign(id, user.username)
        toast('success', 'RFS mulai dikerjakan', `${refNo} ditetapkan kepada ${user.username}`)
      } else {
        const result = await workItemsApi.addCollaborator(id, user.username)
        // Status maju ke in_progress hanya bila jadwal RFS sudah tiba.
        const detail = result.status
          ? `${refNo} — status: ${statusLabel(result.status)}`
          : `${refNo} — menunggu jadwal untuk mulai dikerjakan`
        toast(
          result.inserted ? 'success' : 'info',
          result.inserted ? 'Berhasil mengikuti RFS' : 'Sudah mengikuti RFS',
          detail,
        )
      }
      items.reload()
    } catch (err) {
      toast('error', 'Gagal mengikuti RFS', err instanceof Error ? err.message : undefined)
    } finally {
      setJoiningID(null)
    }
  }

  /** changeStatus mengubah status aktivasi/EWO (F25: selalu konfirmasi). */
  function changeStatus(id: string, refNo: string, next: string, label: string) {
    setConfirm({
      title: `${label}?`,
      body: `Status ${refNo} akan diubah menjadi "${next.replace(/_/g, ' ')}".`,
      confirmLabel: label,
      tone: 'primary',
      icon: 'swap_horiz',
      run: async () => {
        setBusyID(id)
        try {
          await workItemsApi.changeStatus(id, next, label)
          toast('success', label, refNo)
          items.reload()
        } catch (err) {
          toast('error', `Gagal: ${label}`, err instanceof Error ? err.message : undefined)
        } finally {
          setBusyID(null)
        }
      },
    })
  }

  /**
   * startRFS — tombol "Mulai Kerjakan": new → in_progress.
   *
   * Hanya tersedia setelah jadwal RFS (expire_at) tiba; sebelum itu tombol
   * nonaktif dan alasannya ditampilkan. Backend juga menolak permintaan yang
   * terlalu awal sehingga aturan tidak dapat dilewati dari klien.
   *
   * RFS tanpa owner: pengambil alih sekaligus menjadi owner (dan tercatat
   * sebagai penangan), karena status tidak boleh "in progress" tanpa penangan.
   */
  function startRFS(id: string, refNo: string, gateReason: string, ownerUsername?: string) {
    const claimFirst = !ownerUsername && !!user?.username
    setConfirm({
      title: 'Mulai kerjakan RFS?',
      body: claimFirst
        ? `${refNo} akan diambil oleh ${user?.username} dan statusnya menjadi "In Progress".`
        : `${refNo} akan dipindahkan ke status "In Progress". ${gateReason}.`,
      confirmLabel: 'Mulai Kerjakan',
      tone: 'primary',
      icon: 'play_arrow',
      run: async () => {
        setBusyID(id)
        try {
          if (claimFirst && user?.username) {
            // assign() memajukan status ke in_progress karena jadwal sudah tiba.
            await workItemsApi.assign(id, user.username)
          } else {
            await workItemsApi.rfsStart(id, 'Mulai kerjakan')
          }
          toast('success', 'RFS mulai dikerjakan', refNo)
          items.reload()
        } catch (err) {
          toast('error', 'Gagal memulai RFS', err instanceof Error ? err.message : undefined)
        } finally {
          setBusyID(null)
        }
      },
    })
  }

  function markActivated(id: string, refNo: string) {
    setConfirm({
      title: 'Tutup / Aktifkan RFS?',
      body: `RFS ${refNo} akan ditutup sebagai Closed/Completed dan tahap instalasi menjadi "Activated".`,
      confirmLabel: 'Close Ticket',
      tone: 'primary',
      icon: 'task_alt',
      run: async () => {
        await workItemsApi.rfsClose(id, 'Close ticket (activated)')
        toast('success', 'RFS ditutup (Closed/Completed)', refNo)
        items.reload()
      },
    })
  }

  function openCancel(id: string, refNo: string) {
    setCancelTarget({ id, refNo })
  }

  function openDelete(id: string, refNo: string) {
    setDeleteTarget({ id, refNo })
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
        title="Aktivasi / EWO"
        description="Kelola judul, customer, kategori, product, jadwal, dan tag Aktivasi/EWO. NOC diingatkan otomatis pada H-7/H-3/H-1/H-0."
        actions={
          <>
            <button className="btn-secondary" onClick={items.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            {isAdmin && (
              <button className="btn-secondary" onClick={() => setShowSummary(true)}>
                <span className="material-symbols-outlined text-[18px]">notifications_active</span>
                Ringkasan Notifikasi
              </button>
            )}
            {canWrite && (
              <button className="btn-primary" onClick={() => setShowCreate(true)}>
                <span className="material-symbols-outlined text-[18px]">event_available</span>
                Aktivasi / EWO Baru
              </button>
            )}
          </>
        }
      />

      {/* Ringkasan */}
      <div className="mb-5 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatFilterCard label="Total" value={total} tone="bg-surface-container text-text-secondary border-outline-variant" active={!bucket} onClick={() => { setStatus(''); setOnlyNotActivated(false); setStage(''); setBucket(''); setOffset(0) }} />
        <StatFilterCard label="7 hari ke depan" value={soon} tone="bg-warning-container text-on-warning-container border-outline-variant" active={bucket === 'soon'} onClick={() => { setStatus(''); setBucket(bucket === 'soon' ? '' : 'soon'); setOffset(0) }} />
        <StatFilterCard label="Terlewat" value={overdue} tone={overdue > 0 ? 'bg-critical-container text-on-critical-container border-critical' : 'bg-surface-container text-text-secondary border-outline-variant'} active={bucket === 'overdue'} onClick={() => { setBucket(bucket === 'overdue' ? '' : 'overdue'); setOffset(0) }} />
        <StatFilterCard label="Closed / Completed" value={statAll.filter(isCompletedRFS).length} tone="bg-success-container text-on-success-container border-outline-variant" active={bucket === 'closed'} onClick={() => { const on = bucket !== 'closed'; setBucket(on ? 'closed' : ''); setStatus(on ? 'activated' : ''); setOffset(0) }} />
      </div>

      {/* Filter */}
      <section className="card mb-5 p-4">
        <div className="grid gap-3 sm:grid-cols-3">
          <div>
            <label className="label-field" htmlFor="x-search">
              Cari
            </label>
            <input
              id="x-search"
              className="input"
              placeholder="customer, product, ref…"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value)
                setOffset(0)
              }}
            />
          </div>
          <div>
            <label className="label-field" htmlFor="x-status">
              Status
            </label>
            <select
              id="x-status"
              className="input"
              value={status}
              onChange={(e) => {
                setStatus(e.target.value)
                setOffset(0)
              }}
            >
              <option value="">Semua</option>
              <option value="new">New</option>
              <option value="planned">Planned (lama)</option>
              <option value="in_progress">In Progress</option>
              <option value="in_progress_field">Penanganan Lapangan</option>
              <option value="pending_troubleshoot">Pending Troubleshooting</option>
              <option value="activated">Closed / Completed</option>
              <option value="postponed">Postponed</option>
              <option value="cancelled">Cancelled</option>
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="x-stage">
              Tahap instalasi
            </label>
            <select id="x-stage" className="input" value={stage} onChange={(e) => setStage(e.target.value)}>
              <option value="">Semua</option>
              <option value="planned">Planned</option>
              <option value="provisioning">Provisioning</option>
              <option value="delivered">Delivered</option>
              <option value="activated">Activated</option>
              <option value="postponed">Postponed</option>
              <option value="cancelled">Cancelled</option>
            </select>
          </div>
        </div>
        <label className="mt-3 flex items-center gap-2 text-label-md text-text-secondary">
          <input
            type="checkbox"
            className="h-4 w-4"
            checked={onlyNotActivated}
            onChange={(e) => setOnlyNotActivated(e.target.checked)}
          />
          Hanya yang belum aktivasi (belum Closed/Completed &amp; belum cancelled)
        </label>
      </section>

      <section className="card">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3.5">
          <div>
            <h3 className="font-headline text-lg font-semibold">Daftar Aktivasi / EWO</h3>
            <p className="text-body-sm text-text-secondary">
              {rows.length} ditampilkan · {total} total · halaman {page}/{maxPage}
            </p>
          </div>
          <div className="flex gap-2">
            <button className="btn-secondary" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - limit))}>
              Sebelumnya
            </button>
            <button className="btn-secondary" disabled={offset + limit >= total} onClick={() => setOffset(offset + limit)}>
              Berikutnya
            </button>
          </div>
        </div>

        {items.loading && <LoadingBlock />}
        {items.error && (
          <div className="p-4">
            <ErrorState message={items.error} onRetry={items.reload} />
          </div>
        )}
        {!items.loading && !items.error && rows.length === 0 && (
          <EmptyState
            icon="event_available"
            title="Belum ada Aktivasi / EWO"
            body="Tambahkan data aktivasi dengan Customer, Product, Priority, dan Bandwidth."
            action={
              canWrite ? (
                <button className="btn-primary" onClick={() => setShowCreate(true)}>
                  <span className="material-symbols-outlined text-[18px]">event_available</span>
                  Tambah Aktivasi / EWO
                </button>
              ) : undefined
            }
          />
        )}

        {!items.loading && !items.error && rows.length > 0 && (
          <div className="overflow-x-auto">
            <table className="table">
              <thead>
                <tr>
                  <th>Ref</th>
                  <th>Judul</th>
                  <th>Customer</th>
                  <th>Product / Bandwidth</th>
                  <th>Priority</th>
                  <th>RFS (WIB)</th>
                  <th>Hitung mundur</th>
                  <th>Status</th>
                  <th>Penangan</th>
                  <th>Tag</th>
                  <th className="w-px whitespace-nowrap text-right">Aksi</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((it) => {
                  const cd = countdown(it.expire_at, it.status, it.rfs?.install_stage)
                  const rfs = it.rfs
                  const closed = isClosedItem(it)
                  const busy = busyID === it.id
                  // Tombol "Mulai Kerjakan" hanya aktif setelah jadwal RFS tiba.
                  const gate = rfsStartGate(it.status, it.expire_at, it.rfs?.install_stage)
                  const notStarted = it.status === 'new' || it.status === 'planned' || it.status === 'postponed'
                  return (
                    <tr key={it.id}>
                      <td>
                        <button className="mono text-label-md text-primary hover:underline" onClick={() => setDetailID(it.id)}>
                          {it.ref_no}
                        </button>
                      </td>
                      <td className="max-w-[240px]">
                        <button
                          className="block w-full break-words text-left font-medium hover:underline"
                          title={it.title}
                          onClick={() => setDetailID(it.id)}
                        >
                          {it.title || '—'}
                        </button>
                        {rfs?.category_label && (
                          <span className="badge mt-1 border-outline-variant bg-surface-container text-text-secondary">
                            <span className="material-symbols-outlined text-[13px]">category</span>
                            {rfs.category_label}
                          </span>
                        )}
                      </td>
                      <td className="max-w-[200px]">
                        <div
                          className="truncate text-body-sm"
                          title={rfs?.customer_name || ''}
                        >
                          {rfs?.customer_name || '—'}
                        </div>
                      </td>
                      <td className="max-w-[200px]">
                        <div className="truncate text-body-sm" title={rfs?.service_package || ''}>
                          {rfs?.service_package || '—'}
                        </div>
                        <span className="mono block truncate text-label-sm text-text-secondary" title={rfs?.bandwidth || ''}>
                          {rfs?.bandwidth || '—'}
                        </span>
                      </td>
                      <td>
                        <PriorityPill priority={it.priority} />
                      </td>
                      <td className="mono whitespace-nowrap text-label-md">{formatWIB(it.expire_at)}</td>
                      <td className={`mono whitespace-nowrap text-label-md ${cd.tone}`}>{cd.text}</td>
                      <td>
                        <StatusBadge status={statusLabel(it.status)} tone={statusTone[it.status]} />
                      </td>
                      <td className="max-w-[180px]">
                        {it.owner_username ? (
                          <div className="flex flex-wrap items-center gap-1">
                            <span
                              className="mono rounded-full bg-surface-container px-2 py-0.5 text-label-sm font-semibold"
                              title={`Owner: ${it.owner_username}`}
                            >
                              {it.owner_username}
                            </span>
                            {(it.collaborators?.length ?? 0) > 0 && (
                              <span
                                className="inline-flex items-center gap-0.5 rounded-full border border-outline-variant px-1.5 py-0.5 text-label-sm text-text-secondary"
                                title={`Ikut menangani: ${(it.collaborators ?? []).map((c) => c.username).join(', ')}`}
                              >
                                <span className="material-symbols-outlined text-[13px]">group</span>
                                {it.collaborators?.length}
                              </span>
                            )}
                          </div>
                        ) : (
                          <span className="text-label-sm text-text-secondary">belum diambil</span>
                        )}
                      </td>
                      <td className="max-w-[160px]">
                        <TagList tags={it.tags} colors={tagColors} />
                      </td>
                      <td className="w-px whitespace-nowrap">
                        <div className="flex justify-end gap-2">
                          {/* Tombol utama: mulai kerjakan (belum dimulai) atau ikut menangani. */}
                          {canWrite && !closed && notStarted && (
                            <button
                              className="btn-primary h-8 px-2 text-label-sm"
                              disabled={!gate.allowed || busy}
                              onClick={() => startRFS(it.id, it.ref_no, gate.reason, it.owner_username)}
                              title={gate.allowed ? 'Mulai kerjakan RFS (status menjadi In Progress)' : gate.reason}
                            >
                              <span className={`material-symbols-outlined text-[16px] ${busy ? 'animate-spin' : ''}`}>
                                {busy ? 'progress_activity' : 'play_arrow'}
                              </span>
                              Mulai Kerjakan
                            </button>
                          )}
                          {canWrite && !closed && !notStarted && !isHandledByMe(it) && (
                            <button
                              className="btn-primary h-8 px-2 text-label-sm"
                              disabled={joiningID === it.id}
                              onClick={() => handleJoinRFS(it.id, it.ref_no, it.owner_username)}
                              title={it.owner_username ? 'Ikuti RFS sebagai penangan tambahan' : 'Ambil dan kerjakan RFS'}
                            >
                              <span className={`material-symbols-outlined text-[16px] ${joiningID === it.id ? 'animate-spin' : ''}`}>
                                {joiningID === it.id ? 'progress_activity' : it.owner_username ? 'group_add' : 'play_arrow'}
                              </span>
                              {it.owner_username ? 'Ikuti RFS' : 'Kerjakan'}
                            </button>
                          )}
                          <ActionMenu
                            label="Penanganan"
                            items={[
                              { label: 'Lihat detail', icon: 'open_in_new', onClick: () => setDetailID(it.id) },
                              // Mulai Kerjakan: new → in_progress, hanya setelah jadwal tiba.
                              ...(canWrite && !closed && notStarted
                                ? [{
                                    label: 'Mulai Kerjakan',
                                    icon: 'play_arrow',
                                    disabled: !gate.allowed || busy,
                                    title: gate.allowed ? 'Mulai kerjakan RFS' : gate.reason,
                                    onClick: () => startRFS(it.id, it.ref_no, gate.reason, it.owner_username),
                                  }]
                                : []),
                              ...(canWrite && !closed
                                ? [
                                    { label: 'Troubleshoot', icon: 'handyman', tone: 'warning' as const, disabled: busy, onClick: () => setTroubleTarget({ id: it.id, refNo: it.ref_no }) },
                                    { label: 'Postpone', icon: 'schedule', disabled: busy, onClick: () => changeStatus(it.id, it.ref_no, 'postponed', 'Postpone') },
                                    {
                                      label: 'Close ticket',
                                      icon: 'task_alt',
                                      tone: 'success' as const,
                                      disabled: busy || notStarted,
                                      title: notStarted ? 'Mulai kerjakan RFS dulu sebelum menutup tiket' : 'Tutup RFS (Closed/Completed)',
                                      onClick: () => markActivated(it.id, it.ref_no),
                                    },
                                  ]
                                : []),
                              ...(canManage(it) ? [{ label: 'Ubah RFS', icon: 'edit', onClick: () => setEditItem(it.id) }] : []),
                              ...(canWrite
                                ? [{ label: 'Kirim notifikasi', icon: notifyingID === it.id ? 'progress_activity' : 'send', disabled: notifyingID === it.id, onClick: () => handleNotify(it.id, it.ref_no) }]
                                : []),
                              ...(canWrite && !closed && canCancel(it)
                                ? [{ label: 'Batalkan RFS', icon: 'cancel', tone: 'warning' as const, onClick: () => openCancel(it.id, it.ref_no) }]
                                : []),
                              ...(canWrite && canManage(it)
                                ? [{ label: 'Hapus RFS', icon: 'delete', tone: 'danger' as const, onClick: () => openDelete(it.id, it.ref_no) }]
                                : []),
                            ]}
                          />
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {showCreate && (
        <RFSForm
          onClose={() => setShowCreate(false)}
          onSaved={() => {
            setShowCreate(false)
            toast('success', 'Aktivasi / EWO dibuat', 'NOC diingatkan pada H-7, H-3, H-1, dan H-0.')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membuat Aktivasi / EWO', msg)}
        />
      )}

      {editItem && (
        <RFSForm
          editId={editItem}
          onClose={() => setEditItem(null)}
          onSaved={() => {
            setEditItem(null)
            toast('success', 'Aktivasi / EWO diperbarui')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal memperbarui Aktivasi / EWO', msg)}
        />
      )}

      {showSummary && <RFSSummaryModal toast={toast} onClose={() => setShowSummary(false)} />}

      {troubleTarget && (
        <TroubleshootModal
          target={troubleTarget}
          onClose={() => setTroubleTarget(null)}
          onSaved={() => {
            setTroubleTarget(null)
            toast('success', 'Status & catatan penanganan disimpan')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal menyimpan penanganan', msg)}
        />
      )}

      {cancelTarget && (
        <ReasonModal
          title={`Batalkan RFS — ${cancelTarget.refNo}`}
          label="Alasan pembatalan (opsional)"
          confirmLabel="Batalkan RFS"
          tone="danger"
          icon="cancel"
          onClose={() => setCancelTarget(null)}
          onSubmit={async (reason) => {
            await workItemsApi.rfsCancel(cancelTarget.id, reason)
            toast('success', 'RFS dibatalkan', cancelTarget.refNo)
            setCancelTarget(null)
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membatalkan RFS', msg)}
        />
      )}

      {deleteTarget && (
        <ReasonModal
          title={`Hapus RFS — ${deleteTarget.refNo}`}
          label="Alasan penghapusan (wajib)"
          confirmLabel="Hapus RFS"
          tone="danger"
          icon="delete"
          required
          onClose={() => setDeleteTarget(null)}
          onSubmit={async (reason) => {
            await workItemsApi.rfsDelete(deleteTarget.id, reason)
            toast('success', 'RFS dihapus', deleteTarget.refNo)
            setDeleteTarget(null)
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal menghapus RFS', msg)}
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

/** PriorityPill menampilkan prioritas Aktivasi/EWO. */
function PriorityPill({ priority }: { priority: string }) {
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

/** StatFilterCard — kartu ringkasan yang dapat diklik untuk memfilter daftar. */
function StatFilterCard({
  label,
  value,
  tone,
  active,
  onClick,
}: {
  label: string
  value: number
  tone: string
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`card px-4 py-3 text-left transition hover:bg-surface-container-low/70 ${
        active ? 'ring-2 ring-primary' : ''
      }`}
      aria-pressed={active}
    >
      <div className="kicker">{label}</div>
      <div className="mt-0.5 flex items-center gap-2">
        <span className="font-headline text-xl font-semibold">{value}</span>
        <span className={`badge ${tone}`} aria-hidden />
      </div>
    </button>
  )
}

/* ------------------------------------------------------------------------- */
/* F23: modal Troubleshoot — status pending_troubleshoot + catatan penanganan */
/* ------------------------------------------------------------------------- */

function TroubleshootModal({
  target,
  onClose,
  onSaved,
  onError,
}: {
  target: { id: string; refNo: string }
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const detail = useAsync(() => workItemsApi.get(target.id), [target.id])
  const [issue, setIssue] = useState('')
  const [trouble, setTrouble] = useState('')
  const [solution, setSolution] = useState('')
  const [busy, setBusy] = useState(false)
  const [initialised, setInitialised] = useState(false)

  const rfs = detail.data?.item?.rfs
  if (rfs && !initialised) {
    setIssue(rfs.issue_found ?? '')
    setTrouble(rfs.troubleshooting ?? '')
    setSolution(rfs.action_solution ?? '')
    setInitialised(true)
  }

  async function save() {
    setBusy(true)
    try {
      // Simpan catatan penanganan lalu ubah status ke pending_troubleshoot.
      await workItemsApi.update(target.id, {
        rfs: { issue_found: issue, troubleshooting: trouble, action_solution: solution },
      })
      const cur = detail.data?.item?.status
      if (cur !== 'pending_troubleshoot') {
        try {
          await workItemsApi.changeStatus(target.id, 'pending_troubleshoot', 'Menemukan kendala')
        } catch {
          /* status mungkin tidak dapat bertransisi dari posisi saat ini */
        }
      }
      onSaved()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Terjadi kesalahan')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title={`Troubleshoot — ${target.refNo}`}
      width="lg"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={save} disabled={busy}>
            {busy ? 'Menyimpan…' : 'Simpan & Pending Troubleshoot'}
          </button>
        </>
      }
    >
      {detail.loading ? (
        <LoadingBlock label="Memuat data…" />
      ) : (
        <div className="space-y-3.5">
          <p className="text-body-sm text-text-secondary">
            Isi catatan penanganan. Menyimpan akan menandai status sebagai{' '}
            <strong>Pending Troubleshooting</strong>.
          </p>
          <div>
            <label className="label-field" htmlFor="tr-issue">
              Issue ditemukan
            </label>
            <textarea id="tr-issue" className="input h-20 py-2" value={issue} onChange={(e) => setIssue(e.target.value)} />
          </div>
          <div>
            <label className="label-field" htmlFor="tr-trouble">
              Troubleshooting
            </label>
            <textarea id="tr-trouble" className="input h-20 py-2" value={trouble} onChange={(e) => setTrouble(e.target.value)} />
          </div>
          <div>
            <label className="label-field" htmlFor="tr-solution">
              Action / Solusi
            </label>
            <textarea id="tr-solution" className="input h-20 py-2" value={solution} onChange={(e) => setSolution(e.target.value)} />
          </div>
        </div>
      )}
    </Modal>
  )
}

/** ReasonModal — modal generik untuk Cancel/Hapus dengan alasan. */
function ReasonModal({
  title,
  label,
  confirmLabel,
  tone,
  icon,
  required = false,
  onClose,
  onSubmit,
  onError,
}: {
  title: string
  label: string
  confirmLabel: string
  tone: 'primary' | 'danger'
  icon: string
  required?: boolean
  onClose: () => void
  onSubmit: (reason: string) => Promise<void>
  onError: (msg: string) => void
}) {
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit() {
    if (required && !reason.trim()) {
      onError('Alasan wajib diisi')
      return
    }
    setBusy(true)
    try {
      await onSubmit(reason.trim())
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Terjadi kesalahan')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title={title}
      width="sm"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className={tone === 'danger' ? 'btn-brand' : 'btn-primary'} onClick={submit} disabled={busy}>
            {busy ? 'Memproses…' : confirmLabel}
          </button>
        </>
      }
    >
      <div className="space-y-3">
        <div className={`flex items-start gap-2.5 rounded-control border p-3 text-body-sm ${tone === 'danger' ? 'border-critical bg-critical-container/40 text-on-critical-container' : 'border-border bg-surface-container-low'}`}>
          <span className="material-symbols-outlined text-[19px] shrink-0">{icon}</span>
          <span>{label}</span>
        </div>
        <div>
          <label className="label-field" htmlFor="reason-input">
            {required ? 'Alasan (wajib)' : 'Alasan'}
          </label>
          <textarea
            id="reason-input"
            className="input h-24 py-2"
            autoFocus
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder={required ? 'Tulis alasan penghapusan…' : 'Opsional'}
          />
        </div>
      </div>
    </Modal>
  )
}

/* ------------------------------------------------------------------------- */

export function RFSForm({
  editId,
  onClose,
  onSaved,
  onError,
}: {
  editId?: string
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const packages = useAsync(() => masterDataApi.list({ kind: 'service_package', active: 'true' }), [])
  const teams = useAsync(() => api.teams(), [])
  // F45: customer aktif dari Master Data (kind customer) untuk dropdown.
  const customers = useAsync(() => masterDataApi.list({ kind: 'customer', active: 'true' }), [])
  // F45: kategori & tag EWO dari Master Data.
  const categories = useAsync(() => masterDataApi.list({ kind: 'ewo_category', active: 'true' }), [])
  const tagOptions = useAsync(() => masterDataApi.list({ kind: 'ewo_tag', active: 'true' }), [])
  const existing = useAsync(
    () => (editId ? workItemsApi.get(editId) : Promise.resolve(null)),
    [editId],
  )

  const item = existing.data?.item
  const rfs = item?.rfs
  // CID customer (meta master data) untuk ditampilkan pada dropdown customer.
  const customerCID = (entry: MasterDataEntry): string => {
    const m = (entry.meta ?? {}) as Record<string, unknown>
    return m.cid == null ? '' : String(m.cid)
  }

  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [customerName, setCustomerName] = useState('')
  // F45: manualCustomer=true menampilkan input teks (client di luar daftar).
  const [manualCustomer, setManualCustomer] = useState(false)
  const [categoryCode, setCategoryCode] = useState('')
  const [tags, setTags] = useState<string[]>([])
  const [servicePackage, setServicePackage] = useState('')
  const [priority, setPriority] = useState('normal')
  const [bandwidth, setBandwidth] = useState('')
  const [picTeamID, setPicTeamID] = useState('')
  const [rfsDate, setRfsDate] = useState('')
  const [initialised, setInitialised] = useState(false)
  const [busy, setBusy] = useState(false)

  const pkgEntries: MasterDataEntry[] = packages.data?.entries ?? []
  const customerEntries: MasterDataEntry[] = customers.data?.entries ?? []
  const categoryEntries: MasterDataEntry[] = categories.data?.entries ?? []
  const activeTagEntries: MasterDataEntry[] = tagOptions.data?.entries ?? []
  const activeTagLabels = activeTagEntries.map((t) => t.label)

  // Isi form saat data existing + pilihan customer tiba (mode edit).
  // Menunggu customer termuat agar customer legacy dapat dideteksi manual mode.
  if (editId && item && !customers.loading && !initialised) {
    const cn = rfs?.customer_name ?? ''
    setTitle(item.title ?? '')
    setDescription(item.description ?? '')
    setCustomerName(cn)
    // Customer tidak ada di daftar aktif → otomatis masuk mode manual.
    setManualCustomer(!!cn && !customerEntries.some((c) => c.label === cn))
    setCategoryCode(rfs?.category_code || rfs?.category_label || '')
    setTags(item.tags ?? [])
    setServicePackage(rfs?.service_package ?? '')
    setPriority(item.priority ?? 'normal')
    setBandwidth(rfs?.bandwidth ?? '')
    setPicTeamID(rfs?.pic_team_id ?? '')
    if (item.expire_at) {
      const d = new Date(item.expire_at)
      const pad = (n: number) => String(n).padStart(2, '0')
      setRfsDate(
        `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`,
      )
    }
    setInitialised(true)
  }

  function toggleTag(label: string) {
    setTags((prev) => (prev.includes(label) ? prev.filter((t) => t !== label) : [...prev, label]))
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!title.trim()) {
      onError('Judul wajib diisi')
      return
    }
    if (!customerName.trim()) {
      onError('Customer wajib diisi')
      return
    }
    if (!rfsDate) {
      onError('Tanggal RFS wajib diisi')
      return
    }
    setBusy(true)
    try {
      const expireAt = new Date(rfsDate).toISOString()
      // Kategori legacy (tidak aktif di Master Data) disimpan sebagai label
      // bebas agar penyimpanan tidak gagal validasi code tak dikenal.
      const legacyCategory =
        !!categoryCode && !categoryEntries.some((c) => c.code === categoryCode) ? categoryCode : ''
      // F45: judul & customer dikirim terpisah (tidak saling menimpa).
      const payload = {
        title: title.trim(),
        description: description.trim(),
        priority,
        expire_at: expireAt,
        tags,
        rfs: {
          customer_name: customerName.trim(),
          service_package: servicePackage,
          bandwidth,
          pic_team_id: picTeamID || undefined,
          category_code: legacyCategory ? '' : categoryCode,
          ...(legacyCategory ? { category_label: legacyCategory } : {}),
        },
      }

      if (editId) {
        await workItemsApi.update(editId, payload)
      } else {
        await workItemsApi.create({
          ...payload,
          item_type: 'rfs',
          // Status awal RFS: `new` (belum dikerjakan). Status maju ke
          // in_progress hanya lewat tombol "Mulai Kerjakan" setelah jadwal tiba.
          status: 'new',
        })
      }
      onSaved()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Terjadi kesalahan')
    } finally {
      setBusy(false)
    }
  }

  // Customer legacy yang tak ada di opsi aktif tetap ditampilkan pada select.
  const customerMissing =
    !!customerName.trim() && !customerEntries.some((c) => c.label === customerName.trim())
  // Tag legacy (tidak ada di master data aktif) tetap tampil saat edit.
  const legacyTags = tags.filter((t) => !activeTagLabels.includes(t))

  return (
    <Modal
      title={editId ? 'Ubah Aktivasi / EWO' : 'Aktivasi / EWO Baru'}
      onClose={onClose}
      width="lg"
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button
            className="btn-primary"
            onClick={submit}
            disabled={busy || !title.trim() || !customerName.trim() || !rfsDate}
          >
            {busy ? 'Menyimpan…' : editId ? 'Simpan Perubahan' : 'Buat'}
          </button>
        </>
      }
    >
      {editId && existing.loading && <LoadingBlock label="Memuat data…" />}

      <form onSubmit={submit} className="space-y-3.5">
        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="rf-title">
              Judul
            </label>
            <input
              id="rf-title"
              className="input"
              required
              autoFocus
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Aktivasi baru — PT Contoh Jaya"
            />
            <p className="mt-1 text-label-sm text-text-secondary">Judul ringkas permintaan Aktivasi/EWO.</p>
          </div>
          <div>
            <label className="label-field" htmlFor="rf-customer">
              Customer
            </label>
            {manualCustomer ? (
              <input
                id="rf-customer"
                className="input"
                required
                value={customerName}
                onChange={(e) => setCustomerName(e.target.value)}
                placeholder="Nama client di luar daftar"
              />
            ) : (
              <select
                id="rf-customer"
                className="input"
                value={customerName}
                onChange={(e) => setCustomerName(e.target.value)}
              >
                <option value="">— Pilih Customer —</option>
                {customerEntries.map((c) => (
                  <option key={c.id} value={c.label}>
                    {c.label}
                    {customerCID(c) ? ` — ${customerCID(c)}` : ''}
                  </option>
                ))}
                {customerMissing && <option value={customerName}>{customerName}</option>}
              </select>
            )}
            <label className="mt-2 flex items-center gap-2 text-label-sm text-text-secondary">
              <input
                type="checkbox"
                className="h-4 w-4"
                checked={manualCustomer}
                onChange={(e) => setManualCustomer(e.target.checked)}
              />
              Client tidak ada dalam daftar/list
            </label>
          </div>
        </div>

        <div>
          <label className="label-field" htmlFor="rf-desc">
            Deskripsi
          </label>
          <textarea
            id="rf-desc"
            className="input h-20 py-2"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Detail permintaan, kebutuhan teknis, atau catatan tambahan…"
          />
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="rf-category">
              Kategori EWO
            </label>
            <select
              id="rf-category"
              className="input"
              value={categoryCode}
              onChange={(e) => setCategoryCode(e.target.value)}
            >
              <option value="">— Pilih Kategori —</option>
              {categoryEntries.map((c) => (
                <option key={c.id} value={c.code}>
                  {c.label}
                </option>
              ))}
              {categoryCode && !categoryEntries.some((c) => c.code === categoryCode) && (
                <option value={categoryCode}>{rfs?.category_label || categoryCode}</option>
              )}
            </select>
            <p className="mt-1 text-label-sm text-text-secondary">Dikelola di Master Data › Kategori EWO.</p>
          </div>
          <div>
            <label className="label-field" htmlFor="rf-package">
              Product (jenis paket)
            </label>
            <select id="rf-package" className="input" value={servicePackage} onChange={(e) => setServicePackage(e.target.value)}>
              <option value="">— Pilih Product —</option>
              {pkgEntries.map((p) => (
                <option key={p.id} value={p.label}>
                  {p.label}
                </option>
              ))}
              {servicePackage && !pkgEntries.some((p) => p.label === servicePackage) && (
                <option value={servicePackage}>{servicePackage}</option>
              )}
            </select>
            <p className="mt-1 text-label-sm text-text-secondary">
              Dikelola di Master Data › Jenis Paket (dapat diubah/ditambah).
            </p>
          </div>
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="rf-priority">
              Priority
            </label>
            <select id="rf-priority" className="input" value={priority} onChange={(e) => setPriority(e.target.value)}>
              <option value="low">Low</option>
              <option value="normal">Normal</option>
              <option value="high">High</option>
              <option value="critical">Critical</option>
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="rf-bandwidth">
              Bandwidth
            </label>
            <input
              id="rf-bandwidth"
              className="input mono"
              value={bandwidth}
              onChange={(e) => setBandwidth(e.target.value)}
              placeholder="100 Mbps"
            />
          </div>
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="rf-date">
              Tanggal &amp; jam RFS
            </label>
            <input
              id="rf-date"
              type="datetime-local"
              className="input"
              required
              value={rfsDate}
              onChange={(e) => setRfsDate(e.target.value)}
            />
            <p className="mt-1 text-label-sm text-text-secondary">
              Pengingat dikirim H-7, H-3, H-1, dan H-0 sebelum waktu ini.
            </p>
          </div>
        </div>

        <div>
          <div className="label-field">Tag</div>
          <div className="flex flex-wrap gap-1.5">
            {activeTagEntries.map((t) => {
              const on = tags.includes(t.label)
              return (
                <button
                  key={t.id}
                  type="button"
                  onClick={() => toggleTag(t.label)}
                  aria-pressed={on}
                  className={`badge border transition ${
                    on
                      ? 'border-primary bg-primary-container text-on-primary-container'
                      : 'border-outline-variant bg-surface-container text-text-secondary'
                  }`}
                >
                  {t.label}
                </button>
              )
            })}
            {/* F45: tag legacy (nonaktif di Master Data) tetap tampil & terpilih agar tidak hilang. */}
            {legacyTags.map((t) => (
              <button
                key={`legacy-${t}`}
                type="button"
                onClick={() => toggleTag(t)}
                aria-pressed
                title="Tag lama (tidak aktif di Master Data)"
                className="badge border border-warning bg-warning-container/50 text-on-warning-container"
              >
                {t}
                <span className="material-symbols-outlined text-[13px]">history</span>
              </button>
            ))}
            {activeTagEntries.length === 0 && legacyTags.length === 0 && (
              <span className="text-label-sm text-text-secondary">Belum ada tag di Master Data.</span>
            )}
          </div>
          <p className="mt-1 text-label-sm text-text-secondary">Dikelola di Master Data › Tag EWO.</p>
        </div>

        <div>
          <label className="label-field" htmlFor="rf-picteam">
            PIC Team
          </label>
          <select
            id="rf-picteam"
            className="input"
            value={picTeamID}
            onChange={(e) => setPicTeamID(e.target.value)}
          >
            <option value="">— Pilih PIC Team —</option>
            {(teams.data?.teams ?? []).map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
              </option>
            ))}
          </select>
          <p className="mt-1 text-label-sm text-text-secondary">
            Target notifikasi otomatis mengikuti tim PIC yang dipilih.
          </p>
        </div>
      </form>
    </Modal>
  )
}

/** RFSSummaryModal (F45) — pengaturan ringkasan Aktivasi/EWO terbuka. */
function RFSSummaryModal({ toast, onClose }: { toast: Toast; onClose: () => void }) {
  const [mode, setMode] = useState<RFSSummarySettings['mode']>('interval')
  const [interval, setInterval] = useState(60)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    rfsSummaryApi
      .get()
      .then((s) => {
        setMode(s.mode)
        setInterval(s.interval_minutes || 60)
      })
      .catch(() => toast('error', 'Gagal memuat pengaturan ringkasan Aktivasi/EWO'))
      .finally(() => setLoading(false))
  }, [toast])

  async function save() {
    setBusy(true)
    try {
      await rfsSummaryApi.save({ mode, interval_minutes: interval })
      toast('success', 'Pengaturan disimpan', 'Ringkasan Aktivasi/EWO diperbarui.')
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
      const res = await rfsSummaryApi.preview()
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

  const modes: { value: RFSSummarySettings['mode']; label: string; note: string }[] = [
    { value: 'on_change', label: 'Saat ada perubahan', note: 'Kirim saat Aktivasi/EWO dibuat atau berubah.' },
    { value: 'interval', label: 'Berkala', note: 'Kirim otomatis setiap N menit.' },
    { value: 'off', label: 'Nonaktif', note: 'Tidak mengirim ringkasan otomatis.' },
  ]

  return (
    <Modal
      title="Ringkasan Notifikasi Aktivasi/EWO"
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
            Ringkasan memuat seluruh Aktivasi/EWO yang belum ditutup, dikelompokkan sebagai
            direncanakan, sedang dikerjakan, dan menunggu troubleshooting.
          </p>
          <div className="space-y-2">
            {modes.map((m) => (
              <label
                key={m.value}
                className={`flex cursor-pointer items-start gap-3 rounded-control border p-3 ${
                  mode === m.value ? 'border-primary bg-primary/5' : 'border-outline-variant'
                }`}
              >
                <input
                  type="radio"
                  name="rfs-summary-mode"
                  value={m.value}
                  checked={mode === m.value}
                  onChange={() => setMode(m.value)}
                  className="mt-0.5"
                />
                <span>
                  <span className="block text-body-sm font-medium">{m.label}</span>
                  <span className="block text-label-sm text-text-secondary">{m.note}</span>
                </span>
              </label>
            ))}
          </div>
          {mode === 'interval' && (
            <div>
              <label className="label-field" htmlFor="rfs-summary-interval">
                Interval (menit)
              </label>
              <input
                id="rfs-summary-interval"
                type="number"
                min={5}
                max={1440}
                className="input"
                value={interval}
                onChange={(e) => setInterval(Number(e.target.value))}
              />
            </div>
          )}
        </div>
      )}
    </Modal>
  )
}
