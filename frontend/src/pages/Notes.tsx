import { useEffect, useMemo, useState } from 'react'
import { api, ApiUser, Note, notesApi } from '../api'
import {
  ConfirmDialog,
  EmptyState,
  ErrorState,
  LoadingBlock,
  Modal,
  PageHeader,
  formatWIB,
} from '../components/ui'
import { useAsync, useDebounced } from '../hooks'

export type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

type ConfirmState = {
  title: string
  body?: string
  confirmLabel?: string
  tone?: 'primary' | 'danger'
  icon?: string
  run: () => Promise<void>
}

/** Palet warna sticky note (label → kelas Tailwind). */
const NOTE_COLORS: { code: string; label: string; swatch: string; card: string }[] = [
  { code: 'yellow', label: 'Kuning', swatch: 'bg-amber-300', card: 'bg-amber-50 border-amber-200 dark:bg-amber-950/40 dark:border-amber-800/70' },
  { code: 'green', label: 'Hijau', swatch: 'bg-emerald-300', card: 'bg-emerald-50 border-emerald-200 dark:bg-emerald-950/40 dark:border-emerald-800/70' },
  { code: 'blue', label: 'Biru', swatch: 'bg-sky-300', card: 'bg-sky-50 border-sky-200 dark:bg-sky-950/40 dark:border-sky-800/70' },
  { code: 'pink', label: 'Pink', swatch: 'bg-pink-300', card: 'bg-pink-50 border-pink-200 dark:bg-pink-950/40 dark:border-pink-800/70' },
  { code: 'purple', label: 'Ungu', swatch: 'bg-violet-300', card: 'bg-violet-50 border-violet-200 dark:bg-violet-950/40 dark:border-violet-800/70' },
  { code: 'gray', label: 'Abu', swatch: 'bg-slate-300', card: 'bg-slate-50 border-slate-200 dark:bg-slate-800/40 dark:border-slate-700' },
]

function colorCard(code: string): string {
  return NOTE_COLORS.find((c) => c.code === code)?.card ?? 'bg-surface-container-lowest border-border'
}

/**
 * Notes (F26) — papan catatan (sticky notes) mandiri dengan tim pemilik dan
 * dukungan berbagi ke tim lain. Flag visibilitas internal/eksternal.
 */
