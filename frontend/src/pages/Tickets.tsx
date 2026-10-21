import { useEffect, useMemo, useState } from 'react'
import { ApiUser, masterDataApi, notificationApi, WorkItem, workItemsApi } from '../api'
import {
  ActionMenu,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  LoadingBlock,
  Modal,
  PageHeader,
  PriorityBadge,
  StatusBadge,
  TagList,
} from '../components/ui'
import { useAsync, useDebounced, useTagColors } from '../hooks'
import { slaTone, formatDuration } from '../lib/format'
import { statusTone } from '../design/tokens'
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

/** Tipe tiket yang dikelola halaman ini. */
const TICKET_TYPES = [
  { value: 'incident', label: 'Insiden' },
  { value: 'request', label: 'Permintaan' },
  { value: 'change', label: 'Change' },
] as const

/** Status tiket (sinkron dengan workitems.DefaultWorkflows untuk tiap tipe). */
const TICKET_STATUSES: Record<string, string[]> = {
  incident: ['new', 'assigned', 'in_progress', 'pending_customer', 'resolved', 'closed'],
  request: ['new', 'assigned', 'in_progress', 'pending_customer', 'fulfilled', 'closed'],
  change: ['draft', 'review', 'approved', 'scheduled', 'implementing', 'completed', 'rolled_back', 'cancelled'],
}

const PRIORITIES = ['low', 'normal', 'high', 'critical']
const LEVELS = ['low', 'medium', 'high']

/** Filter status per kartu statistik, disesuaikan dengan alur tiap tipe. */
function statusByStat(itemType: string) {
  if (itemType === 'change') {
    const open = 'draft,review,approved,scheduled,implementing'
    return {
      todo: { statusIn: 'draft,review,approved,scheduled', slaState: '' },
      doing: { statusIn: 'implementing', slaState: '' },
      late: { statusIn: open, slaState: 'breached' },
      done: { statusIn: 'completed,rolled_back', slaState: '' },
    }
  }
  if (itemType === 'request') {
    return {
      todo: { statusIn: 'new,assigned', slaState: '' },
      doing: { statusIn: 'in_progress,pending_customer', slaState: '' },
      late: { statusIn: 'new,assigned,in_progress,pending_customer', slaState: 'breached' },
      done: { statusIn: 'fulfilled,closed', slaState: '' },
    }
  }
  return {
    todo: { statusIn: 'new,assigned', slaState: '' },
    doing: { statusIn: 'in_progress,pending_customer', slaState: '' },
    late: { statusIn: 'new,assigned,in_progress,pending_customer', slaState: 'breached' },
    done: { statusIn: 'resolved,closed', slaState: '' },
  }
}

/**
 * Tickets (F10) — item_type = incident / request / change.
 *
 * - Kategori & subkategori diambil dari Master Data (hierarki parent_id).
 * - Jenis gangguan dari Master Data `incident_type`.
 * - Dampak/urgensi berupa dropdown low/medium/high.
 * - Tidak ada tenggat waktu; SLA dihitung dari penanganan pertama s.d. closed.
 */
