import { useEffect, useState } from 'react'
import { ApiUser, EscalationPolicy, notificationApi, WorkItem, workItemsApi } from '../api'
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

/** Status Reminder (sinkron dengan workitems.DefaultWorkflows["reminder"]). */
export const REMINDER_STATUSES = ['scheduled', 'active', 'expiring', 'expired', 'resolved', 'cancelled']

/** Menghitung sisa waktu dari sebuah timestamp, dalam teks singkat.
 *  Tidak berjalan lagi setelah reminder resolved/cancelled. */
function remainingText(expireAt?: string, status?: string): { text: string; tone: string } {
  const isClosed = status === 'resolved' || status === 'cancelled'
  if (!expireAt || isClosed) return { text: '—', tone: 'text-text-secondary' }
  const ms = new Date(expireAt).getTime() - Date.now()
  const late = ms < 0
  const abs = Math.abs(ms)
  const days = Math.floor(abs / 86_400_000)
  const hours = Math.floor((abs % 86_400_000) / 3_600_000)
  const mins = Math.floor((abs % 3_600_000) / 60_000)

  let text: string
  if (days > 0) text = `${days}h ${hours}j`
  else if (hours > 0) text = `${hours}j ${mins}m`
  else text = `${mins}m`
  if (late) text += ' lewat'

  // Warna: merah bila lewat, amber bila < 24 jam, hijau bila masih jauh.
  const tone = late
    ? 'text-critical font-semibold'
    : abs < 86_400_000
      ? 'text-warning font-semibold'
      : 'text-success'
  return { text, tone }
}

/**
 * Reminders (F6) — item_type = reminder.
 *
 * Menyediakan preset "Trial Dedicated — 3 Hari" yang mengisi expire_at = now + 72 jam
 * dan memakai policy TRIAL-3D (H-2, H-1, H-0, LATE-1H).
 */