export default function Notes({ user, toast }: { user: ApiUser | null; toast: Toast }) {
  const [visTab, setVisTab] = useState<'' | 'internal' | 'eksternal'>('')
  const [search, setSearch] = useState('')
  const debounced = useDebounced(search)

  const notes = useAsync(
    () =>
      notesApi.list({
        ...(visTab ? { visibility: visTab } : {}),
        ...(debounced ? { q: debounced } : {}),
      }),
    [visTab, debounced],
  )

  const teams = useAsync(() => api.teams(), [])
  const teamName = (id?: string) => teams.data?.teams.find((t) => t.id === id)?.name

  const [editing, setEditing] = useState<Note | 'new' | null>(null)
  const [shareTarget, setShareTarget] = useState<Note | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)

  const canWrite = !!user && (user.is_super === true || (user.permissions ?? []).includes('notes.write') || user.role === 'admin')
  const isAdmin = !!user && (user.role === 'admin' || user.is_super === true)

  const rows = notes.data?.notes ?? []
  const pinned = rows.filter((n) => n.pinned)
  const others = rows.filter((n) => !n.pinned)

  function canEdit(n: Note): boolean {
    if (isAdmin) return true
    if (!user) return false
    if (n.created_by?.toLowerCase() === user.username.toLowerCase()) return true
    return !!user.team_id && !!n.owner_team_id && user.team_id === n.owner_team_id
  }

  function requestDelete(n: Note) {
    setConfirm({
      title: 'Hapus catatan?',
      body: `Catatan "${n.title || '(tanpa judul)'}" akan dihapus permanen.`,
      tone: 'danger',
      confirmLabel: 'Hapus',
      run: async () => {
        await notesApi.remove(n.id)
        toast('success', 'Catatan dihapus')
        notes.reload()
      },
    })
  }

  async function togglePin(n: Note) {
    try {
      await notesApi.update(n.id, { pinned: !n.pinned })
      notes.reload()
    } catch (err) {
      toast('error', 'Gagal menyematkan', err instanceof Error ? err.message : undefined)
    }
  }

  return (
    <div>
      <PageHeader
        kicker="Operasional"
        title="Catatan"
        description="Catatan cepat (sticky notes) untuk tim. Bisa dibagikan ke tim lain dan ditandai internal/eksternal."
        actions={
          <>
            <button className="btn-secondary" onClick={notes.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            {canWrite && (
              <button className="btn-primary" onClick={() => setEditing('new')}>
                <span className="material-symbols-outlined text-[18px]">add</span>
                Catatan Baru
              </button>
            )}
          </>
        }
      />

      {/* Filter visibilitas + cari */}
      <section className="card mb-5 p-4">
        <div className="flex flex-wrap items-end gap-3">
          <div className="flex flex-wrap gap-1">
            {(
              [
                { v: '', label: 'Semua' },
                { v: 'internal', label: 'Internal' },
                { v: 'eksternal', label: 'Eksternal' },
              ] as const
            ).map((t) => (
              <button
                key={t.v}
                className={`rounded-full border px-3 py-1.5 text-label-md ${
                  visTab === t.v
                    ? 'border-primary bg-primary-container text-on-primary-container font-semibold'
                    : 'border-border bg-surface-container-lowest text-text-secondary'
                }`}
                onClick={() => setVisTab(t.v)}
              >
                {t.label}
              </button>
            ))}
          </div>
          <div className="min-w-[200px] flex-1">
            <label className="label-field" htmlFor="nt-search">
              Cari
            </label>
            <input
              id="nt-search"
              className="input"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="cari judul atau isi catatan…"
            />
          </div>
        </div>
      </section>

      {notes.loading && <LoadingBlock label="Memuat catatan…" />}
      {notes.error && <ErrorState message={notes.error} onRetry={notes.reload} />}

      {!notes.loading && !notes.error && rows.length === 0 && (
        <EmptyState
          icon="sticky_note_2"
          title="Belum ada catatan"
          body={canWrite ? 'Buat catatan pertama lewat tombol "Catatan Baru".' : 'Belum ada catatan untuk tim Anda.'}
        />
      )}

      {pinned.length > 0 && (
        <>
          <div className="kicker mb-2">Disematkan</div>
          <NoteGrid notes={pinned} teamName={teamName} canWrite={canWrite} canEdit={canEdit} onPin={togglePin} onEdit={setEditing} onDelete={requestDelete} onShare={setShareTarget} />
          <div className="mb-5" />
        </>
      )}

      {others.length > 0 && (
        <>
          {pinned.length > 0 && <div className="kicker mb-2">Lainnya</div>}
          <NoteGrid notes={others} teamName={teamName} canWrite={canWrite} canEdit={canEdit} onPin={togglePin} onEdit={setEditing} onDelete={requestDelete} onShare={setShareTarget} />
        </>
      )}

      {editing && (
        <NoteForm
          existing={editing === 'new' ? undefined : editing}
          teams={(teams.data?.teams ?? []).map((t) => ({ id: t.id, name: t.name }))}
          isAdmin={isAdmin}
          defaultTeamID={user?.team_id}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            toast('success', 'Catatan disimpan')
            notes.reload()
          }}
          onError={(msg) => toast('error', 'Gagal menyimpan', msg)}
        />
      )}

      {shareTarget && (
        <ShareModal
          note={shareTarget}
          teams={(teams.data?.teams ?? []).map((t) => ({ id: t.id, name: t.name }))}
          onClose={() => setShareTarget(null)}
          onSaved={() => {
            setShareTarget(null)
            toast('success', 'Pembagian diperbarui')
            notes.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membagikan', msg)}
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

/** NoteGrid menata catatan dalam grid responsif. */
function NoteGrid({
  notes,
  teamName,
  canWrite,
  canEdit,
  onPin,
  onEdit,
  onDelete,
  onShare,
}: {
  notes: Note[]
  teamName: (id?: string) => string | undefined
  canWrite: boolean
  canEdit: (n: Note) => boolean
  onPin: (n: Note) => void
  onEdit: (n: Note) => void
  onDelete: (n: Note) => void
  onShare: (n: Note) => void
}) {
  return (
    <div className="mb-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
      {notes.map((n) => {
        const editable = canEdit(n)
        const shared = n.shared_teams ?? []
        return (
          <article
            key={n.id}
            className={`flex flex-col rounded-control border p-3.5 shadow-sm ${colorCard(n.color)}`}
          >
            <div className="flex items-start justify-between gap-2">
              <h3 className="font-headline text-body-md font-semibold">{n.title || '(tanpa judul)'}</h3>
              {canWrite && (
                <button
                  className="shrink-0 text-text-secondary hover:text-primary"
                  title={n.pinned ? 'Lepas sematan' : 'Sematkan'}
                  onClick={() => onPin(n)}
                >
                  <span className="material-symbols-outlined text-[18px]">
                    {n.pinned ? 'push_pin' : 'keep'}
                  </span>
                </button>
              )}
            </div>

            {n.body && (
              <p className="mt-1.5 line-clamp-6 whitespace-pre-wrap text-body-sm text-text-primary">{n.body}</p>
            )}

            <div className="mt-2 flex flex-wrap items-center gap-1.5">
              <span
                className={`rounded-full border px-1.5 py-0.5 text-[10px] font-bold uppercase ${
                  n.visibility === 'eksternal'
                    ? 'border-info-container bg-info-container text-on-info-container'
                    : 'border-outline-variant bg-surface-container text-text-secondary'
                }`}
              >
                {n.visibility}
              </span>
              {n.owner_team_id && (
                <span className="rounded-full bg-surface-container px-1.5 py-0.5 text-[10px] font-semibold text-text-secondary">
                  {teamName(n.owner_team_id) ?? 'Tim'}
                </span>
              )}
              {shared.length > 0 && (
                <span
                  className="inline-flex items-center gap-0.5 rounded-full bg-surface-container px-1.5 py-0.5 text-[10px] font-semibold text-text-secondary"
                  title={`Dibagikan ke: ${shared.map((s) => s.name).join(', ')}`}
                >
                  <span className="material-symbols-outlined text-[12px]">group</span>
                  {shared.length}
                </span>
              )}
            </div>

            <div className="mt-auto flex items-center justify-between gap-1 border-t border-black/5 pt-2">
              <span className="mono text-label-sm text-text-secondary">
                {n.updated_by_username || n.created_by} · {formatWIB(n.updated_at, true)}
              </span>
              <div className="flex items-center gap-0.5">
                {canWrite && (
                  <button className="btn-ghost h-7 w-7 px-0" title="Bagikan ke tim" onClick={() => onShare(n)}>
                    <span className="material-symbols-outlined text-[17px]">share</span>
                  </button>
                )}
                {editable && (
                  <button className="btn-ghost h-7 w-7 px-0" title="Ubah" onClick={() => onEdit(n)}>
                    <span className="material-symbols-outlined text-[17px]">edit</span>
                  </button>
                )}
                {editable && (
                  <button className="btn-ghost h-7 w-7 px-0 text-critical" title="Hapus" onClick={() => onDelete(n)}>
                    <span className="material-symbols-outlined text-[17px]">delete</span>
                  </button>
                )}
              </div>
            </div>
          </article>
        )
      })}
    </div>
  )
}

/** NoteForm membuat/menyunting catatan. */
function NoteForm({
  existing,
  teams,
  isAdmin,
  defaultTeamID,
  onClose,
  onSaved,
  onError,
}: {
  existing?: Note
  teams: { id: string; name: string }[]
  isAdmin: boolean
  defaultTeamID?: string
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const [title, setTitle] = useState(existing?.title ?? '')
  const [body, setBody] = useState(existing?.body ?? '')
  const [visibility, setVisibility] = useState<'internal' | 'eksternal'>(existing?.visibility ?? 'internal')
  const [color, setColor] = useState(existing?.color ?? 'yellow')
  const [pinned, setPinned] = useState(existing?.pinned ?? false)
  const [ownerTeam, setOwnerTeam] = useState(existing?.owner_team_id ?? defaultTeamID ?? '')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!title.trim() && !body.trim()) {
      onError('Judul atau isi catatan wajib diisi.')
      return
    }
    setBusy(true)
    try {
      const payload = {
        title: title.trim(),
        body,
        visibility,
        color,
        pinned,
        ...(isAdmin ? { owner_team_id: ownerTeam } : {}),
      }
      if (existing) {
        await notesApi.update(existing.id, payload)
      } else {
        await notesApi.create(payload)
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
      title={existing ? 'Ubah Catatan' : 'Catatan Baru'}
      width="md"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy}>
            {busy ? 'Menyimpan…' : existing ? 'Simpan Perubahan' : 'Simpan'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <div>
          <label className="label-field" htmlFor="nf-title">
            Judul
          </label>
          <input
            id="nf-title"
            className="input"
            autoFocus
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="mis. Kredensial VPN pelanggan X"
          />
        </div>
        <div>
          <label className="label-field" htmlFor="nf-body">
            Isi catatan
          </label>
          <textarea
            id="nf-body"
            className="input h-32 py-2"
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="Tulis catatan…"
          />
        </div>
        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="nf-vis">
              Visibilitas
            </label>
            <select
              id="nf-vis"
              className="input"
              value={visibility}
              onChange={(e) => setVisibility(e.target.value as 'internal' | 'eksternal')}
            >
              <option value="internal">Internal (tim sendiri)</option>
              <option value="eksternal">Eksternal (tim terbagi + tim sendiri)</option>
            </select>
          </div>
          {isAdmin && (
            <div>
              <label className="label-field" htmlFor="nf-team">
                Tim pemilik
              </label>
              <select id="nf-team" className="input" value={ownerTeam} onChange={(e) => setOwnerTeam(e.target.value)}>
                <option value="">— Tanpa tim —</option>
                {teams.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>
        <div>
          <span className="label-field">Warna</span>
          <div className="mt-1 flex flex-wrap gap-2">
            {NOTE_COLORS.map((c) => (
              <button
                key={c.code}
                type="button"
                title={c.label}
                className={`h-7 w-7 rounded-full border-2 ${c.swatch} ${
                  color === c.code ? 'border-text-primary' : 'border-transparent'
                }`}
                onClick={() => setColor(c.code)}
              />
            ))}
          </div>
        </div>
        <label className="flex items-center gap-2 text-body-sm">
          <input type="checkbox" checked={pinned} onChange={(e) => setPinned(e.target.checked)} />
          Sematkan di atas
        </label>
      </form>
    </Modal>
  )
}

/** ShareModal memilih tim (multi) yang boleh melihat catatan. */
function ShareModal({
  note,
  teams,
  onClose,
  onSaved,
  onError,
}: {
  note: Note
  teams: { id: string; name: string }[]
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const initial = useMemo(() => new Set(note.shared_team_ids ?? []), [note.shared_team_ids])
  const [selected, setSelected] = useState<Set<string>>(new Set(initial))
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setSelected(new Set(initial))
  }, [initial])

  function toggle(id: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  async function submit() {
    setBusy(true)
    try {
      await notesApi.setShares(note.id, Array.from(selected))
      onSaved()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Terjadi kesalahan')
    } finally {
      setBusy(false)
    }
  }

  const ownerTeam = note.owner_team_id

  return (
    <Modal
      title="Bagikan Catatan"
      width="sm"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy}>
            {busy ? 'Menyimpan…' : 'Simpan'}
          </button>
        </>
      }
    >
      <p className="mb-3 text-body-sm text-text-secondary">
        Pilih tim yang boleh melihat catatan ini. Tim pemilik selalu dapat melihat.
      </p>
      <div className="space-y-1.5">
        {teams.map((t) => {
          const isOwner = t.id === ownerTeam
          return (
            <label
              key={t.id}
              className={`flex items-center gap-2 rounded-control border border-border px-3 py-2 text-body-sm ${
                isOwner ? 'bg-surface-container opacity-70' : ''
              }`}
            >
              <input
                type="checkbox"
                disabled={isOwner}
                checked={isOwner || selected.has(t.id)}
                onChange={() => toggle(t.id)}
              />
              {t.name}
              {isOwner && <span className="ml-auto text-label-sm text-text-secondary">tim pemilik</span>}
            </label>
          )
        })}
      </div>
    </Modal>
  )
}
