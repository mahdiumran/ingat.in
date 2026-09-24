import { useEffect, useRef, useState } from 'react'
import { api, ApiUser, Attachment, attachmentsApi, workItemsApi } from '../api'
import {
  ConfirmDialog,
  EmptyState,
  ErrorState,
  LoadingBlock,
  Modal,
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

type Tab = 'ringkasan' | 'aktivasi' | 'evidence' | 'aktivitas' | 'action'

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
  // F25: daftar tim untuk pemilihan PIC team (Aktivasi/EWO).
  const teams = useAsync(() => api.teams(), [])

  const [tab, setTab] = useState<Tab>('ringkasan')
  const [comment, setComment] = useState('')
  const [busy, setBusy] = useState(false)
  const [editingDesc, setEditingDesc] = useState(false)
  const [descDraft, setDescDraft] = useState('')
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [forceStatus, setForceStatus] = useState('')
  const [forceReason, setForceReason] = useState('')
  const [unlockStatus, setUnlockStatus] = useState('in_progress')
  const [unlockReason, setUnlockReason] = useState('')
  const [collabInput, setCollabInput] = useState('')
  const [assignInput, setAssignInput] = useState('')
  // F21/F22: catatan penanganan selalu dapat disunting (tanpa tombol Edit).
  const [handling, setHandling] = useState<{ issue: string; trouble: string; solution: string }>({
    issue: '',
    trouble: '',
    solution: '',
  })
  const [handlingBusy, setHandlingBusy] = useState(false)
  // handlingLoaded mencegah penimpaan draf saat pengguna sedang mengetik.
  const handlingLoaded = useRef(false)
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const [uploading, setUploading] = useState(false)
  // F23: Aktivasi/EWO — modal cancel/hapus ber-alasan.
  const [rfsReason, setRfsReason] = useState<{ mode: 'cancel' | 'delete'; reason: string } | null>(null)
  // F25: data teknis aktivasi RFS (dapat disunting).
  const [activation, setActivation] = useState({
    ip_address: '',
    vlan_detail: '',
    interface_port: '',
    bandwidth_test: '',
    ping_test: '',
    packet_loss: '',
  })
  const [activationBusy, setActivationBusy] = useState(false)
  const activationLoaded = useRef(false)
  // F25: force reassign owner (admin) — alasan wajib.
  const [reassign, setReassign] = useState<{ username: string; reason: string } | null>(null)

  const item = detail.data?.item
  const events = detail.data?.events ?? []
  const comments = detail.data?.comments ?? []
  const workflow = detail.data?.workflow
  const canWrite = !!user && user.role !== 'viewer'
  const isAdmin = user?.role === 'admin'

  // Muat lampiran saat data siap.
  useEffect(() => {
    if (detail.data?.item) {
      void loadAttachments()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [detail.data?.item?.id])

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
    // F25: konfirmasi untuk SEMUA perpindahan status.
    setConfirm({
      title: `Ubah status ke "${next.replace(/_/g, ' ')}"?`,
      body: terminal
        ? 'Status ini bersifat final. Pastikan penanganan sudah selesai.'
        : 'Status work item akan dipindahkan sesuai alur workflow.',
      confirmLabel: 'Ubah Status',
      tone: next === 'cancelled' || next === 'canceled' ? 'danger' : 'primary',
      icon: terminal ? 'warning' : 'swap_horiz',
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

  // ---- F23: aksi Aktivasi/EWO (In Progress, Troubleshoot, Close, Cancel, Delete) ----
  async function rfsInProgress() {
    setBusy(true)
    try {
      await workItemsApi.changeStatus(id, 'in_progress', 'In Progress')
      toast('success', 'Status: In Progress')
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal mengubah status', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  async function rfsCloseTicket() {
    if (!item) return
    setBusy(true)
    try {
      await workItemsApi.update(id, { rfs: { install_stage: 'activated' } })
      try {
        await workItemsApi.changeStatus(id, 'activated', 'Close ticket')
      } catch {
        /* tahap tetap tersimpan walau transisi tidak diizinkan */
      }
      toast('success', 'RFS ditutup (Closed/Completed)', item.ref_no)
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal menutup RFS', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  // ---- F23: submit alasan cancel/hapus Aktivasi/EWO ----
  async function submitRFSReason() {
    if (!rfsReason || !item) return
    const reason = rfsReason.reason.trim()
    if (rfsReason.mode === 'delete' && !reason) {
      toast('warning', 'Alasan wajib', 'Isi alasan penghapusan.')
      return
    }
    setBusy(true)
    try {
      if (rfsReason.mode === 'cancel') {
        await workItemsApi.rfsCancel(id, reason)
        toast('success', 'RFS dibatalkan', item.ref_no)
        setRfsReason(null)
        detail.reload()
      } else {
        await workItemsApi.rfsDelete(id, reason)
        toast('success', 'RFS dihapus', item.ref_no)
        setRfsReason(null)
        onBack()
      }
    } catch (err) {
      toast('error', 'Aksi gagal', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  // ---- F20: assign & penangan tambahan ----
  async function assignOwner(username: string, opts: { reason?: string; force?: boolean } = {}) {
    const target = username.trim()
    if (!target) return
    setBusy(true)
    try {
      await workItemsApi.assign(id, target, opts)
      toast('success', 'Penanggung jawab diperbarui', target)
      setAssignInput('')
      setReassign(null)
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal menetapkan penanggung jawab', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  async function claimTicket() {
    if (!user?.username) return
    await assignOwner(user.username)
  }

  // ---- F25: PIC team (Aktivasi/EWO) ----
  async function savePicTeam(teamID: string) {
    setBusy(true)
    try {
      await workItemsApi.updateRFS(id, { pic_team_id: teamID })
      toast('success', 'PIC team diperbarui')
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal menyimpan PIC team', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  async function addCollaborator(username: string) {
    const target = username.trim()
    if (!target) return
    setBusy(true)
    try {
      await workItemsApi.addCollaborator(id, target)
      toast('success', 'Penangan ditambahkan', target)
      setCollabInput('')
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal menambah penangan', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  async function removeCollaborator(username: string) {
    setBusy(true)
    try {
      await workItemsApi.removeCollaborator(id, username)
      toast('success', 'Penangan dilepas', username)
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal melepas penangan', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  // ---- F20: force unlock (admin) ----
  async function forceUnlockAction() {
    if (!item) return
    if (!unlockReason.trim()) {
      toast('warning', 'Alasan wajib', 'Isi alasan membuka kembali tiket.')
      return
    }
    setBusy(true)
    try {
      await workItemsApi.forceUnlock(id, unlockStatus, unlockReason.trim())
      toast('success', 'Tiket dibuka kembali', `${item.ref_no} → ${unlockStatus}`)
      setUnlockReason('')
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal membuka tiket', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  // ---- F21/F22/F23: catatan penanganan (tiket & Aktivasi/EWO) ----
  // Sinkronkan draf dari item saat pertama kali data tiba (agar tidak menimpa
  // ketikan pengguna pada reload berikutnya).
  const isRFS = detail.data?.item?.item_type === 'rfs'
  useEffect(() => {
    const t = detail.data?.item?.ticket ?? detail.data?.item?.rfs
    if (t && !handlingLoaded.current) {
      setHandling({
        issue: t.issue_found ?? '',
        trouble: t.troubleshooting ?? '',
        solution: t.action_solution ?? '',
      })
      handlingLoaded.current = true
    }
  }, [detail.data?.item?.ticket, detail.data?.item?.rfs])

  const storedHandling = {
    issue: item?.ticket?.issue_found ?? item?.rfs?.issue_found ?? '',
    trouble: item?.ticket?.troubleshooting ?? item?.rfs?.troubleshooting ?? '',
    solution: item?.ticket?.action_solution ?? item?.rfs?.action_solution ?? '',
  }
  const handlingDirty =
    handling.issue !== storedHandling.issue ||
    handling.trouble !== storedHandling.trouble ||
    handling.solution !== storedHandling.solution

  async function saveHandling() {
    if (!handlingDirty) return
    setHandlingBusy(true)
    try {
      const ext = { issue_found: handling.issue, troubleshooting: handling.trouble, action_solution: handling.solution }
      await workItemsApi.update(id, isRFS ? { rfs: ext } : { ticket: ext })
      toast('success', 'Catatan penanganan disimpan')
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal menyimpan catatan', err instanceof Error ? err.message : undefined)
    } finally {
      setHandlingBusy(false)
    }
  }

  // F25: sinkronkan draf data aktivasi (sekali, agar tidak menimpa ketikan).
  //
  // Tunggu sampai detail benar-benar termuat: pada render pertama
  // `detail.data` masih null sehingga data aktivasi belum tersedia. Bila ref
  // ditandai "loaded" saat itu, data yang tersimpan tidak akan pernah muncul.
  useEffect(() => {
    if (!detail.data) return
    if (!activationLoaded.current) {
      const a = detail.data.item?.activation
      if (a) {
        setActivation({
          ip_address: a.ip_address ?? '',
          vlan_detail: a.vlan_detail ?? '',
          interface_port: a.interface_port ?? '',
          bandwidth_test: a.bandwidth_test ?? '',
          ping_test: a.ping_test ?? '',
          packet_loss: a.packet_loss ?? '',
        })
      }
      activationLoaded.current = true
    }
  }, [detail.data])

  async function saveActivation() {
    setActivationBusy(true)
    try {
      await workItemsApi.updateRFS(id, { activation })
      toast('success', 'Data aktivasi disimpan')
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal menyimpan data aktivasi', err instanceof Error ? err.message : undefined)
    } finally {
      setActivationBusy(false)
    }
  }

  // ---- F21: lampiran ----
  async function loadAttachments() {
    try {
      const res = await attachmentsApi.list(id)
      setAttachments(res.attachments ?? [])
    } catch {
      /* lampiran opsional; gagal memuat tidak menghalangi halaman */
    }
  }

  async function uploadAttachment(file: File) {
    setUploading(true)
    try {
      await attachmentsApi.upload(id, file)
      toast('success', 'Lampiran diunggah', file.name)
      await loadAttachments()
      detail.reload()
    } catch (err) {
      toast('error', 'Gagal mengunggah lampiran', err instanceof Error ? err.message : undefined)
    } finally {
      setUploading(false)
    }
  }

  function deleteAttachment(att: Attachment) {
    setConfirm({
      title: 'Hapus lampiran?',
      body: `${att.filename} akan dihapus permanen.`,
      confirmLabel: 'Hapus',
      tone: 'danger',
      run: async () => {
        await attachmentsApi.remove(att.id)
        toast('success', 'Lampiran dihapus', att.filename)
        await loadAttachments()
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
  /** canTransition melaporkan apakah transisi ke status tertentu diizinkan. */
  const canTransition = (target: string) => nextStates.includes(target)

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
            {(() => {
              // F25: tab Aktivasi & Evidence hanya untuk RFS (Aktivasi/EWO).
              const tabs: { id: Tab; label: string; icon: string }[] = [...TABS]
              if (isRFS) {
                tabs.splice(1, 0, { id: 'aktivasi', label: 'Aktivasi', icon: 'settings_ethernet' })
                tabs.splice(2, 0, { id: 'evidence', label: 'Evidence', icon: 'attach_file' })
              }
              return tabs
            })().map((t) => {
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
                    <Field
                      label="Diperbarui oleh"
                      value={
                        item.updated_by_username
                          ? `${item.updated_by_username}${item.updated_at ? ` · ${formatWIB(item.updated_at)}` : ''}`
                          : '—'
                      }
                      mono
                    />
                    {/* F27: waktu selesai + aktor penyelesai. */}
                    <Field label="Waktu Selesai" value={item.completed_at ? formatWIB(item.completed_at) : '—'} mono />
                    <Field label="Diselesaikan oleh" value={item.completed_by || '—'} mono />

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
                        <Field label="Jumlah reopen" value={String(item.ticket.reopen_count)} mono />
                        {item.sla_cycles && item.sla_cycles.length > 0 && (
                          <Field
                            label="Reopen terakhir"
                            value={
                              item.sla_cycles[item.sla_cycles.length - 1].reopened_at
                                ? formatWIB(item.sla_cycles[item.sla_cycles.length - 1].reopened_at)
                                : '—'
                            }
                            mono
                          />
                        )}
                      </>
                    )}

                    {item.task && (
                      <>
                        <Field label="Progres" value={`${item.task.progress_pct}%`} />
                        <Field
                          label="Checklist"
                          value={`${item.task.checklist.filter((c) => c.done).length}/${item.task.checklist.length} selesai`}
                        />
                        {item.task.daily_task_type && (
                          <Field label="Jenis" value={item.task.daily_task_type} />
                        )}
                        {item.task.result_status && (
                          <Field
                            label="Hasil"
                            value={
                              item.task.result_status === 'bermasalah'
                                ? 'Bermasalah (gangguan)'
                                : item.task.result_status === 'normal'
                                  ? 'Normal'
                                  : item.task.result_status
                            }
                          />
                        )}
                        {item.task.completion_note && (
                          <Field label="Keterangan selesai" value={item.task.completion_note} />
                        )}
                      </>
                    )}
                  </dl>

                  {item.tags.length > 0 && (
                    <div className="mt-3 border-t border-border pt-3">
                      <div className="kicker mb-1.5">Tag</div>
                      <TagList tags={item.tags} colors={tagColors} />
                    </div>
                  )}

                  {/* F21/F22: catatan penanganan. F25: hanya tampil pada status
                      pending_troubleshoot; di luar itu cukup field Catatan
                      (Deskripsi/Komentar). */}
                  {item.status === 'pending_troubleshoot' && (item.ticket || item.rfs) && (
                    <div className="mt-4 border-t border-border pt-3">
                      <div className="mb-2 flex items-center justify-between gap-2">
                        <div className="kicker">Catatan Penanganan</div>
                        {canWrite && handlingDirty && (
                          <span className="badge bg-warning-container text-on-warning-container border-outline-variant">
                            belum disimpan
                          </span>
                        )}
                      </div>

                      {canWrite ? (
                        <div className="space-y-3">
                          <HandlingField
                            id="h-issue"
                            label="Issue ditemukan"
                            value={handling.issue}
                            onChange={(v) => setHandling({ ...handling, issue: v })}
                          />
                          <HandlingField
                            id="h-trouble"
                            label="Troubleshooting"
                            value={handling.trouble}
                            onChange={(v) => setHandling({ ...handling, trouble: v })}
                          />
                          <HandlingField
                            id="h-solution"
                            label="Action / Solusi"
                            value={handling.solution}
                            onChange={(v) => setHandling({ ...handling, solution: v })}
                          />
                          <div className="flex items-center justify-end gap-2">
                            <button
                              className="btn-primary"
                              onClick={saveHandling}
                              disabled={handlingBusy || !handlingDirty}
                            >
                              <span className="material-symbols-outlined text-[16px]">save</span>
                              {handlingBusy ? 'Menyimpan…' : 'Simpan Catatan'}
                            </button>
                          </div>
                        </div>
                      ) : (
                        <div className="space-y-2.5">
                          <HandlingRead label="Issue ditemukan" value={storedHandling.issue} />
                          <HandlingRead label="Troubleshooting" value={storedHandling.trouble} />
                          <HandlingRead label="Action / Solusi" value={storedHandling.solution} />
                        </div>
                      )}
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
                    <Field label="Waktu Selesai" value={item.completed_at ? formatWIB(item.completed_at) : '—'} mono />
                    <Field label="Diselesaikan oleh" value={item.completed_by || '—'} mono />
                  </dl>
                </section>
              </div>
            </div>
          )}

          {/* ---------------- Tab: Aktivasi (RFS only) ---------------- */}
          {tab === 'aktivasi' && isRFS && (
            <div className="grid gap-5 lg:grid-cols-3">
              <section className="card card-pad lg:col-span-2">
                <h3 className="mb-1 font-headline text-lg font-semibold">Data Aktivasi</h3>
                <p className="mb-4 text-body-sm text-text-secondary">
                  Data teknis saat aktivasi layanan. Tersimpan terpisah dari ringkasan RFS.
                </p>
                {canWrite ? (
                  <div className="grid gap-3.5 sm:grid-cols-2">
                    <ActivationField
                      id="act-ip"
                      label="IP Address"
                      value={activation.ip_address}
                      onChange={(v) => setActivation({ ...activation, ip_address: v })}
                      placeholder="mis. 10.10.0.2/30"
                    />
                    <ActivationField
                      id="act-vlan"
                      label="VLAN / Detail"
                      value={activation.vlan_detail}
                      onChange={(v) => setActivation({ ...activation, vlan_detail: v })}
                      placeholder="mis. VLAN 120"
                    />
                    <ActivationField
                      id="act-port"
                      label="Interface / Port"
                      value={activation.interface_port}
                      onChange={(v) => setActivation({ ...activation, interface_port: v })}
                      placeholder="mis. Gi0/1"
                    />
                    <ActivationField
                      id="act-bw"
                      label="Hasil Bandwidth Test"
                      value={activation.bandwidth_test}
                      onChange={(v) => setActivation({ ...activation, bandwidth_test: v })}
                      placeholder="mis. 95 Mbps"
                    />
                    <ActivationField
                      id="act-ping"
                      label="Hasil Ping Test"
                      value={activation.ping_test}
                      onChange={(v) => setActivation({ ...activation, ping_test: v })}
                      placeholder="mis. 5ms, 0% loss"
                    />
                    <ActivationField
                      id="act-loss"
                      label="Packet Loss"
                      value={activation.packet_loss}
                      onChange={(v) => setActivation({ ...activation, packet_loss: v })}
                      placeholder="mis. 0%"
                    />
                    <div className="sm:col-span-2 flex items-center justify-between">
                      <span className="text-label-sm text-text-secondary">
                        {item.activation?.updated_by
                          ? `Terakhir diubah oleh ${item.activation.updated_by}`
                          : 'Belum ada data aktivasi.'}
                      </span>
                      <button className="btn-primary" onClick={saveActivation} disabled={activationBusy}>
                        <span className="material-symbols-outlined text-[16px]">save</span>
                        {activationBusy ? 'Menyimpan…' : 'Simpan Data Aktivasi'}
                      </button>
                    </div>
                  </div>
                ) : (
                  <dl className="grid gap-3.5 sm:grid-cols-2">
                    <Field label="IP Address" value={activation.ip_address || '—'} mono />
                    <Field label="VLAN / Detail" value={activation.vlan_detail || '—'} mono />
                    <Field label="Interface / Port" value={activation.interface_port || '—'} mono />
                    <Field label="Hasil Bandwidth Test" value={activation.bandwidth_test || '—'} mono />
                    <Field label="Hasil Ping Test" value={activation.ping_test || '—'} mono />
                    <Field label="Packet Loss" value={activation.packet_loss || '—'} mono />
                  </dl>
                )}
              </section>
              <div className="space-y-5">
                <section className="card card-pad">
                  <h3 className="mb-3 font-headline text-lg font-semibold">PIC</h3>
                  {canWrite ? (
                    <div>
                      <label className="label-field" htmlFor="rfs-pic-team">
                        PIC Team
                      </label>
                      <select
                        id="rfs-pic-team"
                        className="input"
                        value={item.rfs?.pic_team_id ?? ''}
                        onChange={(e) => savePicTeam(e.target.value)}
                        disabled={busy}
                      >
                        <option value="">— Tanpa PIC team —</option>
                        {(teams.data?.teams ?? []).map((t) => (
                          <option key={t.id} value={t.id}>
                            {t.name}
                          </option>
                        ))}
                      </select>
                    </div>
                  ) : (
                    <Field
                      label="PIC Team"
                      value={
                        (teams.data?.teams ?? []).find((t) => t.id === item.rfs?.pic_team_id)?.name ?? '—'
                      }
                    />
                  )}
                  <dl className="mt-3 space-y-2.5 border-t border-border pt-3">
                    <Field label="PIC Sales" value={item.rfs?.pic_sales || '—'} />
                  </dl>
                </section>
              </div>
            </div>
          )}

          {/* ---------------- Tab: Evidence (RFS only) ---------------- */}
          {tab === 'evidence' && isRFS && (
            <section className="card lg:col-span-2">
              <div className="flex items-center justify-between gap-3 border-b border-border px-5 py-3.5">
                <div>
                  <h3 className="font-headline text-lg font-semibold">Evidence</h3>
                  <p className="text-body-sm text-text-secondary">{attachments.length} berkas</p>
                </div>
                {canWrite && (
                  <label className="btn-secondary cursor-pointer">
                    <span className="material-symbols-outlined text-[18px]">upload</span>
                    {uploading ? 'Mengunggah…' : 'Unggah'}
                    <input
                      type="file"
                      className="hidden"
                      disabled={uploading}
                      onChange={(e) => {
                        const f = e.target.files?.[0]
                        if (f) void uploadAttachment(f)
                        e.target.value = ''
                      }}
                    />
                  </label>
                )}
              </div>
              <div className="p-5">
                {attachments.length === 0 ? (
                  <p className="text-body-sm text-text-secondary">
                    Belum ada berkas evidence. Format diizinkan: gambar, PDF, teks/CSV, zip (maks 50 MB).
                  </p>
                ) : (
                  <ul className="space-y-2">
                    {attachments.map((att) => (
                      <li
                        key={att.id}
                        className="flex items-center justify-between gap-3 rounded-control border border-border bg-surface-container-lowest px-3 py-2"
                      >
                        <a
                          href={attachmentsApi.downloadUrl(att.id)}
                          className="flex min-w-0 items-center gap-2 text-body-sm hover:text-primary"
                        >
                          <span className="material-symbols-outlined text-[18px]">description</span>
                          <span className="truncate">{att.filename}</span>
                        </a>
                        {canWrite && (
                          <button
                            className="text-text-secondary hover:text-critical"
                            title="Hapus"
                            onClick={() => deleteAttachment(att)}
                            disabled={busy}
                          >
                            <span className="material-symbols-outlined text-[18px]">delete</span>
                          </button>
                        )}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </section>
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

              {/* F21: Lampiran pendukung — F25: dipindah ke tab Evidence untuk RFS. */}
              {!isRFS && (
              <section className="card lg:col-span-2">
                <div className="flex items-center justify-between gap-3 border-b border-border px-5 py-3.5">
                  <div>
                    <h3 className="font-headline text-lg font-semibold">Lampiran Pendukung</h3>
                    <p className="text-body-sm text-text-secondary">{attachments.length} berkas</p>
                  </div>
                  {canWrite && (
                    <label className="btn-secondary cursor-pointer">
                      <span className="material-symbols-outlined text-[18px]">
                        {uploading ? 'progress_activity' : 'upload'}
                      </span>
                      {uploading ? 'Mengunggah…' : 'Unggah'}
                      <input
                        type="file"
                        className="hidden"
                        disabled={uploading}
                        onChange={(e) => {
                          const f = e.target.files?.[0]
                          if (f) void uploadAttachment(f)
                          e.target.value = ''
                        }}
                      />
                    </label>
                  )}
                </div>
                {attachments.length === 0 ? (
                  <p className="px-5 py-6 text-center text-body-sm text-text-secondary">
                    Belum ada lampiran. Format diizinkan: gambar, PDF, teks/CSV, zip (maks 50 MB).
                  </p>
                ) : (
                  <ul className="divide-y divide-border">
                    {attachments.map((att) => (
                      <li key={att.id} className="flex items-center gap-3 px-5 py-3">
                        <span className="material-symbols-outlined text-[22px] text-text-secondary">
                          {attIcon(att.mime)}
                        </span>
                        <div className="min-w-0 flex-1">
                          <a
                            href={attachmentsApi.downloadUrl(att.id)}
                            className="block truncate font-medium hover:underline"
                            title={att.filename}
                          >
                            {att.filename}
                          </a>
                          <span className="text-label-sm text-text-secondary">
                            {formatBytes(att.size_bytes)} · {att.uploaded_by} · {formatWIB(att.created_at)}
                          </span>
                        </div>
                        <a
                          className="btn-ghost h-8 w-8 px-0"
                          href={attachmentsApi.downloadUrl(att.id)}
                          title="Unduh"
                        >
                          <span className="material-symbols-outlined text-[18px]">download</span>
                        </a>
                        {(isAdmin || canManage || att.uploaded_by === user?.username) && (
                          <button
                            className="btn-ghost h-8 w-8 px-0 text-critical"
                            title="Hapus"
                            onClick={() => deleteAttachment(att)}
                          >
                            <span className="material-symbols-outlined text-[18px]">delete</span>
                          </button>
                        )}
                      </li>
                    ))}
                  </ul>
                )}
              </section>
              )}

              <section className="card">
                <div className="border-b border-border px-5 py-3.5">
                  <h3 className="font-headline text-lg font-semibold">Timeline</h3>
                  <p className="text-body-sm text-text-secondary">{events.length} kejadian</p>
                </div>
                {events.length === 0 ? (
                  <EmptyState icon="history" title="Belum ada aktivitas" />
                ) : (
                  <ol className="relative space-y-3 px-5 py-4">
                    {events.map((e) => {
                      // F27: tandai event penyelesaian (status akhir selesai).
                      const done = ['done', 'closed', 'activated', 'completed', 'fulfilled', 'resolved'].includes(
                        (e.to_value || '').toLowerCase(),
                      )
                      return (
                      <li key={e.id} className="flex gap-3">
                        <span
                          className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${
                            done
                              ? 'bg-success'
                              : e.event_type.includes('fail') || e.event_type.includes('breach')
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
                          <div className="flex flex-wrap items-center gap-1.5 text-body-sm">
                            <span className="mono text-label-md">{e.event_type.replace(/_/g, ' ')}</span>
                            {e.from_value && e.to_value && (
                              <span className="text-text-secondary">
                                <span className="mono">{e.from_value}</span> → <span className="mono">{e.to_value}</span>
                              </span>
                            )}
                            {!e.from_value && e.to_value && (
                              <span className="mono text-text-secondary">{e.to_value}</span>
                            )}
                            {done && (
                              <span className="inline-flex items-center gap-0.5 rounded-full bg-success-container px-1.5 py-0.5 text-[10px] font-bold uppercase text-on-success-container">
                                <span className="material-symbols-outlined text-[12px]">task_alt</span>
                                Selesai
                              </span>
                            )}
                          </div>
                          <div className="mono text-label-sm text-text-secondary">
                            {formatWIB(e.created_at, true)} · {e.actor_username || 'sistem'}
                          </div>
                        </div>
                      </li>
                      )
                    })}
                  </ol>
                )}
              </section>
            </div>
          )}

          {/* ---------------- Tab: Action ---------------- */}
          {tab === 'action' && canWrite && (
            <div className="grid gap-5 lg:grid-cols-3">
              <div className="space-y-5 lg:col-span-2">
                {/* F20: Penanggung jawab & penangan */}
                {(isTicket || isRFS) && (
                  <section className="card card-pad">
                    <h3 className="mb-3 font-headline text-lg font-semibold">Penanggung Jawab</h3>

                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-body-sm text-text-secondary">Owner:</span>
                      {item.owner_username ? (
                        <span className="mono rounded-full bg-surface-container px-2.5 py-1 text-label-md font-semibold">
                          {item.owner_username}
                        </span>
                      ) : (
                        <span className="text-body-sm text-text-secondary">belum ditetapkan</span>
                      )}
                      {/* F25: "Ikuti Tiket" (self-claim) untuk RFS/incident/request,
                          hanya bila owner masih kosong. */}
                      {canWrite &&
                        !item.owner_username &&
                        user?.username && (
                          <button className="btn-secondary h-8 px-3 text-label-sm" onClick={claimTicket} disabled={busy}>
                            <span className="material-symbols-outlined text-[16px]">front_hand</span>
                            Ikuti Tiket
                          </button>
                        )}
                    </div>

                    {/* F25: owner lock — owner sudah terisi, hanya admin yang bisa ganti. */}
                    {item.owner_username && !isAdmin && (
                      <p className="mt-2 text-label-sm text-text-secondary">
                        Penanggung jawab sudah terisi. Hubungi admin untuk menggantinya.
                      </p>
                    )}

                    {isAdmin && (
                      <div className="mt-3 flex flex-wrap items-end gap-2 border-t border-border pt-3">
                        <div className="min-w-[180px] flex-1">
                          <label className="label-field" htmlFor="assign-owner">
                            {item.owner_username ? 'Force reassign owner (username)' : 'Tetapkan owner (username)'}
                          </label>
                          <input
                            id="assign-owner"
                            className="input mono"
                            value={assignInput}
                            onChange={(e) => setAssignInput(e.target.value)}
                            placeholder="mis. epul"
                          />
                        </div>
                        <button
                          className="btn-secondary"
                          disabled={busy || !assignInput.trim()}
                          onClick={() => {
                            // Owner sudah terisi → wajib alasan (force reassign).
                            if (item.owner_username && assignInput.trim().toLowerCase() !== item.owner_username.toLowerCase()) {
                              setReassign({ username: assignInput.trim(), reason: '' })
                            } else {
                              void assignOwner(assignInput)
                            }
                          }}
                        >
                          {item.owner_username ? 'Force Reassign' : 'Tetapkan'}
                        </button>
                      </div>
                    )}

                    {/* Penangan tambahan */}
                    <div className="mt-4 border-t border-border pt-3">
                      <div className="kicker mb-2">Ikut Menangani ({item.collaborators?.length ?? 0})</div>
                      <div className="mb-2 flex flex-wrap gap-1.5">
                        {(item.collaborators ?? []).map((c) => (
                          <span
                            key={c.id}
                            className="inline-flex items-center gap-1 rounded-full bg-surface-container px-2 py-0.5 text-label-sm"
                          >
                            {c.username}
                            {(isAdmin || c.username.toLowerCase() === user?.username?.toLowerCase()) && (
                              <button
                                className="text-text-secondary hover:text-critical"
                                title="Lepas"
                                onClick={() => removeCollaborator(c.username)}
                                disabled={busy}
                              >
                                <span className="material-symbols-outlined text-[14px]">close</span>
                              </button>
                            )}
                          </span>
                        ))}
                        {(item.collaborators ?? []).length === 0 && (
                          <span className="text-body-sm text-text-secondary">Belum ada penangan tambahan.</span>
                        )}
                      </div>
                      {user?.username && !(item.collaborators ?? []).some((c) => c.username.toLowerCase() === user.username.toLowerCase()) && (
                        <button
                          className="btn-secondary h-8 px-3 text-label-sm"
                          onClick={() => addCollaborator(user.username)}
                          disabled={busy}
                        >
                          <span className="material-symbols-outlined text-[16px]">group_add</span>
                          Ikut menangani
                        </button>
                      )}
                      {canManage && (
                        <div className="mt-2 flex flex-wrap items-end gap-2">
                          <input
                            className="input mono h-9 flex-1 py-0"
                            value={collabInput}
                            onChange={(e) => setCollabInput(e.target.value)}
                            placeholder="tambah penangan (username)"
                          />
                          <button
                            className="btn-secondary h-9"
                            disabled={busy || !collabInput.trim()}
                            onClick={() => addCollaborator(collabInput)}
                          >
                            Tambah
                          </button>
                        </div>
                      )}
                    </div>
                  </section>
                )}

                {/* Rubah Status — F25: disembunyikan untuk RFS (pakai tombol aksi khusus). */}
                {!isRFS && (
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
                      {canTransition('waiting_customer') && (
                        <div className="mt-3">
                          <button
                            className="btn-secondary"
                            disabled={busy}
                            onClick={() => changeStatus('waiting_customer')}
                            title="Tandai sedang menunggu konfirmasi pelanggan"
                          >
                            <span className="material-symbols-outlined text-[18px]">hourglass_top</span>
                            Menunggu Konfirmasi Pelanggan
                          </button>
                        </div>
                      )}
                      <p className="mt-3 text-label-sm text-text-secondary">
                        Status saat ini <span className="mono">{item.status}</span> · alur:{' '}
                        <span className="mono">{workflow.states.join(' → ')}</span>
                      </p>
                    </>
                  ) : (
                    <p className="text-body-sm text-text-secondary">Workflow untuk tipe ini tidak tersedia.</p>
                  )}
                </section>
                )}

                {/* F21: tombol cepat buka/tutup tiket */}
                {isTicket && workflow && !item.sla_cycles?.some(() => false) && (
                  <section className="card card-pad">
                    <h3 className="mb-3 font-headline text-lg font-semibold">Buka / Tutup Tiket</h3>
                    <div className="flex flex-wrap gap-2">
                      <button
                        className="btn-brand"
                        disabled={busy || !canTransition('closed')}
                        onClick={() => changeStatus('closed')}
                        title={canTransition('closed') ? 'Tutup tiket' : 'Transisi tidak tersedia dari status ini'}
                      >
                        <span className="material-symbols-outlined text-[18px]">lock</span>
                        Tutup Tiket
                      </button>
                      <button
                        className="btn-secondary"
                        disabled={busy || !canTransition('in_progress')}
                        onClick={() => changeStatus('in_progress')}
                        title={canTransition('in_progress') ? 'Buka kembali' : 'Transisi tidak tersedia dari status ini'}
                      >
                        <span className="material-symbols-outlined text-[18px]">lock_open</span>
                        Buka Kembali
                      </button>
                    </div>
                    <p className="mt-2 text-label-sm text-text-secondary">
                      Mengikuti alur workflow; tombol nonaktif bila transisi tidak sah.
                    </p>
                  </section>
                )}

                {/* Aksi lain */}
                <section className="card card-pad">
                  <h3 className="mb-3 font-headline text-lg font-semibold">Aksi Lain</h3>
                  <div className="flex flex-wrap gap-2">
                    {item.item_type === 'rfs' && item.status !== 'activated' && item.status !== 'cancelled' && (
                      <>
                        <button className="btn-secondary" onClick={rfsInProgress} disabled={busy}>
                          <span className="material-symbols-outlined text-[18px]">play_arrow</span>
                          In Progress
                        </button>
                        <button className="btn-secondary" onClick={() => changeStatus('pending_troubleshoot')} disabled={busy || !canTransition('pending_troubleshoot')}>
                          <span className="material-symbols-outlined text-[18px]">handyman</span>
                          Troubleshoot
                        </button>
                        <button className="btn-brand" onClick={rfsCloseTicket} disabled={busy}>
                          <span className="material-symbols-outlined text-[18px]">task_alt</span>
                          Close Ticket
                        </button>
                        <button
                          className="btn-secondary"
                          onClick={() => changeStatus('postponed')}
                          disabled={busy || !canTransition('postponed')}
                          title={canTransition('postponed') ? 'Tunda (postpone)' : 'Transisi tidak tersedia dari status ini'}
                        >
                          <span className="material-symbols-outlined text-[18px]">schedule</span>
                          Postpone
                        </button>
                        <button className="btn-secondary text-warning" onClick={() => setRfsReason({ mode: 'cancel', reason: '' })} disabled={busy}>
                          <span className="material-symbols-outlined text-[18px]">cancel</span>
                          Cancel
                        </button>
                      </>
                    )}
                    <button className="btn-secondary" onClick={sendNotification} disabled={busy} title="Kirim notifikasi sekarang">
                      <span className="material-symbols-outlined text-[18px]">send</span>
                      Kirim Notifikasi
                    </button>
                    {canManage && item.item_type === 'rfs' && (
                      <button className="btn-danger" onClick={() => setRfsReason({ mode: 'delete', reason: '' })} disabled={busy} title="Hapus RFS (alasan wajib)">
                        <span className="material-symbols-outlined text-[18px]">delete</span>
                        Hapus
                      </button>
                    )}
                    {canManage && item.item_type !== 'rfs' && (
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

                {/* F20: Force Unlock — admin, hanya untuk tiket yang sudah ditutup */}
                {isAdmin && workflow && isTicket && isTerminalStatus(item.status) && (
                  <section className="card card-pad border-critical/40">
                    <div className="mb-3 flex items-center gap-2">
                      <span className="material-symbols-outlined text-[20px] text-critical">lock_open</span>
                      <h3 className="font-headline text-lg font-semibold">Force Unlock (Admin)</h3>
                    </div>
                    <p className="mb-3 text-body-sm text-text-secondary">
                      Membuka kembali tiket yang sudah <span className="mono">{item.status}</span> tanpa aturan transisi.
                      Siklus SLA baru dibuka dan alasan wajib dicatat pada audit.
                    </p>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div>
                        <label className="label-field" htmlFor="unlock-status">
                          Status tujuan
                        </label>
                        <select
                          id="unlock-status"
                          className="input"
                          value={unlockStatus}
                          onChange={(e) => setUnlockStatus(e.target.value)}
                        >
                          {workflow.states
                            .filter((s) => s !== 'closed')
                            .map((s) => (
                              <option key={s} value={s}>
                                {s.replace(/_/g, ' ')}
                              </option>
                            ))}
                        </select>
                      </div>
                      <div>
                        <label className="label-field" htmlFor="unlock-reason">
                          Alasan (wajib)
                        </label>
                        <input
                          id="unlock-reason"
                          className="input"
                          value={unlockReason}
                          onChange={(e) => setUnlockReason(e.target.value)}
                          placeholder="mis. pelanggan melaporkan masalah berulang"
                        />
                      </div>
                    </div>
                    <div className="mt-3 flex justify-end">
                      <button
                        className="btn-danger"
                        disabled={busy || !unlockReason.trim()}
                        onClick={forceUnlockAction}
                      >
                        <span className="material-symbols-outlined text-[18px]">lock_open</span>
                        Buka Paksa
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

      {/* F23: modal alasan cancel/hapus Aktivasi/EWO */}
      {rfsReason && (
        <Modal
          title={rfsReason.mode === 'delete' ? `Hapus RFS — ${item?.ref_no ?? ''}` : `Batalkan RFS — ${item?.ref_no ?? ''}`}
          width="sm"
          onClose={() => setRfsReason(null)}
          footer={
            <>
              <button className="btn-secondary" onClick={() => setRfsReason(null)} disabled={busy}>
                Batal
              </button>
              <button className="btn-danger" onClick={submitRFSReason} disabled={busy}>
                {busy ? 'Memproses…' : rfsReason.mode === 'delete' ? 'Hapus RFS' : 'Batalkan RFS'}
              </button>
            </>
          }
        >
          <div>
            <label className="label-field" htmlFor="rfs-reason">
              {rfsReason.mode === 'delete' ? 'Alasan penghapusan (wajib)' : 'Alasan pembatalan (opsional)'}
            </label>
            <textarea
              id="rfs-reason"
              className="input h-24 py-2"
              autoFocus
              value={rfsReason.reason}
              onChange={(e) => setRfsReason({ ...rfsReason, reason: e.target.value })}
              placeholder={rfsReason.mode === 'delete' ? 'Tulis alasan penghapusan…' : 'Opsional'}
            />
          </div>
        </Modal>
      )}

      {/* F25: force reassign owner (admin) — alasan wajib. */}
      {reassign && (
        <Modal
          title={`Force Reassign — ${item?.ref_no ?? ''}`}
          width="sm"
          onClose={() => setReassign(null)}
          footer={
            <>
              <button className="btn-secondary" onClick={() => setReassign(null)} disabled={busy}>
                Batal
              </button>
              <button
                className="btn-danger"
                disabled={busy || !reassign.reason.trim()}
                onClick={() => assignOwner(reassign.username, { reason: reassign.reason.trim(), force: true })}
              >
                {busy ? 'Memproses…' : 'Ganti Penanggung Jawab'}
              </button>
            </>
          }
        >
          <div className="space-y-3.5">
            <p className="text-body-sm text-text-secondary">
              Mengganti penanggung jawab dari <span className="mono">{item?.owner_username}</span> ke{' '}
              <span className="mono">{reassign.username}</span>. Alasan wajib diisi dan tercatat pada audit.
            </p>
            <div>
              <label className="label-field" htmlFor="reassign-reason">
                Alasan penggantian (wajib)
              </label>
              <textarea
                id="reassign-reason"
                className="input h-24 py-2"
                autoFocus
                value={reassign.reason}
                onChange={(e) => setReassign({ ...reassign, reason: e.target.value })}
                placeholder="mis. penanggung jawab sebelumnya mutasi shift"
              />
            </div>
          </div>
        </Modal>
      )}
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

/** HandlingField adalah textarea bernomor untuk catatan penanganan. */
function HandlingField({
  id,
  label,
  value,
  onChange,
}: {
  id: string
  label: string
  value: string
  onChange: (v: string) => void
}) {
  return (
    <div>
      <label className="label-field" htmlFor={id}>
        {label}
      </label>
      <textarea id={id} className="input h-20 py-2" value={value} onChange={(e) => onChange(e.target.value)} />
    </div>
  )
}

/** HandlingRead menampilkan catatan penanganan (mode baca). */
function HandlingRead({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="kicker mb-0.5">{label}</div>
      <p className="whitespace-pre-wrap text-body-sm text-text-primary">{value || <span className="text-text-secondary">—</span>}</p>
    </div>
  )
}

/** ActivationField (F25) — satu field teks data aktivasi RFS. */
function ActivationField({
  id,
  label,
  value,
  onChange,
  placeholder,
}: {
  id: string
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
}) {
  return (
    <div>
      <label className="label-field" htmlFor={id}>
        {label}
      </label>
      <input
        id={id}
        className="input mono"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
      />
    </div>
  )
}

/** formatBytes mengubah ukuran berkas menjadi teks ringkas. */
function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

/** attIcon memilih ikon Material sesuai jenis lampiran. */
function attIcon(mime: string): string {
  if (mime.startsWith('image/')) return 'image'
  if (mime === 'application/pdf') return 'picture_as_pdf'
  if (mime === 'application/zip' || mime.includes('gzip')) return 'folder_zip'
  if (mime.startsWith('text/')) return 'description'
  return 'attach_file'
}

/** isTerminalStatus melaporkan status yang mengakhiri tiket. */
function isTerminalStatus(status: string): boolean {
  return ['closed', 'completed', 'fulfilled', 'done', 'cancelled', 'canceled'].includes(status)
}