export default function Reminders({
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
  const [category, setCategory] = useState('')
  const [offset, setOffset] = useState(0)
  const [showCreate, setShowCreate] = useState(false)
  const [editing, setEditing] = useState<WorkItem | null>(null)
  const [detailID, setDetailID] = useState<string | null>(null)
  const [notifyingID, setNotifyingID] = useState<string | null>(null)
  const [resolvingID, setResolvingID] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)

  const tagColors = useTagColors()

  const isAdmin = user?.role === 'admin'

  /** canManage mengikuti aturan backend: admin, pembuat, atau owner. */
  function canManage(it: WorkItem): boolean {
    if (isAdmin) return true
    const u = (user?.username ?? '').toLowerCase()
    if (!u) return false
    return it.created_by?.toLowerCase() === u || it.owner_username?.toLowerCase() === u
  }

  async function handleDelete(it: WorkItem) {
    setConfirm({
      title: 'Hapus reminder?',
      body: `Reminder ${it.ref_no} — "${it.reminder?.subject_name || it.title}" akan dihapus. Tindakan ini tidak dapat dibatalkan.`,
      confirmLabel: 'Hapus',
      tone: 'danger',
      run: async () => {
        await workItemsApi.remove(it.id)
        toast('success', 'Reminder dihapus', it.ref_no)
        items.reload()
      },
    })
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

  async function handleNotify(it: WorkItem) {
    setNotifyingID(it.id)
    try {
      const res = await workItemsApi.triggerNotification(it.id, { offset_label: 'MANUAL' })
      if (res.added > 0) toast('success', 'Notifikasi dikirim', res.message)
      else toast('warning', 'Tidak ada pesan baru', 'Binding aktif mungkin sudah menerima pesan ini.')
      items.reload()
    } catch (err) {
      toast('error', 'Gagal mengirim notifikasi', err instanceof Error ? err.message : undefined)
    } finally {
      setNotifyingID(null)
    }
  }

  /**
   * handleResolve menandai reminder sudah ditangani (status `resolved`).
   * Setelah resolved, fanout berhenti mengantrikan ulang offset untuk item ini.
   */
  async function handleResolve(it: WorkItem) {
    setResolvingID(it.id)
    try {
      await workItemsApi.changeStatus(it.id, 'resolved')
      toast('success', 'Reminder ditandai selesai', `${it.ref_no} → resolved`)
      items.reload()
    } catch (err) {
      toast('error', 'Gagal menandai selesai', err instanceof Error ? err.message : undefined)
    } finally {
      setResolvingID(null)
    }
  }

  const debouncedSearch = useDebounced(search)
  const limit = 50

  const items = useAsync(
    () =>
      workItemsApi.list({
        type: 'reminder',
        ...(status ? { status } : {}),
        ...(debouncedSearch ? { q: debouncedSearch } : {}),
        limit: String(limit),
        offset: String(offset),
      }),
    [status, debouncedSearch, offset],
  )

  const all = items.data?.items ?? []
  // Filter kategori dilakukan di klien karena kategori tersimpan pada extension.
  const rows = category ? all.filter((i) => i.reminder?.category === category) : all
  const total = items.data?.total ?? 0
  const canWrite = !!user && user.role !== 'viewer'
  const page = Math.floor(offset / limit) + 1
  const maxPage = Math.max(1, Math.ceil(total / limit))

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
        title="Reminder"
        description="Pengingat berjenjang (H-7 … H-0 + eskalasi) untuk trial dan layanan."
        actions={
          <>
            <button className="btn-secondary" onClick={items.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            {canWrite && (
              <button className="btn-primary" onClick={() => setShowCreate(true)}>
                <span className="material-symbols-outlined text-[18px]">alarm_add</span>
                Add Reminder
              </button>
            )}
          </>
        }
      />

      {/* Filter */}
      <section className="card mb-5 p-4">
        <div className="grid gap-3 sm:grid-cols-3">
          <div>
            <label className="label-field" htmlFor="r-search">
              Cari
            </label>
            <input
              id="r-search"
              className="input"
              placeholder="subjek, ref…"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value)
                setOffset(0)
              }}
            />
          </div>
          <div>
            <label className="label-field" htmlFor="r-status">
              Status
            </label>
            <select
              id="r-status"
              className="input"
              value={status}
              onChange={(e) => {
                setStatus(e.target.value)
                setOffset(0)
              }}
            >
              <option value="">Semua</option>
              <option value="scheduled">Scheduled</option>
              <option value="active">Active</option>
              <option value="expiring">Expiring</option>
              <option value="expired">Expired</option>
              <option value="resolved">Resolved</option>
              <option value="cancelled">Cancelled</option>
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="r-category">
              Kategori
            </label>
            <select id="r-category" className="input" value={category} onChange={(e) => setCategory(e.target.value)}>
              <option value="">Semua</option>
              <option value="trial">Trial</option>
              <option value="rfs">RFS</option>
              <option value="maintenance">Maintenance</option>
              <option value="generic">Generic</option>
              <option value="custom">Custom</option>
            </select>
          </div>
        </div>
      </section>

      <section className="card">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3.5">
          <div>
            <h3 className="font-headline text-lg font-semibold">Daftar Reminder</h3>
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
            icon="alarm"
            title="Belum ada reminder"
            body="Gunakan tombol “Trial Dedicated — 3 Hari” untuk membuat reminder trial dengan pola H-2, H-1, H-0, dan eskalasi otomatis."
          />
        )}

        {!items.loading && !items.error && rows.length > 0 && (
          <div className="overflow-x-auto">
            <table className="table">
              <thead>
                <tr>
                  <th>Ref</th>
                  <th>Subjek / Judul</th>
                  <th>Kategori</th>
                  <th>Status</th>
                  <th>Tag</th>
                  <th>Expire (WIB)</th>
                  <th>Sisa</th>
                  <th>Offset terakhir</th>
                  <th className="w-px whitespace-nowrap text-right">Aksi</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((it) => {
                  const rem = remainingText(it.expire_at, it.status)
                  return (
                    <tr key={it.id}>
                      <td>
                        <button className="mono text-label-md text-primary hover:underline" onClick={() => setDetailID(it.id)}>
                          {it.ref_no}
                        </button>
                      </td>
                      <td className="max-w-[280px]">
                        <button className="block truncate text-left font-medium hover:underline" onClick={() => setDetailID(it.id)}>
                          {it.reminder?.subject_name || it.title}
                        </button>
                        <span className="truncate text-label-sm text-text-secondary">{it.title}</span>
                      </td>
                      <td>
                        <span className="badge bg-surface-container text-text-secondary border-outline-variant">
                          {it.reminder?.category ?? 'generic'}
                        </span>
                      </td>
                      <td>
                        <StatusBadge status={it.status} tone={statusTone[it.status]} />
                      </td>
                      <td className="max-w-[160px]">
                        <TagList tags={it.tags} colors={tagColors} />
                      </td>
                      <td className="mono whitespace-nowrap text-label-md">{formatWIB(it.expire_at)}</td>
                      <td className={`mono whitespace-nowrap text-label-md ${rem.tone}`}>{rem.text}</td>
                      <td className="mono text-label-md text-text-secondary">
                        {it.reminder?.last_offset_fired || '—'}
                      </td>
                      <td className="w-px whitespace-nowrap">
                        <div className="flex justify-end">
                          <ActionMenu
                            items={[
                              { label: 'Lihat detail', icon: 'open_in_new', onClick: () => setDetailID(it.id) },
                              ...(canWrite
                                ? [
                                    {
                                      label: 'Tandai selesai',
                                      icon: resolvingID === it.id ? 'progress_activity' : 'task_alt',
                                      tone: 'success' as const,
                                      disabled: resolvingID === it.id || it.status === 'resolved' || it.status === 'cancelled',
                                      onClick: () => handleResolve(it),
                                    },
                                    {
                                      label: 'Kirim notifikasi',
                                      icon: notifyingID === it.id ? 'progress_activity' : 'send',
                                      disabled: notifyingID === it.id,
                                      onClick: () => handleNotify(it),
                                    },
                                    { label: 'Edit reminder', icon: 'edit', onClick: () => setEditing(it) },
                                  ]
                                : []),
                              ...(canWrite && canManage(it)
                                ? [{ label: 'Hapus reminder', icon: 'delete', tone: 'danger' as const, onClick: () => handleDelete(it) }]
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
        <ReminderForm
          onClose={() => {
            setShowCreate(false)
          }}
          onSaved={() => {
            setShowCreate(false)
            toast('success', 'Reminder dibuat', 'Peringatan H-2, H-1, H-0 akan dikirim otomatis.')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membuat reminder', msg)}
        />
      )}

      {editing && (
        <ReminderForm
          entry={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            toast('success', 'Reminder diperbarui')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal menyimpan reminder', msg)}
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

/** toLocalInput mengubah ISO ke format datetime-local (waktu lokal browser). */
function toLocalInput(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** splitDateTime memisahkan ISO menjadi bagian tanggal (YYYY-MM-DD) dan jam 24h (HH:MM). */
function splitDateTime(iso?: string): { date: string; time: string } {
  const local = toLocalInput(iso) // "YYYY-MM-DDTHH:MM"
  if (!local) return { date: '', time: '' }
  const [date, time] = local.split('T')
  return { date: date ?? '', time: time ?? '' }
}

/** defaultExpire mengembalikan tanggal & jam default (24 jam dari sekarang), format 24 jam. */
function defaultExpire(): { date: string; time: string } {
  const d = new Date(Date.now() + 24 * 3_600_000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return {
    date: `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`,
    time: `${pad(d.getHours())}:${pad(d.getMinutes())}`,
  }
}

/** combineDateTime menggabungkan tanggal + jam (24 jam) menjadi ISO string. */
function combineDateTime(date: string, time: string): string {
  if (!date) return ''
  const iso = new Date(`${date}T${time || '00:00'}`)
  if (Number.isNaN(iso.getTime())) return ''
  return iso.toISOString()
}

export function ReminderForm({
  entry,
  onClose,
  onSaved,
  onError,
}: {
  entry?: WorkItem
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const isEdit = !!entry
  const targets = useAsync(() => notificationApi.targetOptions(), [])
  const policies = useAsync(() => notificationApi.policies(), [])

  const [title, setTitle] = useState(entry?.title ?? '')
  const [subjectName, setSubjectName] = useState(entry?.reminder?.subject_name ?? '')
  const [subjectType, setSubjectType] = useState(entry?.reminder?.subject_type ?? 'customer')
  const [category, setCategory] = useState(entry?.reminder?.category ?? 'generic')
  const [notes, setNotes] = useState(entry?.description ?? '')
  const [owner, setOwner] = useState(entry?.owner_username ?? '')
  const [deviceRef, setDeviceRef] = useState(entry?.device_ref ?? '')
  const [targetId, setTargetId] = useState(entry?.target_id ?? '')
  const [policyId, setPolicyId] = useState(entry?.reminder?.escalation_policy_id ?? '')
  // Tanggal & jam expire memakai picker kalender + jam (format 24 jam).
  const initialExpire = splitDateTime(entry?.expire_at)
  const fallbackExpire = defaultExpire()
  const [expireDate, setExpireDate] = useState(initialExpire.date || fallbackExpire.date)
  const [expireTime, setExpireTime] = useState(initialExpire.time || fallbackExpire.time)
  const [busy, setBusy] = useState(false)

  // Pilih policy default saat data policies tersedia.
  const availablePolicies = (policies.data?.policies ?? []) as EscalationPolicy[]
  const suggestedPolicy = availablePolicies.find((p) => p.is_default)
  const effectivePolicyId = policyId || suggestedPolicy?.id || ''

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    const expireAt = combineDateTime(expireDate, expireTime)
    if (!expireAt) {
      onError('Tanggal & jam expire wajib diisi')
      return
    }
    setBusy(true)
    try {
      if (isEdit && entry) {
        await workItemsApi.update(entry.id, {
          title: title || undefined,
          description: notes,
          owner_username: owner,
          device_ref: deviceRef,
          target_id: targetId || '',
          expire_at: expireAt,
          reminder: {
            category,
            subject_name: subjectName,
            subject_type: subjectType,
            escalation_policy_id: effectivePolicyId || undefined,
          },
        })
      } else {
        await workItemsApi.create({
          item_type: 'reminder',
          title: title || `Reminder — ${subjectName}`,
          description: notes,
          priority: 'normal',
          status: 'active',
          owner_username: owner,
          expire_at: expireAt,
          target_id: targetId || undefined,
          device_ref: deviceRef,
          reminder: {
            category,
            subject_name: subjectName,
            subject_type: subjectType,
            escalation_policy_id: effectivePolicyId || undefined,
          },
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
      title={isEdit ? `Edit Reminder — ${entry?.ref_no}` : 'Add Reminder'}
      onClose={onClose}
      width="md"
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !subjectName.trim()}>
            {busy ? 'Menyimpan…' : isEdit ? 'Simpan Perubahan' : 'Buat Reminder'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="rm-subject">
              Nama subjek (customer/layanan)
            </label>
            <input
              id="rm-subject"
              className="input"
              required
              autoFocus
              value={subjectName}
              onChange={(e) => setSubjectName(e.target.value)}
              placeholder="mis. PT Contoh Jaya"
            />
          </div>
          <div>
            <label className="label-field" htmlFor="rm-type">
              Jenis subjek
            </label>
            <select id="rm-type" className="input" value={subjectType} onChange={(e) => setSubjectType(e.target.value)}>
              <option value="customer">Customer</option>
              <option value="service">Layanan</option>
              <option value="device">Perangkat</option>
              <option value="none">Tanpa subjek</option>
            </select>
          </div>
        </div>

        <div>
          <label className="label-field" htmlFor="rm-title">
            Judul reminder
          </label>
          <input
            id="rm-title"
            className="input"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="kosongkan untuk judul otomatis"
          />
        </div>

        <div className="grid gap-3.5 sm:grid-cols-3">
          <div>
            <label className="label-field" htmlFor="rm-category">
              Kategori
            </label>
            <select id="rm-category" className="input" value={category} onChange={(e) => setCategory(e.target.value)}>
              <option value="trial">Trial</option>
              <option value="rfs">RFS</option>
              <option value="maintenance">Maintenance</option>
              <option value="generic">Generic</option>
              <option value="custom">Custom</option>
            </select>
          </div>
          <div className="sm:col-span-2">
            <div className="label-field">Tanggal &amp; jam expire (WIB · 24 jam)</div>
            <div className="grid gap-2 sm:grid-cols-2">
              <div>
                <label className="label-field" htmlFor="rm-date">
                  Tanggal
                </label>
                <input
                  id="rm-date"
                  type="date"
                  className="input mono"
                  value={expireDate}
                  onChange={(e) => setExpireDate(e.target.value)}
                />
              </div>
              <div>
                <label className="label-field" htmlFor="rm-time">
                  Jam
                </label>
                <input
                  id="rm-time"
                  type="time"
                  step={60}
                  className="input mono"
                  value={expireTime}
                  onChange={(e) => setExpireTime(e.target.value)}
                />
              </div>
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-2">
              <p className="text-label-sm text-text-secondary">
                Expire:{' '}
                <span className="mono">{formatWIB(combineDateTime(expireDate, expireTime) || undefined)}</span>
              </p>
              <button
                type="button"
                className="btn-ghost h-7 px-2 text-label-sm"
                onClick={() => {
                  const q = defaultExpire()
                  setExpireDate(q.date)
                  setExpireTime(q.time)
                }}
              >
                <span className="material-symbols-outlined text-[15px]">schedule</span>
                +24 jam
              </button>
            </div>
          </div>
        </div>

        <div>
          <label className="label-field" htmlFor="rm-device">
            Perangkat
          </label>
          <input
            id="rm-device"
            className="input mono"
            value={deviceRef}
            onChange={(e) => setDeviceRef(e.target.value)}
            placeholder="opsional"
          />
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="rm-policy">
              Policy eskalasi
            </label>
            <select
              id="rm-policy"
              className="input"
              value={effectivePolicyId}
              onChange={(e) => setPolicyId(e.target.value)}
            >
              <option value="">— Default sistem —</option>
              {availablePolicies.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} ({p.offsets.length} offset)
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="rm-target">
              Target notifikasi
            </label>
            <select id="rm-target" className="input" value={targetId} onChange={(e) => setTargetId(e.target.value)}>
              <option value="">— Default sistem (NOC) —</option>
              {(targets.data?.targets ?? []).map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div>
          <label className="label-field" htmlFor="rm-owner">
            PIC / owner
          </label>
          <input
            id="rm-owner"
            className="input mono"
            value={owner}
            onChange={(e) => setOwner(e.target.value)}
            placeholder="username PIC NOC"
          />
        </div>

        <div>
          <label className="label-field" htmlFor="rm-notes">
            Catatan
          </label>
          <textarea
            id="rm-notes"
            className="input h-16 py-2"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            placeholder="Detail layanan, bandwidth, dsb."
          />
        </div>
      </form>
    </Modal>
  )
}
