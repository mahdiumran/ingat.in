import { useState } from 'react'
import { ApiUser, workItemsApi } from '../api'
import {
  ConfirmDialog,
  EmptyState,
  ErrorState,
  LoadingBlock,
  PriorityBadge,
  StatusBadge,
  StatusSelect,
  TagList,
  formatWIB,
} from '../components/ui'
import { useAsync, useTagColors } from '../hooks'
import { slaTone, formatDuration } from '../lib/format'
import { statusTone } from '../design/tokens'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

type Tab = 'ringkasan' | 'aktivitas' | 'action'

type ConfirmState = {
  title: string
  body?: string
  confirmLabel?: string
  tone?: 'primary' | 'danger'
  icon?: string
  run: () => Promise<void>
}

const TABS: { id: Tab; label: string; icon: string }[] = [
  { id: 'ringkasan', label: 'Ringkasan', icon: 'summarize' },
  { id: 'aktivitas', label: 'Aktivitas', icon: 'history' },
  { id: 'action', label: 'Action', icon: 'bolt' },
]

function typeLabel(itemType: string): string {
  switch (itemType) {
    case 'rfs':
      return 'RFS'
    case 'reminder':
      return 'Reminder'
    case 'incident':
      return 'Insiden'
    case 'request':
      return 'Permintaan'
    case 'change':
      return 'Change'
    case 'daily_task':
      return 'Daily Task'
    default:
      return 'Task'
  }
}

/**
 * WorkItemDetail — SATU halaman untuk SEMUA tipe work item dengan 3 tab:
 * Ringkasan | Aktivitas | Action. Perubahan status (workflow) dipindahkan ke
 * tab Action (lihat DESIGN.md).
 */
