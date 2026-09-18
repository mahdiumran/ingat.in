import { useEffect, useState } from 'react'
import { ApiUser, notificationApi, Target, workItemsApi } from '../api'
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
import { statusTone } from '../design/tokens'
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
 * RFS (F7) — item_type = rfs.
 *
 * Data RFS diisi admin sales; NOC menerima pengingat otomatis pada
 * H-7, H-3, H-1, dan H-0, serta eskalasi bila RFS terlewat.
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
  const [offset, setOffset] = useState(0)
  const [showCreate, setShowCreate] = useState(false)
  const [editItem, setEditItem] = useState<string | null>(null)
  const [detailID, setDetailID] = useState<string | null>(null)
  const [notifyingID, setNotifyingID] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)

  const tagColors = useTagColors()

  const isAdmin = user?.role === 'admin'

  /** canManage mengikuti aturan backend: admin, pembuat, atau owner. */
  function canManage(item: { created_by?: string; owner_username?: string }): boolean {
    if (isAdmin) return true
    const u = (user?.username ?? '').toLowerCase()
    if (!u) return false
    return item.created_by?.toLowerCase() === u || item.owner_username?.toLowerCase() === u
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
  const rows = stage ? all.filter((i) => i.rfs?.install_stage === stage) : all
  const total = items.data?.total ?? 0
  const canWrite = !!user && user.role !== 'viewer'
  const page = Math.floor(offset / limit) + 1
  const maxPage = Math.max(1, Math.ceil(total / limit))

  // Ringkasan cepat 7 hari ke depan.
  const soon = all.filter((i) => {
    if (!i.expire_at || i.status === 'activated' || i.status === 'cancelled') return false
    const diff = new Date(i.expire_at).getTime() - Date.now()
    return diff >= 0 && diff <= 7 * 86_400_000
  }).length

  const overdue = all.filter((i) => {
    if (!i.expire_at || i.status === 'activated' || i.status === 'cancelled') return false
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

  function markActivated(id: string, refNo: string) {
    setConfirm({
      title: 'Tandai RFS aktif?',
      body: `RFS ${refNo} akan ditandai aktif dan tahap instalasi menjadi "activated".`,
      confirmLabel: 'Tandai Aktif',
      tone: 'primary',
      icon: 'play_circle',
      run: async () => {
        await workItemsApi.update(id, { rfs: { install_stage: 'activated' } })
        try {
          await workItemsApi.changeStatus(id, 'activated', 'Ditandai aktif')
        } catch {
          /* tahap tetap tersimpan walau transisi status tidak diizinkan */
        }
        toast('success', 'RFS ditandai aktif', refNo)
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
        title="Data RFS"
        description="Ready For Service — diinput admin sales, NOC diingatkan otomatis pada H-7/H-3/H-1/H-0."
        actions={
          <>
            <button className="btn-secondary" onClick={items.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            {canWrite && (
              <button className="btn-primary" onClick={() => setShowCreate(true)}>
                <span className="material-symbols-outlined text-[18px]">event_available</span>
                RFS Baru
              </button>
            )}
          </>
        }
      />

      {/* Ringkasan */}
      <div className="mb-5 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <MiniCard label="Total RFS" value={total} tone="bg-surface-container text-text-secondary border-outline-variant" />
        <MiniCard label="7 hari ke depan" value={soon} tone="bg-warning-container text-on-warning-container border-outline-variant" />
        <MiniCard label="Terlewat" value={overdue} tone={overdue > 0 ? 'bg-critical-container text-on-critical-container border-critical' : 'bg-surface-container text-text-secondary border-outline-variant'} />
        <MiniCard
          label="Aktif"
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
              placeholder="customer, paket, ref…"
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
              <option value="in_progress">In progress</option>
              <option value="in_progress_field">Pemasangan lapangan</option>
              <option value="activated">Activated</option>
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
      </section>

      <section className="card">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3.5">
          <div>
            <h3 className="font-headline text-lg font-semibold">Daftar RFS</h3>
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
            title="Belum ada data RFS"
            body="Tambahkan RFS dengan tanggal layanan aktif; NOC akan diingatkan otomatis mendekati tanggal tersebut."
            action={
              canWrite ? (
                <button className="btn-primary" onClick={() => setShowCreate(true)}>
                  <span className="material-symbols-outlined text-[18px]">event_available</span>
                  Tambah RFS
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
                  <th>Paket / Bandwidth</th>
                  <th>RFS (WIB)</th>
                  <th>Hitung mundur</th>
                  <th>Site</th>
                  <th>PIC NOC</th>
                  <th>Status</th>
                  <th>Tag</th>
                  <th className="w-px whitespace-nowrap text-right">Aksi</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((it) => {
                  const cd = countdown(it.expire_at)
                  const rfs = it.rfs
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
                      <td className="mono whitespace-nowrap text-label-md">{formatWIB(it.expire_at)}</td>
                      <td className={`mono whitespace-nowrap text-label-md ${cd.tone}`}>{cd.text}</td>
                      <td className="text-text-secondary">{rfs?.site || '—'}</td>
                      <td className="mono text-label-md text-text-secondary">{rfs?.pic_noc || '—'}</td>
                      <td>
                        <StatusBadge status={it.status} tone={statusTone[it.status]} />
                      </td>
                      <td className="max-w-[160px]">
                        <TagList tags={it.tags} colors={tagColors} />
                      </td>
                      <td className="w-px whitespace-nowrap">
                        <div className="flex justify-end gap-1">
                          {canWrite && rfs?.install_stage !== 'activated' && it.status !== 'cancelled' && (
                            <button
                              className="btn-ghost h-8 w-8 px-0 text-success"
                              title="Tandai Aktif"
                              onClick={() => markActivated(it.id, it.ref_no)}
                            >
                              <span className="material-symbols-outlined text-[18px]">play_circle</span>
                            </button>
                          )}
                          {canWrite && canManage(it) && (
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
            toast('success', 'Data RFS dibuat', 'NOC akan diingatkan pada H-7, H-3, H-1, dan H-0.')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membuat RFS', msg)}
        />
      )}

      {editItem && (
        <RFSForm
          editId={editItem}
          onClose={() => setEditItem(null)}
          onSaved={() => {
            setEditItem(null)
            toast('success', 'Data RFS diperbarui')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal memperbarui RFS', msg)}
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
  const targets = useAsync(() => notificationApi.targets(), [])
  const existing = useAsync(
    () => (editId ? workItemsApi.get(editId) : Promise.resolve(null)),
    [editId],
  )

  const item = existing.data?.item
  const rfs = item?.rfs

  const [customerName, setCustomerName] = useState('')
  const [servicePackage, setServicePackage] = useState('')
  const [bandwidth, setBandwidth] = useState('')
  const [serviceID, setServiceID] = useState('')
  const [site, setSite] = useState('')
  const [picNoc, setPicNoc] = useState('')
  const [picSales, setPicSales] = useState('')
  const [deviceRef, setDeviceRef] = useState('')
  const [installStage, setInstallStage] = useState('planned')
  const [rfsDate, setRfsDate] = useState('')
  const [targetId, setTargetId] = useState('')
  const [notes, setNotes] = useState('')
  const [initialised, setInitialised] = useState(false)
  const [busy, setBusy] = useState(false)

  // Isi form saat data existing tiba (mode edit).
  if (editId && item && !initialised) {
    setCustomerName(rfs?.customer_name ?? '')
    setServicePackage(rfs?.service_package ?? '')
    setBandwidth(rfs?.bandwidth ?? '')
    setServiceID(rfs?.service_id ?? '')
    setSite(rfs?.site ?? '')
    setPicNoc(rfs?.pic_noc ?? '')
    setPicSales(rfs?.pic_sales ?? '')
    setDeviceRef(item.device_ref ?? '')
    setInstallStage(rfs?.install_stage ?? 'planned')
    setNotes(item.description ?? '')
    if (item.expire_at) {
      // datetime-local memerlukan format tanpa zona.
      const d = new Date(item.expire_at)
      const pad = (n: number) => String(n).padStart(2, '0')
      setRfsDate(
        `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`,
      )
    }
    setInitialised(true)
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
        title: `RFS — ${customerName}`,
        description: notes,
        expire_at: expireAt,
        device_ref: deviceRef,
        rfs: {
          customer_name: customerName,
          service_id: serviceID,
          service_package: servicePackage,
          bandwidth,
          pic_noc: picNoc,
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
          priority: 'high',
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
      title={editId ? 'Ubah Data RFS' : 'Data RFS Baru'}
      onClose={onClose}
      width="lg"
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !customerName.trim() || !rfsDate}>
            {busy ? 'Menyimpan…' : editId ? 'Simpan Perubahan' : 'Buat RFS'}
          </button>
        </>
      }
    >
      {editId && existing.loading && <LoadingBlock label="Memuat data…" />}

      <form onSubmit={submit} className="space-y-3.5">
        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="rf-customer">
              Nama customer
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

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="rf-package">
              Paket layanan
            </label>
            <input
              id="rf-package"
              className="input"
              value={servicePackage}
              onChange={(e) => setServicePackage(e.target.value)}
              placeholder="Dedicated Internet"
            />
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
              Tanggal & jam RFS
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
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="rf-picnoc">
              PIC NOC
            </label>
            <input id="rf-picnoc" className="input" value={picNoc} onChange={(e) => setPicNoc(e.target.value)} placeholder="Budi" />
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
            <label className="label-field" htmlFor="rf-site">
              Site / lokasi
            </label>
            <input id="rf-site" className="input" value={site} onChange={(e) => setSite(e.target.value)} placeholder="CGK-1" />
          </div>
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
        </div>

        {!editId && (
          <div>
            <label className="label-field" htmlFor="rf-target">
              Target notifikasi
            </label>
            <select id="rf-target" className="input" value={targetId} onChange={(e) => setTargetId(e.target.value)}>
              <option value="">— Default sistem (NOC-Team) —</option>
              {((targets.data?.targets ?? []) as Target[]).map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </div>
        )}

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
