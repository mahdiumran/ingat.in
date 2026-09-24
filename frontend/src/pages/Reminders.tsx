import { useEffect, useState } from 'react'
import { ApiUser, EscalationPolicy, notificationApi, WorkItem, workItemsApi } from '../api'
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

/** Status Reminder (sinkron dengan workitems.DefaultWorkflows["reminder"]). */
export const REMINDER_STATUSES = ['scheduled', 'active', 'expiring', 'expired', 'cancelled']

/** Menghitung sisa waktu dari sebuah timestamp, dalam teks singkat. */
function remainingText(expireAt?: string): { text: string; tone: string } {
  if (!expireAt) return { text: '—', tone: 'text-text-secondary' }
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
}: {
  user: ApiUser | null
  toast: Toast
  focusItemId?: string | null
  onFocusConsumed?: () => void
}) {
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [category, setCategory] = useState('')
  const [offset, setOffset] = useState(0)
  const [showCreate, setShowCreate] = useState(false)
  const [editing, setEditing] = useState<WorkItem | null>(null)
  const [detailID, setDetailID] = useState<string | null>(null)
  const [notifyingID, setNotifyingID] = useState<string | null>(null)
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
                  const rem = remainingText(it.expire_at)
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
                        <div className="flex justify-end gap-1">
                          <button className="btn-ghost h-8 w-8 px-0" title="Detail" onClick={() => setDetailID(it.id)}>
                            <span className="material-symbols-outlined text-[18px]">open_in_new</span>
                          </button>
                          {canWrite && (
                            <>
                              <button
                                className="btn-ghost h-8 w-8 px-0"
                                title="Kirim notifikasi sekarang"
                                disabled={notifyingID === it.id}
                                onClick={() => handleNotify(it)}
                              >
                                <span className={`material-symbols-outlined text-[18px] ${notifyingID === it.id ? 'animate-spin' : ''}`}>
                                  {notifyingID === it.id ? 'progress_activity' : 'send'}
                                </span>
                              </button>
                              <button className="btn-ghost h-8 w-8 px-0" title="Edit" onClick={() => setEditing(it)}>
                                <span className="material-symbols-outlined text-[18px]">edit</span>
                              </button>
                              {canManage(it) && (
                                <button
                                  className="btn-ghost h-8 w-8 px-0 text-critical"
                                  title="Hapus"
                                  onClick={() => handleDelete(it)}
                                >
                                  <span className="material-symbols-outlined text-[18px]">delete</span>
                                </button>
                              )}
                            </>
                          )}
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
  // Mode buat memakai durasi; mode edit memakai waktu expire absolut.
  const [expireLocal, setExpireLocal] = useState(toLocalInput(entry?.expire_at))
  const [hours, setHours] = useState(24)
  const [busy, setBusy] = useState(false)

  // Pilih policy default saat data policies tersedia.
  const availablePolicies = (policies.data?.policies ?? []) as EscalationPolicy[]
  const suggestedPolicy = availablePolicies.find((p) => p.is_default)
  const effectivePolicyId = policyId || suggestedPolicy?.id || ''

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      if (isEdit && entry) {
        await workItemsApi.update(entry.id, {
          title: title || undefined,
          description: notes,
          owner_username: owner,
          device_ref: deviceRef,
          target_id: targetId || '',
          expire_at: expireLocal ? new Date(expireLocal).toISOString() : '',
          reminder: {
            category,
            subject_name: subjectName,
            subject_type: subjectType,
            escalation_policy_id: effectivePolicyId || undefined,
          },
        })
      } else {
        const expireAt = new Date(Date.now() + hours * 3_600_000).toISOString()
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
          <div>
            <label className="label-field" htmlFor="rm-hours">
              {isEdit ? 'Expire (waktu setempat)' : 'Durasi (jam)'}
            </label>
            {isEdit ? (
              <input
                id="rm-hours"
                type="datetime-local"
                className="input mono"
                value={expireLocal}
                onChange={(e) => setExpireLocal(e.target.value)}
              />
            ) : (
              <input
                id="rm-hours"
                type="number"
                min={1}
                max={8760}
                className="input mono"
                value={hours}
                onChange={(e) => setHours(Number(e.target.value) || 1)}
              />
            )}
            <p className="mt-1 text-label-sm text-text-secondary">
              Expire:{' '}
              {formatWIB(
                isEdit && expireLocal
                  ? new Date(expireLocal).toISOString()
                  : new Date(Date.now() + hours * 3_600_000).toISOString(),
              )}
            </p>
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