export function ItemDetail({
  id,
  user,
  toast,
  onBack,
}: {
  id: string
  user: ApiUser | null
  toast: Toast
  onBack: () => void
}) {
  const detail = useAsync(() => workItemsApi.get(id), [id])
  const tagColors = useTagColors()

  const [tab, setTab] = useState<Tab>('ringkasan')
  const [comment, setComment] = useState('')
  const [busy, setBusy] = useState(false)
  const [editingDesc, setEditingDesc] = useState(false)
  const [descDraft, setDescDraft] = useState('')
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [forceStatus, setForceStatus] = useState('')
  const [forceReason, setForceReason] = useState('')

  const item = detail.data?.item
  const events = detail.data?.events ?? []
  const comments = detail.data?.comments ?? []
  const workflow = detail.data?.workflow
  const canWrite = !!user && user.role !== 'viewer'
  const isAdmin = user?.role === 'admin'

  // canManage: admin, pembuat, atau owner — selaras dengan aturan backend.
  const canManage =
    !!item &&
    (user?.role === 'admin' ||
      (!!user?.username &&
        (item.created_by?.toLowerCase() === user.username.toLowerCase() ||
          item.owner_username?.toLowerCase() === user.username.toLowerCase())))

  async function submitComment(e: React.FormEvent) {
    e.preventDefault()
    if (!comment.trim()) return
    setBusy(true)
    try {
      await workItemsApi.addComment(id, comment.trim())
      setComment('')
      toast('success', 'Komentar ditambahkan')
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal menambah komentar', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  async function doChangeStatus(next: string) {
    setBusy(true)
    try {
      await workItemsApi.changeStatus(id, next)
      toast('success', 'Status diperbarui', next.replace(/_/g, ' '))
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal mengubah status', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  // Force status (admin): melompati aturan transisi dengan alasan wajib.
  async function forceStatusAction() {
    if (!forceReason.trim() || forceStatus === item?.status) return
    setBusy(true)
    try {
      await workItemsApi.forceStatus(id, forceStatus, forceReason.trim())
      toast('success', 'Status dipaksa', `${item?.ref_no} → ${forceStatus.replace(/_/g, ' ')}`)
      setForceReason('')
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal memaksa status', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  // Konfirmasi sebelum mengubah status ke state terminal/berisiko.
  function changeStatus(next: string) {
    const terminal = ['closed', 'cancelled', 'canceled', 'activated', 'completed', 'rolled_back'].includes(next)
    if (!terminal) {
      void doChangeStatus(next)
      return
    }
    setConfirm({
      title: `Ubah status ke "${next.replace(/_/g, ' ')}"?`,
      body: 'Status ini bersifat final. Pastikan penanganan sudah selesai.',
      confirmLabel: 'Ubah Status',
      tone: next === 'cancelled' || next === 'canceled' ? 'danger' : 'primary',
      icon: 'warning',
      run: () => doChangeStatus(next),
    })
  }

  async function saveDescription() {    setBusy(true)
    try {
      await workItemsApi.update(id, { description: descDraft })
      toast('success', 'Deskripsi diperbarui')
      setEditingDesc(false)
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal menyimpan deskripsi', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  function deleteItem() {
    if (!item) return
    setConfirm({
      title: 'Hapus item?',
      body: `${item.ref_no} — "${item.title}" akan dihapus. Tindakan ini tidak dapat dibatalkan.`,
      confirmLabel: 'Hapus',
      tone: 'danger',
      run: async () => {
        await workItemsApi.remove(id)
        toast('success', 'Item dihapus', item.ref_no)
        onBack()
      },
    })
  }

  // Aksi "Tandai Aktif" untuk RFS.
  function markRFSActivated() {
    setConfirm({
      title: 'Tandai RFS aktif?',
      body: 'RFS akan ditandai aktif dan tahap instalasi menjadi "activated".',
      confirmLabel: 'Tandai Aktif',
      tone: 'primary',
      icon: 'play_circle',
      run: async () => {
        setBusy(true)
        try {
          await workItemsApi.update(id, { rfs: { install_stage: 'activated' } })
          try {
            await workItemsApi.changeStatus(id, 'activated', 'Ditandai aktif oleh operator')
          } catch {
            /* status tetap diperbarui bila transisi tidak diizinkan */
          }
          toast('success', 'RFS ditandai aktif')
          detail.reload()
        } finally {
          setBusy(false)
        }
      },
    })
  }

  async function sendNotification() {
    setBusy(true)
    try {
      const res = await workItemsApi.triggerNotification(id, { offset_label: 'MANUAL' })
      if (res.added > 0) {
        toast('success', 'Notifikasi dikirim', res.message)
      } else {
        toast('warning', 'Tidak ada pesan baru', 'Binding aktif mungkin sudah menerima pesan ini.')
      }
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal mengirim notifikasi', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  const nextStates = item && workflow ? (workflow.transitions[item.status] ?? []) : []
  const isTicket = item?.item_type === 'incident' || item?.item_type === 'request' || item?.item_type === 'change'

  return (
    <div>
      {/* Header */}
      <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <button className="btn-ghost mb-2 h-8 px-2" onClick={onBack}>
            <span className="material-symbols-outlined text-[18px]">arrow_back</span>
            Kembali
          </button>
          {item ? (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <span className="mono rounded-control border border-border bg-surface-container-low px-2 py-0.5 text-label-md">
                  {item.ref_no}
                </span>
                <StatusBadge status={item.status} tone={statusTone[item.status]} />
                <PriorityBadge priority={item.priority} />
                <span className="badge bg-surface-container text-text-secondary border-outline-variant">
                  {typeLabel(item.item_type)}
                </span>
              </div>
              <h2 className="mt-2 font-headline text-headline-md font-semibold">{item.title}</h2>
            </>
          ) : (
            <h2 className="font-headline text-headline-md font-semibold">Memuat…</h2>
          )}
        </div>

        {item && (
          <div className="flex flex-wrap gap-2">
            <button className="btn-secondary" onClick={detail.reload} disabled={busy}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
          </div>
        )}
      </div>

      {detail.loading && <LoadingBlock label="Memuat detail…" />}
      {detail.error && <ErrorState message={detail.error} onRetry={detail.reload} />}

      {item && !detail.loading && (
        <>
          {/* Tab header */}
          <div className="mb-5 flex flex-wrap gap-1 border-b border-border" role="tablist">
            {TABS.map((t) => {
              if (t.id === 'action' && !canWrite) return null
              const active = tab === t.id
              return (
                <button
                  key={t.id}
                  role="tab"
                  aria-selected={active}
                  onClick={() => setTab(t.id)}
                  className={`-mb-px flex items-center gap-1.5 border-b-2 px-4 py-2.5 text-label-md font-semibold transition ${
                    active
                      ? 'border-primary text-primary'
                      : 'border-transparent text-text-secondary hover:text-text-primary'
                  }`}
                >
                  <span className="material-symbols-outlined text-[18px]">{t.icon}</span>
                  {t.label}
                  {t.id === 'aktivitas' && (
                    <span className="badge border-outline-variant bg-surface-container text-text-secondary">
                      {events.length + comments.length}
                    </span>
                  )}
                </button>
              )
            })}
          </div>

          {/* ---------------- Tab: Ringkasan ---------------- */}
          {tab === 'ringkasan' && (
            <div className="grid gap-5 lg:grid-cols-3">
              <div className="space-y-5 lg:col-span-2">
                {/* Deskripsi */}
                <section className="card card-pad">
                  <div className="mb-2 flex items-center justify-between gap-2">
                    <h3 className="font-headline text-lg font-semibold">Deskripsi</h3>
                    {canManage && !editingDesc && (
                      <button
                        className="btn-ghost text-label-md"
                        onClick={() => {
                          setDescDraft(item.description ?? '')
                          setEditingDesc(true)
                        }}
                      >
                        <span className="material-symbols-outlined text-[17px]">edit</span>
                        Sunting
                      </button>
                    )}
                  </div>

                  {editingDesc ? (
                    <div>
                      <textarea
                        className="input h-28 py-2"
                        value={descDraft}
                        onChange={(e) => setDescDraft(e.target.value)}
                        placeholder="Langkah atau konteks tambahan…"
                      />
                      <div className="mt-2 flex justify-end gap-2">
                        <button className="btn-secondary" onClick={() => setEditingDesc(false)} disabled={busy}>
                          Batal
                        </button>
                        <button className="btn-primary" onClick={saveDescription} disabled={busy}>
                          {busy ? 'Menyimpan…' : 'Simpan'}
                        </button>
                      </div>
                    </div>
                  ) : (
                    <p className="whitespace-pre-wrap text-body-sm text-text-secondary">
                      {item.description || 'Belum ada deskripsi.'}
                    </p>
                  )}
                  {!canManage && (
                    <p className="mt-2 text-label-sm text-text-secondary">
                      Hanya pembuat item atau admin yang dapat menyunting deskripsi.
                    </p>
                  )}
                </section>

                {/* Detail per tipe */}
                <section className="card card-pad">
                  <h3 className="mb-3 font-headline text-lg font-semibold">Detail {typeLabel(item.item_type)}</h3>
                  <dl className="space-y-2.5">
                    <Field label="Dibuat oleh" value={createdByLabel(item.created_by, item.requester_username)} mono />
                    <Field label="Dibuat" value={formatWIB(item.created_at)} mono />

                    {!isTicket && <Field label="Tenggat" value={formatWIB(item.due_at)} mono />}
                    {!isTicket && <Field label="Expire" value={formatWIB(item.expire_at)} mono />}

                    <Field label="Perangkat" value={item.device_ref || '—'} mono />
                    <Field label="Layanan" value={item.service_ref || '—'} mono />

                    {item.reminder && (
                      <>
                        <Field label="Kategori" value={item.reminder.category} />
                        <Field label="Subjek" value={item.reminder.subject_name || '—'} />
                        <Field label="Offset terakhir" value={item.reminder.last_offset_fired || '—'} mono />
                        <Field
                          label="Status aktivasi"
                          value={item.reminder.activated_at ? `Aktif ${formatWIB(item.reminder.activated_at)}` : 'Belum diaktivasi'}
                        />
                      </>
                    )}

                    {item.rfs && (
                      <>
                        <Field label="Customer" value={item.rfs.customer_name || '—'} />
                        <Field label="Paket" value={item.rfs.service_package || '—'} />
                        <Field label="Bandwidth" value={item.rfs.bandwidth || '—'} mono />
                        <Field label="Site" value={item.rfs.site || '—'} />
                        <Field label="PIC NOC" value={item.rfs.pic_noc || '—'} />
                        <Field label="PIC Sales" value={item.rfs.pic_sales || '—'} />
                        <Field label="Tahap instalasi" value={item.rfs.install_stage} />
                      </>
                    )}

                    {item.ticket && (
                      <>
                        <Field label="Kategori" value={item.ticket.category || '—'} />
                        <Field label="Subkategori" value={item.ticket.subcategory || '—'} />
                        <Field label="Jenis gangguan" value={item.ticket.incident_type || '—'} />
                        <Field label="Dampak" value={item.ticket.impact} />
                        <Field label="Urgensi" value={item.ticket.urgency} />
                        <Field label="Grup penanganan" value={item.ticket.assignment_group || '—'} />
                        <Field label="Eskalasi" value={String(item.ticket.escalation_level)} mono />
                      </>
                    )}

                    {item.task && (
                      <>
                        <Field label="Progres" value={`${item.task.progress_pct}%`} />
                        <Field
                          label="Checklist"
                          value={`${item.task.checklist.filter((c) => c.done).length}/${item.task.checklist.length} selesai`}
                        />
                      </>
                    )}
                  </dl>

                  {item.tags.length > 0 && (
                    <div className="mt-3 border-t border-border pt-3">
                      <div className="kicker mb-1.5">Tag</div>
                      <TagList tags={item.tags} colors={tagColors} />
                    </div>
                  )}
                </section>
              </div>

              {/* Kolom samping — SLA / meta */}
              <div className="space-y-5">
                {isTicket && (
                  <section className="card card-pad">
                    <h3 className="mb-3 font-headline text-lg font-semibold">SLA Penanganan</h3>
                    {item.sla_start_at ? (
                      <>
                        <div className={`font-headline text-2xl font-bold ${slaTone(item.sla_seconds)}`}>
                          {formatDuration(item.sla_seconds)}
                          {item.sla_running && (
                            <span className="ml-2 align-middle text-label-sm font-normal text-text-secondary">
                              berjalan
                            </span>
                          )}
                        </div>
                        <dl className="mt-3 space-y-2.5">
                          <Field label="Mulai ditangani" value={formatWIB(item.sla_start_at)} mono />
                          <Field
                            label="Selesai"
                            value={item.sla_running ? 'Belum closed' : formatWIB(item.sla_end_at)}
                            mono
                          />
                        </dl>
                        <p className="mt-3 text-label-sm text-text-secondary">
                          Dihitung dari status pertama keluar dari "new" sampai tiket ditutup.
                        </p>
                      </>
                    ) : (
                      <p className="text-body-sm text-text-secondary">
                        Tiket belum ditangani. SLA mulai dihitung saat status berubah dari "new".
                      </p>
                    )}
                  </section>
                )}

                <section className="card card-pad">
                  <h3 className="mb-3 font-headline text-lg font-semibold">Informasi</h3>
                  <dl className="space-y-2.5">
                    <Field label="Prioritas" value={item.priority} />
                    <Field label="Status" value={item.status} />
                    <Field label="Pembuat" value={createdByLabel(item.created_by, item.requester_username)} mono />
                  </dl>
                </section>
              </div>
            </div>
          )}

          {/* ---------------- Tab: Aktivitas ---------------- */}
          {tab === 'aktivitas' && (
            <div className="grid gap-5 lg:grid-cols-3">
              <section className="card lg:col-span-2">
                <div className="border-b border-border px-5 py-3.5">
                  <h3 className="font-headline text-lg font-semibold">Diskusi</h3>
                  <p className="text-body-sm text-text-secondary">{comments.length} komentar</p>
                </div>

                <div className="divide-y divide-border">
                  {comments.length === 0 && (
                    <p className="px-5 py-6 text-center text-body-sm text-text-secondary">Belum ada komentar.</p>
                  )}
                  {comments.map((c) => (
                    <div key={c.id} className="flex gap-3 px-5 py-3.5">
                      <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-primary-container text-label-sm font-bold text-on-primary-container">
                        {c.author_username.slice(0, 2).toUpperCase()}
                      </span>
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-body-sm font-semibold">{c.author_username}</span>
                          <span className="mono text-label-sm text-text-secondary">{formatWIB(c.created_at, true)}</span>
                        </div>
                        <p className="mt-0.5 whitespace-pre-wrap text-body-sm text-text-secondary">{c.body}</p>
                      </div>
                    </div>
                  ))}
                </div>

                {canWrite && (
                  <form onSubmit={submitComment} className="border-t border-border p-5">
                    <label className="label-field" htmlFor="d-comment">
                      Tambah komentar
                    </label>
                    <textarea
                      id="d-comment"
                      className="input h-20 py-2"
                      value={comment}
                      onChange={(e) => setComment(e.target.value)}
                      placeholder="Catatan penanganan, hasil pengecekan, dsb."
                    />
                    <div className="mt-2 flex justify-end">
                      <button type="submit" className="btn-primary" disabled={busy || !comment.trim()}>
                        <span className="material-symbols-outlined text-[17px]">send</span>
                        Kirim
                      </button>
                    </div>
                  </form>
                )}
              </section>

              <section className="card">
                <div className="border-b border-border px-5 py-3.5">
                  <h3 className="font-headline text-lg font-semibold">Timeline</h3>
                  <p className="text-body-sm text-text-secondary">{events.length} kejadian</p>
                </div>

                {events.length === 0 ? (
                  <EmptyState icon="history" title="Belum ada aktivitas" />
                ) : (
                  <ol className="relative space-y-3 px-5 py-4">
                    {events.map((e) => (
                      <li key={e.id} className="flex gap-3">
                        <span
                          className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${
                            e.event_type.includes('fail') || e.event_type.includes('breach')
                              ? 'bg-critical'
                              : e.event_type.includes('sla_warning')
                                ? 'bg-warning'
                                : e.event_type === 'created'
                                  ? 'bg-primary'
                                  : 'bg-outline'
                          }`}
                          aria-hidden
                        />
                        <div className="min-w-0">
                          <div className="text-body-sm">
                            <span className="mono text-label-md">{e.event_type.replace(/_/g, ' ')}</span>
                            {e.from_value && e.to_value && (
                              <>
                                {' '}
                                <span className="text-text-secondary">
                                  <span className="mono">{e.from_value}</span> → <span className="mono">{e.to_value}</span>
                                </span>
                              </>
                            )}
                            {!e.from_value && e.to_value && (
                              <>
                                {' '}
                                <span className="mono text-text-secondary">{e.to_value}</span>
                              </>
                            )}
                          </div>
                          <div className="mono text-label-sm text-text-secondary">
                            {formatWIB(e.created_at, true)} · {e.actor_username || 'sistem'}
                          </div>
                        </div>
                      </li>
                    ))}
                  </ol>
                )}
              </section>
            </div>
          )}

          {/* ---------------- Tab: Action ---------------- */}
          {tab === 'action' && canWrite && (
            <div className="grid gap-5 lg:grid-cols-3">
              <div className="space-y-5 lg:col-span-2">
                {/* Rubah Status */}
                <section className="card card-pad">
                  <h3 className="mb-3 font-headline text-lg font-semibold">Rubah Status</h3>
                  {workflow ? (
                    <>
                      <div className="flex flex-wrap items-center gap-3">
                        <StatusSelect
                          value={item.status}
                          options={[item.status, ...nextStates]}
                          busy={busy}
                          onChange={(next) => changeStatus(next)}
                        />
                        <span className="text-label-sm text-text-secondary">
                          Pilih status baru, perubahan diterapkan langsung.
                        </span>
                      </div>
                      <p className="mt-3 text-label-sm text-text-secondary">
                        Status saat ini <span className="mono">{item.status}</span> · alur:{' '}
                        <span className="mono">{workflow.states.join(' → ')}</span>
                      </p>
                    </>
                  ) : (
                    <p className="text-body-sm text-text-secondary">Workflow untuk tipe ini tidak tersedia.</p>
                  )}
                </section>

                {/* Aksi lain */}
                <section className="card card-pad">
                  <h3 className="mb-3 font-headline text-lg font-semibold">Aksi Lain</h3>
                  <div className="flex flex-wrap gap-2">
                    {item.item_type === 'rfs' && item.rfs?.install_stage !== 'activated' && (
                      <button className="btn-brand" onClick={markRFSActivated} disabled={busy}>
                        <span className="material-symbols-outlined text-[18px]">play_circle</span>
                        Tandai Aktif
                      </button>
                    )}
                    <button className="btn-secondary" onClick={sendNotification} disabled={busy} title="Kirim notifikasi sekarang">
                      <span className="material-symbols-outlined text-[18px]">send</span>
                      Kirim Notifikasi
                    </button>
                    {canManage && (
                      <button className="btn-danger" onClick={deleteItem} disabled={busy} title="Hapus item">
                        <span className="material-symbols-outlined text-[18px]">delete</span>
                        Hapus
                      </button>
                    )}
                  </div>
                </section>

                {/* Force Status — admin saja */}
                {isAdmin && workflow && (
                  <section className="card card-pad border-critical/40">
                    <div className="mb-3 flex items-center gap-2">
                      <span className="material-symbols-outlined text-[20px] text-critical">warning</span>
                      <h3 className="font-headline text-lg font-semibold">Force Status (Admin)</h3>
                    </div>
                    <p className="mb-3 text-body-sm text-text-secondary">
                      Melompati aturan transisi untuk koreksi darurat, mis. mengembalikan tiket dari
                      canceled ke active/closed/pending. Alasan wajib diisi dan tercatat pada audit.
                    </p>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div>
                        <label className="label-field" htmlFor="force-status">
                          Status tujuan
                        </label>
                        <select
                          id="force-status"
                          className="input"
                          value={forceStatus}
                          onChange={(e) => setForceStatus(e.target.value)}
                        >
                          {workflow.states.map((s) => (
                            <option key={s} value={s}>
                              {s.replace(/_/g, ' ')}
                            </option>
                          ))}
                        </select>
                      </div>
                      <div>
                        <label className="label-field" htmlFor="force-reason">
                          Alasan (wajib)
                        </label>
                        <input
                          id="force-reason"
                          className="input"
                          value={forceReason}
                          onChange={(e) => setForceReason(e.target.value)}
                          placeholder="mis. salah tandai canceled"
                        />
                      </div>
                    </div>
                    <div className="mt-3 flex justify-end">
                      <button
                        className="btn-danger"
                        disabled={busy || !forceReason.trim() || forceStatus === item.status}
                        onClick={forceStatusAction}
                      >
                        <span className="material-symbols-outlined text-[18px]">bolt</span>
                        Paksa Status
                      </button>
                    </div>
                  </section>
                )}
              </div>

              <div className="space-y-5">
                <section className="card card-pad">
                  <h3 className="mb-2 font-headline text-lg font-semibold">Panduan</h3>
                  <ul className="list-disc space-y-1.5 pl-5 text-body-sm text-text-secondary">
                    <li>Status terminal (closed/canceled) meminta konfirmasi.</li>
                    <li>Kirim notifikasi mengantri ulang lewat outbox (bisa diulang).</li>
                    <li>Hapus hanya untuk pembuat item atau admin.</li>
                  </ul>
                </section>
              </div>
            </div>
          )}
        </>
      )}

      <ConfirmDialog
        open={!!confirm}
        title={confirm?.title ?? ''}
        body={confirm?.body}
        confirmLabel={confirm?.confirmLabel}
        tone={confirm?.tone}
        icon={confirm?.icon}
        busy={busy}
        onCancel={() => setConfirm(null)}
        onConfirm={async () => {
          if (!confirm) return
          try {
            await confirm.run()
            setConfirm(null)
          } catch (err) {
            toast('error', 'Aksi gagal', err instanceof Error ? err.message : undefined)
            setConfirm(null)
          }
        }}
      />
    </div>
  )
}

/** createdByLabel menampilkan pembuat dan (bila berbeda) requester. */
function createdByLabel(createdBy: string, requester: string): string {
  const c = createdBy || '—'
  if (requester && requester !== createdBy) return `${c} (req: ${requester})`
  return c
}

function Field({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <dt className="shrink-0 text-body-sm text-text-secondary">{label}</dt>
      <dd className={`text-right text-body-sm ${mono ? 'mono text-label-md' : ''}`}>{value}</dd>
    </div>
  )
}