export default function Tickets({
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
  const [itemType, setItemType] = useState('incident')
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [statusIn, setStatusIn] = useState('')
  const [slaState, setSlaState] = useState('')
  const [stats, setStats] = useState({ todo: 0, doing: 0, late: 0, done: 0 })
  const [priority, setPriority] = useState('')
  const [creator, setCreator] = useState('')
  const [offset, setOffset] = useState(0)
  const [showCreate, setShowCreate] = useState(false)
  const [editing, setEditing] = useState<WorkItem | null>(null)
  const [detailID, setDetailID] = useState<string | null>(null)
  const [notifyingID, setNotifyingID] = useState<string | null>(null)
  const [joiningID, setJoiningID] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)

  const tagColors = useTagColors()

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
        type: itemType,
        ...(status ? { status } : {}),
        ...(statusIn ? { status_in: statusIn } : {}),
        ...(slaState ? { sla_state: slaState } : {}),
        ...(priority ? { priority } : {}),
        ...(creator ? { owner: creator } : {}),
        ...(debouncedSearch ? { q: debouncedSearch } : {}),
        limit: String(limit),
        offset: String(offset),
      }),
    [itemType, status, statusIn, slaState, priority, creator, debouncedSearch, offset],
  )

  const statFilters = statusByStat(itemType)

  // Total ringkas lintas halaman untuk kartu statistik.
  useEffect(() => {
    let cancelled = false
    const count = (filters: Parameters<typeof workItemsApi.list>[0]) =>
      workItemsApi.list({ ...filters, type: itemType, limit: '1' }).then((res) => res.total ?? 0)
    const f = statusByStat(itemType)
    Promise.all([
      count({ status_in: f.todo.statusIn }).catch(() => 0),
      count({ status_in: f.doing.statusIn }).catch(() => 0),
      count({ status_in: f.late.statusIn, sla_state: f.late.slaState }).catch(() => 0),
      count({ status_in: f.done.statusIn }).catch(() => 0),
    ]).then(([todo, doing, late, done]) => {
      if (!cancelled) setStats({ todo, doing, late, done })
    })
    return () => {
      cancelled = true
    }
  }, [itemType, items.data])

  function resetStatFilters() {
    setStatusIn('')
    setSlaState('')
    setStatus('')
    setOffset(0)
  }

  function filterByStat(kind: 'todo' | 'doing' | 'late' | 'done') {
    if (statusIn === statFilters[kind].statusIn && slaState === statFilters[kind].slaState) {
      resetStatFilters()
      return
    }
    setStatusIn(statFilters[kind].statusIn)
    setSlaState(statFilters[kind].slaState)
    setStatus('')
    setOffset(0)
  }

  const rows = items.data?.items ?? []
  const total = items.data?.total ?? 0
  const canWrite = !!user && user.role !== 'viewer'
  const isAdmin = user?.role === 'admin'

  function canManage(item: WorkItem): boolean {
    if (isAdmin) return true
    const u = (user?.username ?? '').toLowerCase()
    if (!u) return false
    return item.created_by?.toLowerCase() === u || item.owner_username?.toLowerCase() === u
  }

  const page = Math.floor(offset / limit) + 1
  const maxPage = Math.max(1, Math.ceil(total / limit))

  function handleDelete(item: WorkItem) {
    setConfirm({
      title: 'Hapus tiket?',
      body: `Tiket ${item.ref_no} — "${item.title}" akan dihapus. Tindakan ini tidak dapat dibatalkan.`,
      confirmLabel: 'Hapus',
      tone: 'danger',
      run: async () => {
        await workItemsApi.remove(item.id)
        toast('success', 'Tiket dihapus', item.ref_no)
        items.reload()
      },
    })
  }

  async function handleNotify(item: WorkItem) {
    setNotifyingID(item.id)
    try {
      const res = await workItemsApi.triggerNotification(item.id, { offset_label: 'MANUAL' })
      if (res.added > 0) toast('success', 'Notifikasi dikirim', res.message)
      else toast('warning', 'Tidak ada pesan baru', 'Binding aktif mungkin sudah menerima pesan ini.')
      items.reload()
    } catch (err) {
      toast('error', 'Gagal mengirim notifikasi', err instanceof Error ? err.message : undefined)
    } finally {
      setNotifyingID(null)
    }
  }

  async function handleJoinTicket(item: WorkItem) {
    if (!user?.username) return
    setJoiningID(item.id)
    try {
      if (!item.owner_username) {
        await workItemsApi.assign(item.id, user.username)
        toast('success', 'Tiket mulai dikerjakan', `${item.ref_no} ditetapkan kepada ${user.username}`)
      } else {
        const result = await workItemsApi.addCollaborator(item.id, user.username)
        toast(
          result.inserted ? 'success' : 'info',
          result.inserted ? 'Berhasil mengikuti tiket' : 'Sudah mengikuti tiket',
          item.ref_no,
        )
      }
      items.reload()
    } catch (err) {
      toast('error', 'Gagal mengikuti tiket', err instanceof Error ? err.message : undefined)
    } finally {
      setJoiningID(null)
    }
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
        title="Ticketing NOC"
        description="Insiden, permintaan layanan, dan change request. Status diubah dari halaman detail (tab Action)."
        actions={
          <>
            <button className="btn-secondary" onClick={items.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            {canWrite && (
              <button className="btn-primary" onClick={() => setShowCreate(true)}>
                <span className="material-symbols-outlined text-[18px]">add</span>
                Tiket Baru
              </button>
            )}
          </>
        }
      />

      <div className="mb-4 flex flex-wrap gap-2">
        {TICKET_TYPES.map((t) => (
          <button
            key={t.value}
            onClick={() => {
              setItemType(t.value)
              setStatus('')
              setStatusIn('')
              setSlaState('')
              setOffset(0)
            }}
            className={t.value === itemType ? 'btn-primary' : 'btn-secondary'}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* Statistik tiket — klik kartu untuk memfilter daftar. */}
      <div className="mb-5 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatFilterCard
          label="Belum dikerjakan"
          value={stats.todo}
          tone="bg-surface-container text-text-secondary border-outline-variant"
          active={statusIn === statFilters.todo.statusIn && !slaState}
          onClick={() => filterByStat('todo')}
        />
        <StatFilterCard
          label="Sedang dikerjakan"
          value={stats.doing}
          tone="bg-info-container text-on-info-container border-outline-variant"
          active={statusIn === statFilters.doing.statusIn && !slaState}
          onClick={() => filterByStat('doing')}
        />
        <StatFilterCard
          label="Terlambat"
          value={stats.late}
          tone={stats.late > 0 ? 'bg-critical-container text-on-critical-container border-critical' : 'bg-surface-container text-text-secondary border-outline-variant'}
          active={statusIn === statFilters.late.statusIn && slaState === 'breached'}
          onClick={() => filterByStat('late')}
        />
        <StatFilterCard
          label="Sudah selesai"
          value={stats.done}
          tone="bg-success-container text-on-success-container border-outline-variant"
          active={statusIn === statFilters.done.statusIn && !slaState}
          onClick={() => filterByStat('done')}
        />
      </div>

      <section className="card mb-5 p-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <label className="label-field" htmlFor="tk-search">
              Cari
            </label>
            <input
              id="tk-search"
              className="input"
              placeholder="judul, ref, device…"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value)
                setOffset(0)
              }}
            />
          </div>
          <div>
            <label className="label-field" htmlFor="tk-status">
              Status
            </label>
            <select
              id="tk-status"
              className="input"
              value={status}
              onChange={(e) => {
                setStatus(e.target.value)
                setStatusIn('')
                setSlaState('')
                setOffset(0)
              }}
            >
              <option value="">Semua</option>
              {(TICKET_STATUSES[itemType] ?? []).map((s) => (
                <option key={s} value={s}>
                  {s.replace(/_/g, ' ')}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="tk-priority">
              Prioritas
            </label>
            <select
              id="tk-priority"
              className="input"
              value={priority}
              onChange={(e) => {
                setPriority(e.target.value)
                setOffset(0)
              }}
            >
              <option value="">Semua</option>
              {PRIORITIES.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="tk-creator">
              Dibuat oleh
            </label>
            <input
              id="tk-creator"
              className="input mono"
              placeholder="username"
              value={creator}
              onChange={(e) => {
                setCreator(e.target.value)
                setOffset(0)
              }}
            />
          </div>
        </div>
      </section>

      <section className="card">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3.5">
          <div>
            <h3 className="font-headline text-lg font-semibold">Daftar Tiket</h3>
            <p className="text-body-sm text-text-secondary">
              {total} item · halaman {page} dari {maxPage}
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
            icon="confirmation_number"
            title="Belum ada tiket"
            body={
              debouncedSearch || status || priority || creator
                ? 'Tidak ada tiket yang cocok dengan filter.'
                : 'Buat tiket pertama untuk tipe ini.'
            }
            action={
              canWrite && !debouncedSearch && !status ? (
                <button className="btn-primary" onClick={() => setShowCreate(true)}>
                  <span className="material-symbols-outlined text-[18px]">add</span>
                  Buat Tiket
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
                  <th>Kategori</th>
                  <th>Prioritas</th>
                  <th>Status</th>
                  <th>Tag</th>
                  <th>SLA</th>
                  <th className="w-px whitespace-nowrap text-right">Aksi</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((it) => (
                  <tr key={it.id}>
                    <td>
                      <button className="mono text-label-md text-primary hover:underline" onClick={() => setDetailID(it.id)}>
                        {it.ref_no}
                      </button>
                      {(it.reopen_count ?? 0) > 0 && (
                        <span
                          className="ml-1.5 rounded-full bg-warning-container px-1.5 py-0.5 text-[10px] font-bold text-on-warning-container"
                          title={`Dibuka kembali ${it.reopen_count}×`}
                        >
                          reopen {it.reopen_count}
                        </span>
                      )}
                    </td>
                    <td className="max-w-[280px]">
                      <button className="block truncate text-left font-medium hover:underline" onClick={() => setDetailID(it.id)}>
                        {it.title}
                      </button>
                    </td>
                    <td className="max-w-[160px]">
                      <div className="truncate text-body-sm">{it.ticket?.category || '—'}</div>
                      {it.ticket?.subcategory && (
                        <span className="truncate text-label-sm text-text-secondary">{it.ticket.subcategory}</span>
                      )}
                    </td>
                    <td>
                      <PriorityBadge priority={it.priority} />
                    </td>
                    <td>
                      <StatusBadge status={it.status} tone={statusTone[it.status]} />
                    </td>
                    <td className="max-w-[140px]">
                      <TagList tags={it.tags} colors={tagColors} />
                    </td>
                    <td className={`mono whitespace-nowrap text-label-md ${it.sla_start_at ? slaTone(it.sla_seconds) : 'text-text-secondary'}`}>
                      {it.sla_start_at ? (
                        <>
                          {formatDuration(it.sla_seconds)}
                          {it.sla_running && <span className="ml-1 text-label-sm font-normal">•</span>}
                        </>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td className="w-px whitespace-nowrap">
                      <div className="flex justify-end gap-2">
                        {canWrite && (
                          <button
                            className="btn-primary h-8 px-2 text-label-sm"
                            disabled={joiningID === it.id || it.status === 'closed' || it.status === 'completed' || it.status === 'cancelled'}
                            onClick={() => handleJoinTicket(it)}
                            title={it.owner_username ? 'Ikuti tiket sebagai penangan tambahan' : 'Ambil dan kerjakan tiket'}
                          >
                            <span className={`material-symbols-outlined text-[16px] ${joiningID === it.id ? 'animate-spin' : ''}`}>
                              {joiningID === it.id ? 'progress_activity' : it.owner_username ? 'group_add' : 'play_arrow'}
                            </span>
                            {it.owner_username ? 'Ikuti Tiket' : 'Kerjakan'}
                          </button>
                        )}
                        <ActionMenu
                          label="Penanganan"
                          items={[
                            { label: 'Lihat detail', icon: 'open_in_new', onClick: () => setDetailID(it.id) },
                            ...(canWrite
                              ? [{
                                  label: 'Kirim notifikasi',
                                  icon: notifyingID === it.id ? 'progress_activity' : 'send',
                                  disabled: notifyingID === it.id,
                                  onClick: () => handleNotify(it),
                                }]
                              : []),
                            ...(canWrite && canManage(it)
                              ? [
                                  { label: 'Edit tiket', icon: 'edit', onClick: () => setEditing(it) },
                                  { label: 'Hapus tiket', icon: 'delete', tone: 'danger' as const, onClick: () => handleDelete(it) },
                                ]
                              : []),
                          ]}
                        />
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {showCreate && (
        <TicketForm
          itemType={itemType}
          onClose={() => setShowCreate(false)}
          onSaved={() => {
            setShowCreate(false)
            toast('success', 'Tiket dibuat', 'Notifikasi sedang dikirim ke target.')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membuat tiket', msg)}
        />
      )}

      {editing && (
        <TicketForm
          itemType={editing.item_type}
          entry={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            toast('success', 'Tiket diperbarui')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal menyimpan tiket', msg)}
        />
      )}

      <ConfirmDialog
        open={!!confirm}
        title={confirm?.title ?? ''}
        body={confirm?.body}
        confirmLabel={confirm?.confirmLabel}
        tone={confirm?.tone}
        icon={confirm?.icon}
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

/* ------------------------------------------------------------------------- */

function TicketForm({
  itemType,
  entry,
  onClose,
  onSaved,
  onError,
}: {
  itemType: string
  entry?: WorkItem
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const isEdit = !!entry
  const targets = useAsync(() => notificationApi.targetOptions(), [])
  // Kategori & subkategori dari Master Data (hierarki parent_id).
  const categories = useAsync(() => masterDataApi.list({ kind: 'ticket_category' }), [])
  const incidentTypes = useAsync(() => masterDataApi.list({ kind: 'incident_type' }), [])
  // Tag tiket dari Master Data (kind ticket_tag), multi-pilih.
  const tagOptions = useAsync(() => masterDataApi.list({ kind: 'ticket_tag', active: 'true' }), [])

  const [title, setTitle] = useState(entry?.title ?? '')
  const [description, setDescription] = useState(entry?.description ?? '')
  const [priority, setPriority] = useState(entry?.priority ?? 'normal')
  const [targetId, setTargetId] = useState(entry?.target_id ?? '')
  const [category, setCategory] = useState(entry?.ticket?.category ?? '')
  const [subcategory, setSubcategory] = useState(entry?.ticket?.subcategory ?? '')
  const [incidentType, setIncidentType] = useState(entry?.ticket?.incident_type ?? '')
  const [impact, setImpact] = useState(entry?.ticket?.impact ?? 'medium')
  const [urgency, setUrgency] = useState(entry?.ticket?.urgency ?? 'medium')
  const [group, setGroup] = useState(entry?.ticket?.assignment_group ?? '')
  const [tags, setTags] = useState<string[]>(entry?.tags ?? [])
  const [busy, setBusy] = useState(false)

  function toggleTag(code: string) {
    setTags((prev) => (prev.includes(code) ? prev.filter((t) => t !== code) : [...prev, code]))
  }

  const allEntries = categories.data?.entries ?? []
  // Kategori = entri tanpa parent; subkategori = anak dari kategori terpilih.
  const categoryOptions = useMemo(() => allEntries.filter((e) => !e.parent_id), [allEntries])
  const subcategoryOptions = useMemo(() => {
    const parent = allEntries.find((e) => e.label === category || e.code === category)
    if (!parent) return []
    return allEntries.filter((e) => e.parent_id === parent.id)
  }, [allEntries, category])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const payload = {
        title,
        description,
        priority,
        target_id: targetId || '',
        tags,
      }
      const ticket = {
        category,
        subcategory,
        incident_type: incidentType,
        impact,
        urgency,
        assignment_group: group,
      }
      if (isEdit && entry) {
        await workItemsApi.update(entry.id, { ...payload, ticket })
      } else {
        await workItemsApi.create({ item_type: itemType, ...payload, ticket })
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
      title={isEdit ? `Edit Tiket — ${entry?.ref_no}` : 'Tiket Baru'}
      width="lg"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !title.trim()}>
            {busy ? 'Menyimpan…' : isEdit ? 'Simpan Perubahan' : 'Buat Tiket'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <div>
          <label className="label-field" htmlFor="tf-title">
            Judul
          </label>
          <input
            id="tf-title"
            className="input"
            required
            autoFocus
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="mis. Link down ke POP Bandung"
          />
        </div>

        <div>
          <label className="label-field" htmlFor="tf-desc">
            Deskripsi
          </label>
          <textarea
            id="tf-desc"
            className="input h-20 py-2"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Kronologi, dampak, langkah penanganan…"
          />
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="tf-cat">
              Kategori
            </label>
            <select
              id="tf-cat"
              className="input"
              value={category}
              onChange={(e) => {
                setCategory(e.target.value)
                setSubcategory('')
              }}
            >
              <option value="">— pilih kategori —</option>
              {categoryOptions.map((c) => (
                <option key={c.id} value={c.label}>
                  {c.label}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="tf-subcat">
              Subkategori
            </label>
            <select
              id="tf-subcat"
              className="input"
              value={subcategory}
              disabled={!category || subcategoryOptions.length === 0}
              onChange={(e) => setSubcategory(e.target.value)}
            >
              <option value="">— pilih subkategori —</option>
              {subcategoryOptions.map((c) => (
                <option key={c.id} value={c.label}>
                  {c.label}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="tf-incident">
              Jenis gangguan
            </label>
            <select id="tf-incident" className="input" value={incidentType} onChange={(e) => setIncidentType(e.target.value)}>
              <option value="">— pilih jenis —</option>
              {(incidentTypes.data?.entries ?? []).map((c) => (
                <option key={c.id} value={c.label}>
                  {c.label}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="tf-group">
              Grup penanganan
            </label>
            <input
              id="tf-group"
              className="input"
              value={group}
              onChange={(e) => setGroup(e.target.value)}
              placeholder="mis. NOC L1"
            />
          </div>
        </div>

        <div className="grid gap-3.5 sm:grid-cols-3">
          <div>
            <label className="label-field" htmlFor="tf-impact">
              Dampak
            </label>
            <select id="tf-impact" className="input" value={impact} onChange={(e) => setImpact(e.target.value)}>
              {LEVELS.map((l) => (
                <option key={l} value={l}>
                  {l}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="tf-urgency">
              Urgensi
            </label>
            <select id="tf-urgency" className="input" value={urgency} onChange={(e) => setUrgency(e.target.value)}>
              {LEVELS.map((l) => (
                <option key={l} value={l}>
                  {l}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="tf-priority">
              Prioritas
            </label>
            <select id="tf-priority" className="input" value={priority} onChange={(e) => setPriority(e.target.value)}>
              {PRIORITIES.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="tf-target">
              Target notifikasi
            </label>
            <select id="tf-target" className="input" value={targetId} onChange={(e) => setTargetId(e.target.value)}>
              <option value="">— Default sistem —</option>
              {(targets.data?.targets ?? []).map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </div>
          <div>
            <div className="label-field">Tag</div>
            <div className="flex flex-wrap gap-1.5">
              {(tagOptions.data?.entries ?? []).map((t) => {
                const on = tags.includes(t.label)
                return (
                  <button
                    key={t.id}
                    type="button"
                    onClick={() => toggleTag(t.label)}
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
              {(tagOptions.data?.entries ?? []).length === 0 && (
                <span className="text-label-sm text-text-secondary">Belum ada tag di Master Data.</span>
              )}
            </div>
          </div>
        </div>

        <p className="text-label-sm text-text-secondary">
          Tiket tidak memakai tenggat waktu. SLA dihitung otomatis sejak tiket mulai ditangani
          sampai ditutup.
        </p>
      </form>
    </Modal>
  )
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
