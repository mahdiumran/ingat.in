import { useEffect, useState } from 'react'
import { api, ApiUser, notificationApi, WorkItem, workItemsApi } from '../api'
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
  formatWIB,
} from '../components/ui'
import { useAsync, useDebounced, useTagColors } from '../hooks'
import { statusTone } from '../design/tokens'
import { ItemDetail } from './WorkItemDetail'

export type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

/** Daftar status Todo (sinkron dengan workitems.DefaultWorkflows["task"]). */
export const TASK_STATUSES = ['accepted', 'on_progress', 'expired', 'canceled', 'closed']

/** Konfirmasi aksi yang menampilkan ConfirmDialog sebelum dijalankan. */
export type ConfirmState = {
  title: string
  body?: string
  confirmLabel?: string
  tone?: 'primary' | 'danger'
  icon?: string
  run: () => Promise<void>
}

/**
 * Todos (F5) — item_type = task.
 *
 * Komponen ini juga menjadi rujukan pola untuk Reminders (F6) dan RFS (F7):
 * filter eksplisit, state loading/error/empty, dan detail dengan timeline.
 */
export default function Todos({
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
  const [priority, setPriority] = useState('')
  const [owner, setOwner] = useState('')
  const [teamFilter, setTeamFilter] = useState('')
  const [offset, setOffset] = useState(0)
  const [showCreate, setShowCreate] = useState(false)
  const [editing, setEditing] = useState<WorkItem | null>(null)
  const [detailID, setDetailID] = useState<string | null>(null)
  const [notifyingID, setNotifyingID] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)

  const tagColors = useTagColors()
  // F31: tim — untuk filter (admin) & pemilih tim saat membuat todo.
  const teams = useAsync(() => api.teams(), [])
  const teamList = teams.data?.teams ?? []
  const teamName = (id?: string) => teamList.find((t) => t.id === id)?.name

  // Buka detail saat lonceng notifikasi meminta fokus ke item tertentu.
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
        type: 'task',
        ...(status ? { status } : {}),
        ...(priority ? { priority } : {}),
        ...(owner ? { owner } : {}),
        ...(teamFilter ? { team_id: teamFilter } : {}),
        ...(debouncedSearch ? { q: debouncedSearch } : {}),
        limit: String(limit),
        offset: String(offset),
      }),
    [status, priority, owner, teamFilter, debouncedSearch, offset],
  )

  const rows = items.data?.items ?? []
  const total = items.data?.total ?? 0
  const canWrite = !!user && user.role !== 'viewer'
  const isAdmin = user?.role === 'admin'

  /** canManageUser mengikuti aturan backend: admin, pembuat, atau owner. */
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
      title: 'Hapus todo?',
      body: `Todo ${item.ref_no} — "${item.title}" akan dihapus. Tindakan ini tidak dapat dibatalkan.`,
      tone: 'danger',
      confirmLabel: 'Hapus',
      run: async () => {
        await workItemsApi.remove(item.id)
        toast('success', 'Todo dihapus', item.ref_no)
        items.reload()
      },
    })
  }

  // Kirim notifikasi manual: enqueue ke outbox lalu worker mengirim.
  async function handleNotify(item: WorkItem) {
    setNotifyingID(item.id)
    try {
      const res = await workItemsApi.triggerNotification(item.id, { offset_label: 'MANUAL' })
      if (res.added > 0) {
        toast('success', 'Notifikasi dikirim', res.message)
      } else {
        toast('warning', 'Tidak ada pesan baru', 'Binding aktif mungkin sudah menerima pesan ini.')
      }
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
        title="Todo Tasks"
        description="Task operasional dengan notifikasi otomatis saat dibuat."
        actions={
          <>
            <button className="btn-secondary" onClick={items.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            {canWrite && (
              <button className="btn-primary" onClick={() => setShowCreate(true)}>
                <span className="material-symbols-outlined text-[18px]">add_task</span>
                Todo Baru
              </button>
            )}
          </>
        }
      />

      {/* Filter */}
      <section className="card mb-5 p-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <label className="label-field" htmlFor="t-search">
              Cari
            </label>
            <input
              id="t-search"
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
            <label className="label-field" htmlFor="t-status">
              Status
            </label>
            <select
              id="t-status"
              className="input"
              value={status}
              onChange={(e) => {
                setStatus(e.target.value)
                setOffset(0)
              }}
            >
              <option value="">Semua</option>
              <option value="accepted">Accepted</option>
              <option value="on_progress">On progress</option>
              <option value="expired">Expired</option>
              <option value="canceled">Canceled</option>
              <option value="closed">Closed</option>
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="t-priority">
              Prioritas
            </label>
            <select
              id="t-priority"
              className="input"
              value={priority}
              onChange={(e) => {
                setPriority(e.target.value)
                setOffset(0)
              }}
            >
              <option value="">Semua</option>
              <option value="critical">Critical</option>
              <option value="high">High</option>
              <option value="normal">Normal</option>
              <option value="low">Low</option>
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="t-owner">
              Dibuat oleh
            </label>
            <input
              id="t-owner"
              className="input mono"
              placeholder="username"
              value={owner}
              onChange={(e) => {
                setOwner(e.target.value)
                setOffset(0)
              }}
            />
          </div>
          {isAdmin && (
            <div>
              <label className="label-field" htmlFor="t-team">
                Tim
              </label>
              <select
                id="t-team"
                className="input"
                value={teamFilter}
                onChange={(e) => {
                  setTeamFilter(e.target.value)
                  setOffset(0)
                }}
              >
                <option value="">Semua tim</option>
                {teamList.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>
      </section>

      <section className="card">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3.5">
          <div>
            <h3 className="font-headline text-lg font-semibold">Daftar Todo</h3>
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
            icon="task_alt"
            title="Belum ada todo"
            body={
              debouncedSearch || status || priority || owner
                ? 'Tidak ada todo yang cocok dengan filter. Coba ubah filter atau kata kunci.'
                : 'Buat todo pertama. Notifikasi akan dikirim otomatis ke target yang dipilih.'
            }
            action={
              canWrite && !debouncedSearch && !status ? (
                <button className="btn-primary" onClick={() => setShowCreate(true)}>
                  <span className="material-symbols-outlined text-[18px]">add_task</span>
                  Buat Todo
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
                  <th>Tim</th>
                  <th>Prioritas</th>
                  <th>Status</th>
                  <th>Tag</th>
                  <th>Due (WIB)</th>
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
                    <td className="max-w-[320px]">
                      <button className="block truncate text-left font-medium hover:underline" onClick={() => setDetailID(it.id)}>
                        {it.title}
                      </button>
                      {it.device_ref && (
                        <span className="mono text-label-sm text-text-secondary">{it.device_ref}</span>
                      )}
                    </td>
                    <td>
                      <span className="badge border-outline-variant bg-surface-container text-text-secondary">
                        {teamName(it.team_id) ?? 'Tanpa tim'}
                      </span>
                    </td>
                    <td>
                      <PriorityBadge priority={it.priority} />
                    </td>
                    <td>
                      <StatusBadge status={it.status} tone={statusTone[it.status]} />
                    </td>
                    <td className="max-w-[180px]">
                      <TagList tags={it.tags} colors={tagColors} />
                    </td>
                    <td className="mono whitespace-nowrap text-label-md">{formatWIB(it.due_at)}</td>
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
                            <button
                              className="btn-ghost h-8 w-8 px-0"
                              title="Edit"
                              onClick={() => setEditing(it)}
                            >
                              <span className="material-symbols-outlined text-[18px]">edit</span>
                            </button>
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
        <TaskForm
          user={user}
          teams={teamList}
          onClose={() => setShowCreate(false)}
          onSaved={() => {
            setShowCreate(false)
            toast('success', 'Todo dibuat', 'Notifikasi sedang dikirim ke target.')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membuat todo', msg)}
        />
      )}

      {editing && (
        <TaskForm
          user={user}
          teams={teamList}
          entry={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            toast('success', 'Todo diperbarui')
            items.reload()
          }}
          onError={(msg) => toast('error', 'Gagal menyimpan todo', msg)}
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

export function TaskForm({
  entry,
  user,
  teams = [],
  onClose,
  onSaved,
  onError,
}: {
  entry?: WorkItem
  user?: ApiUser | null
  teams?: { id: string; name: string }[]
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const isEdit = !!entry
  const targets = useAsync(() => notificationApi.targetOptions(), [])

  const [title, setTitle] = useState(entry?.title ?? '')
  const [description, setDescription] = useState(entry?.description ?? '')
  const [priority, setPriority] = useState(entry?.priority ?? 'normal')
  const [owner, setOwner] = useState(entry?.owner_username ?? '')
  const [dueAt, setDueAt] = useState(toLocalInput(entry?.due_at))
  const [targetId, setTargetId] = useState(entry?.target_id ?? '')
  const [deviceRef, setDeviceRef] = useState(entry?.device_ref ?? '')
  const [tags, setTags] = useState((entry?.tags ?? []).join(', '))
  const [teamId, setTeamId] = useState(entry?.team_id ?? user?.team_id ?? '')
  const [busy, setBusy] = useState(false)
  const isAdmin = user?.role === 'admin'

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const payload = {
        title,
        description,
        priority,
        owner_username: owner,
        due_at: dueAt ? new Date(dueAt).toISOString() : '',
        target_id: targetId || '',
        device_ref: deviceRef,
        // F31: pembagian per tim (admin dapat memilih; non-admin dipaksa server).
        ...(isAdmin && teamId ? { team_id: teamId } : {}),
        tags: tags
          ? tags
              .split(',')
              .map((t) => t.trim())
              .filter(Boolean)
          : [],
      }
      if (isEdit && entry) {
        await workItemsApi.update(entry.id, payload)
      } else {
        await workItemsApi.create({
          item_type: 'task',
          ...payload,
          task: { checklist: [] },
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
      title={isEdit ? `Edit Todo — ${entry?.ref_no}` : 'Todo Baru'}
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !title.trim()}>
            {busy ? 'Menyimpan…' : isEdit ? 'Simpan Perubahan' : 'Buat Todo'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <div>
          <label className="label-field" htmlFor="nt-title">
            Judul
          </label>
          <input
            id="nt-title"
            className="input"
            required
            autoFocus
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="mis. Cek BGP flap MX204-CGK1"
          />
        </div>

        <div>
          <label className="label-field" htmlFor="nt-desc">
            Deskripsi
          </label>
          <textarea
            id="nt-desc"
            className="input h-20 py-2"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Langkah atau konteks tambahan…"
          />
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="nt-priority">
              Prioritas
            </label>
            <select id="nt-priority" className="input" value={priority} onChange={(e) => setPriority(e.target.value)}>
              <option value="low">Low</option>
              <option value="normal">Normal</option>
              <option value="high">High</option>
              <option value="critical">Critical</option>
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="nt-owner">
              Owner
            </label>
            <input
              id="nt-owner"
              className="input mono"
              value={owner}
              onChange={(e) => setOwner(e.target.value)}
              placeholder="username"
            />
          </div>
        </div>

        {isAdmin && (
          <div>
            <label className="label-field" htmlFor="nt-team">
              Tim
            </label>
            <select id="nt-team" className="input" value={teamId} onChange={(e) => setTeamId(e.target.value)}>
              <option value="">— Tanpa tim —</option>
              {teams.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
            <p className="mt-1 text-label-sm text-text-secondary">
              Todo hanya terlihat oleh tim yang dipilih (admin melihat semua).
            </p>
          </div>
        )}

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="nt-due">
              Tenggat
            </label>
            <input
              id="nt-due"
              type="datetime-local"
              className="input"
              value={dueAt}
              onChange={(e) => setDueAt(e.target.value)}
            />
          </div>
          <div>
            <label className="label-field" htmlFor="nt-target">
              Target notifikasi
            </label>
            <select id="nt-target" className="input" value={targetId} onChange={(e) => setTargetId(e.target.value)}>
              <option value="">— Default sistem —</option>
              {(targets.data?.targets ?? []).map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
            <p className="mt-1 text-label-sm text-text-secondary">
              Notifikasi dikirim ke seluruh binding aktif pada target.
            </p>
          </div>
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="nt-device">
              Referensi perangkat
            </label>
            <input
              id="nt-device"
              className="input mono"
              value={deviceRef}
              onChange={(e) => setDeviceRef(e.target.value)}
              placeholder="mis. MX204-CGK1"
            />
          </div>
          <div>
            <label className="label-field" htmlFor="nt-tags">
              Tag
            </label>
            <input
              id="nt-tags"
              className="input"
              value={tags}
              onChange={(e) => setTags(e.target.value)}
              placeholder="bgp, cgk1 (pisahkan dengan koma)"
            />
          </div>
        </div>
      </form>
    </Modal>
  )
}
