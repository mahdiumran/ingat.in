import { useEffect, useState } from 'react'
import { ApiUser, api, MasterDataEntry, masterDataApi, notificationApi, workItemsApi } from '../api'
import {
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

/** Preset tag Aktivasi/EWO (kode sinkron dengan master data tag_color). */
const EWO_TAG_PRESETS = ['change_service', 'upgrade', 'new_install', 'request', 'urgent']

/** labelTag memberi label manusiawi untuk preset tag. */
function labelTag(code: string): string {
  switch (code) {
    case 'change_service':
      return 'Change Service'
    case 'upgrade':
      return 'Upgrade'
    case 'new_install':
      return 'New'
    case 'request':
      return 'Request'
    case 'urgent':
      return 'Urgent'
    default:
      return code
  }
}

/** Hitung mundur ke tanggal RFS. */
function countdown(rfsAt?: string): { text: string; tone: string } {
  if (!rfsAt) return { text: '—', tone: 'text-text-secondary' }
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
}: {
  user: ApiUser | null
  toast: Toast
  focusItemId?: string | null
  onFocusConsumed?: () => void
}) {
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [stage, setStage] = useState('')
  const [onlyNotActivated, setOnlyNotActivated] = useState(false)
  const [offset, setOffset] = useState(0)
  const [showCreate, setShowCreate] = useState(false)
  const [editItem, setEditItem] = useState<string | null>(null)
  const [detailID, setDetailID] = useState<string | null>(null)
  const [notifyingID, setNotifyingID] = useState<string | null>(null)
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
  const rows = all.filter((i) => {
    if (stage && i.rfs?.install_stage !== stage) return false
    if (onlyNotActivated && (i.rfs?.install_stage === 'activated' || i.status === 'cancelled' || i.status === 'activated')) {
      return false
    }
    return true
  })
  const total = items.data?.total ?? 0
  const canWrite = !!user && user.role !== 'viewer'
  const page = Math.floor(offset / limit) + 1
  const maxPage = Math.max(1, Math.ceil(total / limit))

  const isClosed = (s: string) => s === 'activated' || s === 'cancelled'
  const soon = all.filter((i) => {
    if (!i.expire_at || isClosed(i.status)) return false
    const diff = new Date(i.expire_at).getTime() - Date.now()
    return diff >= 0 && diff <= 7 * 86_400_000
  }).length

  const overdue = all.filter((i) => {
    if (!i.expire_at || isClosed(i.status)) return false
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

  function markActivated(id: string, refNo: string) {
    setConfirm({
      title: 'Tutup / Aktifkan RFS?',
      body: `RFS ${refNo} akan ditutup sebagai Closed/Completed dan tahap instalasi menjadi "Activated".`,
      confirmLabel: 'Close Ticket',
      tone: 'primary',
      icon: 'task_alt',
      run: async () => {
        await workItemsApi.update(id, { rfs: { install_stage: 'activated' } })
        try {
          await workItemsApi.changeStatus(id, 'activated', 'Close ticket (activated)')
        } catch {
          /* tahap tetap tersimpan walau transisi status tidak diizinkan */
        }
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
        description="Aktivasi layanan / EWO — field: Customer, Product, Priority, Bandwidth, Tag. NOC diingatkan otomatis pada H-7/H-3/H-1/H-0."
        actions={
          <>
            <button className="btn-secondary" onClick={items.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
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
        <MiniCard label="Total" value={total} tone="bg-surface-container text-text-secondary border-outline-variant" />
        <MiniCard label="7 hari ke depan" value={soon} tone="bg-warning-container text-on-warning-container border-outline-variant" />
        <MiniCard label="Terlewat" value={overdue} tone={overdue > 0 ? 'bg-critical-container text-on-critical-container border-critical' : 'bg-surface-container text-text-secondary border-outline-variant'} />
        <MiniCard
          label="Closed / Completed"
          value={all.filter((i) => i.rfs?.install_stage === 'activated').length}
          tone="bg-success-container text-on-success-container border-outline-variant"
        />
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
              <option value="planned">Planned</option>
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
                  <th>Customer</th>
                  <th>Product / Bandwidth</th>
                  <th>Priority</th>
                  <th>RFS (WIB)</th>
                  <th>Hitung mundur</th>
                  <th>Status</th>
                  <th>Tag</th>
                  <th className="w-px whitespace-nowrap text-right">Aksi</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((it) => {
                  const cd = countdown(it.expire_at)
                  const rfs = it.rfs
                  const closed = isClosed(it.status)
                  const busy = busyID === it.id
                  return (
                    <tr key={it.id}>
                      <td>
                        <button className="mono text-label-md text-primary hover:underline" onClick={() => setDetailID(it.id)}>
                          {it.ref_no}
                        </button>
                      </td>
                      <td className="max-w-[200px]">
                        <button className="block truncate text-left font-medium hover:underline" onClick={() => setDetailID(it.id)}>
                          {rfs?.customer_name || it.title}
                        </button>
                        {it.device_ref && <span className="mono text-label-sm text-text-secondary">{it.device_ref}</span>}
                      </td>
                      <td>
                        <div className="text-body-sm">{rfs?.service_package || '—'}</div>
                        <span className="mono text-label-sm text-text-secondary">{rfs?.bandwidth || '—'}</span>
                      </td>
                      <td>
                        <PriorityPill priority={it.priority} />
                      </td>
                      <td className="mono whitespace-nowrap text-label-md">{formatWIB(it.expire_at)}</td>
                      <td className={`mono whitespace-nowrap text-label-md ${cd.tone}`}>{cd.text}</td>
                      <td>
                        <StatusBadge status={statusLabel(it.status)} tone={statusTone[it.status]} />
                      </td>
                      <td className="max-w-[160px]">
                        <TagList tags={it.tags} colors={tagColors} />
                      </td>
                      <td className="w-px whitespace-nowrap">
                        <div className="flex justify-end gap-1">
                          {canWrite && !closed && it.status !== 'in_progress' && (
                            <button
                              className="btn-ghost h-8 px-1.5 text-label-sm"
                              title="Tandai sedang dikerjakan"
                              disabled={busy}
                              onClick={() => changeStatus(it.id, it.ref_no, 'in_progress', 'In Progress')}
                            >
                              <span className="material-symbols-outlined text-[16px]">play_arrow</span>
                              In Progress
                            </button>
                          )}
                          {canWrite && !closed && (
                            <button
                              className="btn-ghost h-8 px-1.5 text-label-sm text-warning"
                              title="Tandai pending troubleshoot + catatan penanganan"
                              disabled={busy}
                              onClick={() => setTroubleTarget({ id: it.id, refNo: it.ref_no })}
                            >
                              <span className="material-symbols-outlined text-[16px]">handyman</span>
                              Troubleshoot
                            </button>
                          )}
                          {canWrite && !closed && (
                            <button
                              className="btn-ghost h-8 px-1.5 text-label-sm"
                              title="Tunda (postpone)"
                              disabled={busy}
                              onClick={() => changeStatus(it.id, it.ref_no, 'postponed', 'Postpone')}
                            >
                              <span className="material-symbols-outlined text-[16px]">schedule</span>
                              Postpone
                            </button>
                          )}
                          {canWrite && !closed && (
                            <button
                              className="btn-ghost h-8 px-1.5 text-label-sm text-success"
                              title="Close ticket (aktifkan)"
                              disabled={busy}
                              onClick={() => markActivated(it.id, it.ref_no)}
                            >
                              <span className="material-symbols-outlined text-[16px]">task_alt</span>
                              Close
                            </button>
                          )}
                          {canManage(it) && (
                            <button className="btn-ghost h-8 w-8 px-0" title="Ubah" onClick={() => setEditItem(it.id)}>
                              <span className="material-symbols-outlined text-[18px]">edit</span>
                            </button>
                          )}
                          {canWrite && (
                            <button
                              className="btn-ghost h-8 w-8 px-0"
                              title="Kirim notifikasi sekarang"
                              disabled={notifyingID === it.id}
                              onClick={() => handleNotify(it.id, it.ref_no)}
                            >
                              <span className={`material-symbols-outlined text-[18px] ${notifyingID === it.id ? 'animate-spin' : ''}`}>
                                {notifyingID === it.id ? 'progress_activity' : 'send'}
                              </span>
                            </button>
                          )}
                          {canWrite && !closed && canCancel(it) && (
                            <button
                              className="btn-ghost h-8 w-8 px-0 text-warning"
                              title="Batalkan RFS"
                              onClick={() => openCancel(it.id, it.ref_no)}
                            >
                              <span className="material-symbols-outlined text-[18px]">cancel</span>
                            </button>
                          )}
                          {canWrite && canManage(it) && (
                            <button
                              className="btn-ghost h-8 w-8 px-0 text-critical"
                              title="Hapus RFS (alasan wajib)"
                              onClick={() => openDelete(it.id, it.ref_no)}
                            >
                              <span className="material-symbols-outlined text-[18px]">delete</span>
                            </button>
                          )}
                          <button className="btn-ghost h-8 w-8 px-0" title="Detail" onClick={() => setDetailID(it.id)}>
                            <span className="material-symbols-outlined text-[18px]">open_in_new</span>
                          </button>
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

function MiniCard({ label, value, tone }: { label: string; value: number; tone: string }) {
  return (
    <div className="card px-4 py-3">
      <div className="kicker">{label}</div>
      <div className="mt-0.5 flex items-center gap-2">
        <span className="font-headline text-xl font-semibold">{value}</span>
        <span className={`badge ${tone}`} aria-hidden />
      </div>
    </div>
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
  const targets = useAsync(() => notificationApi.targetOptions(), [])
  const packages = useAsync(() => masterDataApi.list({ kind: 'service_package', active: 'true' }), [])
  const teams = useAsync(() => api.teams(), [])
  const existing = useAsync(
    () => (editId ? workItemsApi.get(editId) : Promise.resolve(null)),
    [editId],
  )

  const item = existing.data?.item
  const rfs = item?.rfs

  const [customerName, setCustomerName] = useState('')
  const [servicePackage, setServicePackage] = useState('')
  const [priority, setPriority] = useState('normal')
  const [bandwidth, setBandwidth] = useState('')
  const [serviceID, setServiceID] = useState('')
  const [site, setSite] = useState('')
  const [picTeamID, setPicTeamID] = useState('')
  const [picSales, setPicSales] = useState('')
  const [deviceRef, setDeviceRef] = useState('')
  const [installStage, setInstallStage] = useState('planned')
  const [rfsDate, setRfsDate] = useState('')
  const [targetId, setTargetId] = useState('')
  const [notes, setNotes] = useState('')
  const [tags, setTags] = useState<string[]>([])
  const [initialised, setInitialised] = useState(false)
  const [busy, setBusy] = useState(false)

  const pkgEntries: MasterDataEntry[] = packages.data?.entries ?? []

  // Isi form saat data existing tiba (mode edit).
  if (editId && item && !initialised) {
    setCustomerName(rfs?.customer_name ?? '')
    setServicePackage(rfs?.service_package ?? '')
    setPriority(item.priority ?? 'normal')
    setBandwidth(rfs?.bandwidth ?? '')
    setServiceID(rfs?.service_id ?? '')
    setSite(rfs?.site ?? '')
    setPicTeamID(rfs?.pic_team_id ?? '')
    setPicSales(rfs?.pic_sales ?? '')
    setDeviceRef(item.device_ref ?? '')
    setInstallStage(rfs?.install_stage ?? 'planned')
    setNotes(item.description ?? '')
    setTags(item.tags ?? [])
    if (item.expire_at) {
      const d = new Date(item.expire_at)
      const pad = (n: number) => String(n).padStart(2, '0')
      setRfsDate(
        `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`,
      )
    }
    setInitialised(true)
  }

  function toggleTag(code: string) {
    setTags((prev) => (prev.includes(code) ? prev.filter((t) => t !== code) : [...prev, code]))
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!rfsDate) {
      onError('Tanggal RFS wajib diisi')
      return
    }
    setBusy(true)
    try {
      const expireAt = new Date(rfsDate).toISOString()
      const payload = {
        title: `Aktivasi/EWO — ${customerName}`,
        description: notes,
        priority,
        expire_at: expireAt,
        device_ref: deviceRef,
        tags,
        rfs: {
          customer_name: customerName,
          service_id: serviceID,
          service_package: servicePackage,
          bandwidth,
          pic_team_id: picTeamID || undefined,
          pic_sales: picSales,
          site,
          install_stage: installStage,
        },
      }

      if (editId) {
        await workItemsApi.update(editId, payload)
      } else {
        await workItemsApi.create({
          ...payload,
          item_type: 'rfs',
          status: 'planned',
          target_id: targetId || undefined,
        })
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
      title={editId ? 'Ubah Aktivasi / EWO' : 'Aktivasi / EWO Baru'}
      onClose={onClose}
      width="lg"
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !customerName.trim() || !rfsDate}>
            {busy ? 'Menyimpan…' : editId ? 'Simpan Perubahan' : 'Buat'}
          </button>
        </>
      }
    >
      {editId && existing.loading && <LoadingBlock label="Memuat data…" />}

      <form onSubmit={submit} className="space-y-3.5">
        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="rf-customer">
              Nama Customer
            </label>
            <input
              id="rf-customer"
              className="input"
              required
              autoFocus
              value={customerName}
              onChange={(e) => setCustomerName(e.target.value)}
              placeholder="PT Contoh Jaya"
            />
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
          <div>
            <label className="label-field" htmlFor="rf-serviceid">
              Service ID
            </label>
            <input
              id="rf-serviceid"
              className="input mono"
              value={serviceID}
              onChange={(e) => setServiceID(e.target.value)}
              placeholder="opsional"
            />
          </div>
        </div>

        <div>
          <div className="label-field">Tag (opsional)</div>
          <div className="flex flex-wrap gap-1.5">
            {EWO_TAG_PRESETS.map((code) => {
              const on = tags.includes(code)
              return (
                <button
                  key={code}
                  type="button"
                  onClick={() => toggleTag(code)}
                  className={`badge border transition ${
                    on
                      ? 'border-primary bg-primary-container text-on-primary-container'
                      : 'border-outline-variant bg-surface-container text-text-secondary'
                  }`}
                >
                  {labelTag(code)}
                </button>
              )
            })}
          </div>
          <p className="mt-1 text-label-sm text-text-secondary">
            Preset: Change Service, Upgrade, New, Request, Urgent.
          </p>
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="rf-stage">
              Tahap instalasi
            </label>
            <select id="rf-stage" className="input" value={installStage} onChange={(e) => setInstallStage(e.target.value)}>
              <option value="planned">Planned</option>
              <option value="provisioning">Provisioning</option>
              <option value="delivered">Delivered</option>
              <option value="activated">Activated</option>
              <option value="postponed">Postponed</option>
              <option value="cancelled">Cancelled</option>
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="rf-site">
              Site / lokasi
            </label>
            <input id="rf-site" className="input" value={site} onChange={(e) => setSite(e.target.value)} placeholder="CGK-1" />
          </div>
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
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
          </div>
          <div>
            <label className="label-field" htmlFor="rf-picsales">
              PIC Sales
            </label>
            <input id="rf-picsales" className="input" value={picSales} onChange={(e) => setPicSales(e.target.value)} placeholder="Sari" />
          </div>
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="rf-device">
              Perangkat (referensi)
            </label>
            <input
              id="rf-device"
              className="input mono"
              value={deviceRef}
              onChange={(e) => setDeviceRef(e.target.value)}
              placeholder="MX204-CGK1"
            />
          </div>
          {!editId && (
            <div>
              <label className="label-field" htmlFor="rf-target">
                Target notifikasi
              </label>
              <select id="rf-target" className="input" value={targetId} onChange={(e) => setTargetId(e.target.value)}>
                <option value="">— Default sistem (NOC-Team) —</option>
                {(targets.data?.targets ?? []).map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>

        <div>
          <label className="label-field" htmlFor="rf-notes">
            Catatan
          </label>
          <textarea
            id="rf-notes"
            className="input h-16 py-2"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            placeholder="Detail teknis, kebutuhan khusus, dsb."
          />
        </div>
      </form>
    </Modal>
  )
}
