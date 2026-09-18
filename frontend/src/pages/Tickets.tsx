import { useEffect, useMemo, useState } from 'react'
import { ApiUser, masterDataApi, notificationApi, Target, WorkItem, workItemsApi } from '../api'
import {
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
}: {
  user: ApiUser | null
  toast: Toast
  focusItemId?: string | null
  onFocusConsumed?: () => void
}) {
  const [itemType, setItemType] = useState('incident')
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [priority, setPriority] = useState('')
  const [creator, setCreator] = useState('')
  const [offset, setOffset] = useState(0)
  const [showCreate, setShowCreate] = useState(false)
  const [editing, setEditing] = useState<WorkItem | null>(null)
  const [detailID, setDetailID] = useState<string | null>(null)
  const [notifyingID, setNotifyingID] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)

  const tagColors = useTagColors()

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
        type: itemType,
        ...(status ? { status } : {}),
        ...(priority ? { priority } : {}),
        ...(creator ? { owner: creator } : {}),
        ...(debouncedSearch ? { q: debouncedSearch } : {}),
        limit: String(limit),
        offset: String(offset),
      }),
    [itemType, status, priority, creator, debouncedSearch, offset],
  )

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
              setOffset(0)
            }}
            className={t.value === itemType ? 'btn-primary' : 'btn-secondary'}
          >
            {t.label}
          </button>
        ))}
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
                    </td>
                    <td className="max-w-[280px]">
                      <button className="block truncate text-left font-medium hover:underline" onClick={() => setDetailID(it.id)}>
                        {it.title}
                      </button>
                      {it.device_ref && <span className="mono text-label-sm text-text-secondary">{it.device_ref}</span>}
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
                            {canManage(it) && (
                              <button className="btn-ghost h-8 w-8 px-0" title="Edit" onClick={() => setEditing(it)}>
                                <span className="material-symbols-outlined text-[18px]">edit</span>
                              </button>
                            )}
                            {canManage(it) && (
                              <button className="btn-ghost h-8 w-8 px-0 text-critical" title="Hapus" onClick={() => handleDelete(it)}>
                                <span className="material-symbols-outlined text-[18px]">delete</span>
                              </button>
                            )}
                          </>
                        )}
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
  const targets = useAsync(() => notificationApi.targets(), [])
  // Kategori & subkategori dari Master Data (hierarki parent_id).
  const categories = useAsync(() => masterDataApi.list({ kind: 'ticket_category' }), [])
  const incidentTypes = useAsync(() => masterDataApi.list({ kind: 'incident_type' }), [])

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
  const [tags, setTags] = useState((entry?.tags ?? []).join(', '))
  const [busy, setBusy] = useState(false)

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
        tags: tags
          ? tags
              .split(',')
              .map((t) => t.trim())
              .filter(Boolean)
          : [],
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
              {((targets.data?.targets ?? []) as Target[]).map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="tf-tags">
              Tag
            </label>
            <input
              id="tf-tags"
              className="input"
              value={tags}
              onChange={(e) => setTags(e.target.value)}
              placeholder="bgp, cgk1 (pisahkan dengan koma)"
            />
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
