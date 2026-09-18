import { useState } from 'react'
import { api, ApiUser } from '../api'
import { EmptyState, ErrorState, LoadingBlock, Modal, PageHeader, formatWIB } from '../components/ui'
import { useAsync } from '../hooks'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

const ROLE_OPTIONS = [
  { value: 'admin', label: 'Administrator', note: 'Akses penuh termasuk user & audit' },
  { value: 'noc', label: 'NOC', note: 'Kelola reminder, RFS, target, provider' },
  { value: 'agent', label: 'Agent', note: 'Menangani work item & tiket' },
  { value: 'sales', label: 'Sales', note: 'Input data RFS' },
  { value: 'viewer', label: 'Viewer', note: 'Hanya membaca' },
]

const roleTone: Record<string, string> = {
  admin: 'bg-critical-container text-on-critical-container border-critical',
  noc: 'bg-primary-container text-on-primary-container border-outline-variant',
  agent: 'bg-info-container text-on-info-container border-outline-variant',
  sales: 'bg-warning-container text-on-warning-container border-outline-variant',
  viewer: 'bg-surface-container text-text-secondary border-outline-variant',
  customer: 'bg-surface-container text-text-secondary border-outline-variant',
}

export default function Users({ user, toast }: { user: ApiUser | null; toast: Toast }) {
  const users = useAsync(() => api.users(), [])
  const teams = useAsync(() => api.teams(), [])

  const [showCreate, setShowCreate] = useState(false)
  const [editTarget, setEditTarget] = useState<ApiUser | null>(null)
  const [resetTarget, setResetTarget] = useState<ApiUser | null>(null)

  const teamName = (id?: string) => teams.data?.teams.find((t) => t.id === id)?.name

  async function handleDelete(target: ApiUser) {
    if (!window.confirm(`Hapus user "${target.username}"? Tindakan ini tidak dapat dibatalkan.`)) return
    try {
      await api.deleteUser(target.id)
      toast('success', 'User dihapus', target.username)
      users.reload()
    } catch (err) {
      toast('error', 'Gagal menghapus user', err instanceof Error ? err.message : undefined)
    }
  }

  async function toggleActive(target: ApiUser) {
    try {
      await api.updateUser(target.id, { is_active: !target.is_active })
      toast('success', target.is_active ? 'User dinonaktifkan' : 'User diaktifkan', target.username)
      users.reload()
    } catch (err) {
      toast('error', 'Gagal mengubah status', err instanceof Error ? err.message : undefined)
    }
  }

  const list = users.data?.users ?? []
  const adminCount = list.filter((u) => u.role === 'admin' && u.is_active).length

  return (
    <div>
      <PageHeader
        kicker="Manajemen"
        title="Users & Teams"
        description="Kelola akses panel dan penugasan tim."
        actions={
          <>
            <button className="btn-secondary" onClick={() => users.reload()}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            <button className="btn-primary" onClick={() => setShowCreate(true)}>
              <span className="material-symbols-outlined text-[18px]">person_add</span>
              Tambah User
            </button>
          </>
        }
      />

      {/* Ringkasan */}
      <div className="mb-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <MiniStat label="Total user" value={list.length} />
        <MiniStat label="Aktif" value={list.filter((u) => u.is_active).length} />
        <MiniStat label="Administrator" value={adminCount} />
        <MiniStat label="Tim" value={teams.data?.teams.length ?? 0} />
      </div>

      {adminCount <= 1 && list.length > 0 && (
        <div className="mb-4 flex items-start gap-2.5 rounded-card border border-outline-variant bg-warning-container/70 p-3.5 text-body-sm text-on-warning-container">
          <span className="material-symbols-outlined text-[19px] shrink-0">warning</span>
          <div>
            Hanya ada satu administrator aktif. Sistem mencegah penghapusan administrator terakhir —
            tambahkan administrator kedua sebelum melakukan perubahan besar.
          </div>
        </div>
      )}

      <section className="card">
        <div className="border-b border-border px-5 py-3.5">
          <h3 className="font-headline text-lg font-semibold">Daftar User</h3>
        </div>

        {users.loading && <LoadingBlock />}
        {users.error && <div className="p-4"><ErrorState message={users.error} onRetry={users.reload} /></div>}
        {!users.loading && !users.error && list.length === 0 && (
          <EmptyState icon="group" title="Belum ada user" body="Tambahkan user pertama untuk mulai bekerja." />
        )}

        {!users.loading && !users.error && list.length > 0 && (
          <div className="overflow-x-auto">
            <table className="table">
              <thead>
                <tr>
                  <th>Username</th>
                  <th>Nama</th>
                  <th>Role</th>
                  <th>Tim</th>
                  <th>Kontak</th>
                  <th>Status</th>
                  <th>Login terakhir</th>
                  <th className="text-right">Aksi</th>
                </tr>
              </thead>
              <tbody>
                {list.map((u) => {
                  const isSelf = u.id === user?.id
                  return (
                    <tr key={u.id}>
                      <td>
                        <div className="flex items-center gap-2">
                          <span className="mono text-label-md">{u.username}</span>
                          {isSelf && <span className="badge bg-info-container text-on-info-container border-outline-variant">anda</span>}
                        </div>
                      </td>
                      <td className="text-text-secondary">{u.full_name || '—'}</td>
                      <td>
                        <span className={`badge ${roleTone[u.role] ?? ''}`}>{u.role}</span>
                      </td>
                      <td className="text-text-secondary">{teamName((u as unknown as { team_id?: string }).team_id) ?? '—'}</td>
                      <td>
                        <div className="flex flex-col gap-0.5">
                          {(u as unknown as { telegram_chat_id?: string }).telegram_chat_id && (
                            <span className="mono text-label-sm text-text-secondary">
                              TG: {(u as unknown as { telegram_chat_id?: string }).telegram_chat_id}
                            </span>
                          )}
                          {(u as unknown as { wa_number?: string }).wa_number && (
                            <span className="mono text-label-sm text-text-secondary">
                              WA: {(u as unknown as { wa_number?: string }).wa_number}
                            </span>
                          )}
                          {!(u as unknown as { telegram_chat_id?: string }).telegram_chat_id &&
                            !(u as unknown as { wa_number?: string }).wa_number && (
                              <span className="text-text-secondary">—</span>
                            )}
                        </div>
                      </td>
                      <td>
                        <span
                          className={`badge ${
                            u.is_active
                              ? 'bg-success-container text-on-success-container border-outline-variant'
                              : 'bg-surface-container text-text-secondary border-outline-variant'
                          }`}
                        >
                          {u.is_active ? 'aktif' : 'nonaktif'}
                        </span>
                      </td>
                      <td className="text-text-secondary">{formatWIB((u as unknown as { last_login_at?: string }).last_login_at)}</td>
                      <td>
                        <div className="flex justify-end gap-1">
                          <button
                            className="btn-ghost h-8 w-8 px-0"
                            title="Ubah user"
                            onClick={() => setEditTarget(u)}
                          >
                            <span className="material-symbols-outlined text-[18px]">edit</span>
                          </button>
                          <button
                            className="btn-ghost h-8 w-8 px-0"
                            title="Reset password"
                            onClick={() => setResetTarget(u)}
                          >
                            <span className="material-symbols-outlined text-[18px]">key</span>
                          </button>
                          <button
                            className="btn-ghost h-8 w-8 px-0"
                            title={u.is_active ? 'Nonaktifkan' : 'Aktifkan'}
                            disabled={isSelf}
                            onClick={() => toggleActive(u)}
                          >
                            <span className="material-symbols-outlined text-[18px]">
                              {u.is_active ? 'block' : 'check_circle'}
                            </span>
                          </button>
                          <button
                            className="btn-ghost h-8 w-8 px-0 text-critical"
                            title="Hapus user"
                            disabled={isSelf}
                            onClick={() => handleDelete(u)}
                          >
                            <span className="material-symbols-outlined text-[18px]">delete</span>
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

      {/* Tim */}
      <section className="card mt-5">
        <div className="border-b border-border px-5 py-3.5">
          <h3 className="font-headline text-lg font-semibold">Tim</h3>
          <p className="text-body-sm text-text-secondary">Tim dipakai untuk penugasan work item dan tiket.</p>
        </div>
        {teams.loading && <LoadingBlock />}
        {!teams.loading && (
          <div className="divide-y divide-border">
            {(teams.data?.teams ?? []).map((t) => (
              <div key={t.id} className="flex items-center justify-between gap-3 px-5 py-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{t.name}</span>
                    <span className="badge bg-surface-container text-text-secondary border-outline-variant">
                      {list.filter((u) => (u as unknown as { team_id?: string }).team_id === t.id).length} anggota
                    </span>
                  </div>
                  <p className="truncate text-body-sm text-text-secondary">{t.description || 'Tanpa deskripsi'}</p>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      {showCreate && (
        <UserForm
          teams={teams.data?.teams ?? []}
          onClose={() => setShowCreate(false)}
          onSaved={() => {
            setShowCreate(false)
            toast('success', 'User dibuat')
            users.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membuat user', msg)}
        />
      )}

      {editTarget && (
        <UserForm
          existing={editTarget}
          teams={teams.data?.teams ?? []}
          onClose={() => setEditTarget(null)}
          onSaved={() => {
            setEditTarget(null)
            toast('success', 'User diperbarui')
            users.reload()
          }}
          onError={(msg) => toast('error', 'Gagal memperbarui user', msg)}
        />
      )}

      {resetTarget && (
        <ResetPasswordForm
          target={resetTarget}
          onClose={() => setResetTarget(null)}
          onSaved={() => {
            setResetTarget(null)
            toast('success', 'Password direset', 'Seluruh sesi user tersebut telah dicabut.')
          }}
          onError={(msg) => toast('error', 'Gagal reset password', msg)}
        />
      )}
    </div>
  )
}

function MiniStat({ label, value }: { label: string; value: number }) {
  return (
    <div className="card px-4 py-3">
      <div className="kicker">{label}</div>
      <div className="mt-0.5 font-headline text-xl font-semibold">{value}</div>
    </div>
  )
}

function UserForm({
  existing,
  teams,
  onClose,
  onSaved,
  onError,
}: {
  existing?: ApiUser
  teams: { id: string; name: string }[]
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const isEdit = !!existing
  const [username, setUsername] = useState(existing?.username ?? '')
  const [fullName, setFullName] = useState(existing?.full_name ?? '')
  const [email, setEmail] = useState(existing?.email ?? '')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState(existing?.role ?? 'viewer')
  const [teamId, setTeamId] = useState((existing as unknown as { team_id?: string })?.team_id ?? '')
  const [telegram, setTelegram] = useState((existing as unknown as { telegram_chat_id?: string })?.telegram_chat_id ?? '')
  const [waNumber, setWaNumber] = useState((existing as unknown as { wa_number?: string })?.wa_number ?? '')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      if (isEdit && existing) {
        await api.updateUser(existing.id, {
          full_name: fullName,
          email,
          role,
          team_id: teamId,
          telegram_chat_id: telegram,
          wa_number: waNumber,
        })
      } else {
        await api.createUser({
          username: username.trim(),
          password,
          role,
          full_name: fullName,
          email,
          team_id: teamId,
          telegram_chat_id: telegram,
          wa_number: waNumber,
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
      title={isEdit ? `Ubah User — ${existing?.username}` : 'Tambah User'}
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy}>
            {busy ? 'Menyimpan…' : isEdit ? 'Simpan Perubahan' : 'Buat User'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        {!isEdit && (
          <div>
            <label className="label-field" htmlFor="u-username">
              Username
            </label>
            <input
              id="u-username"
              className="input mono"
              required
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="noc.budi"
            />
            <p className="mt-1 text-label-sm text-text-secondary">3–64 karakter: huruf, angka, _ - .</p>
          </div>
        )}

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="u-fullname">
              Nama lengkap
            </label>
            <input
              id="u-fullname"
              className="input"
              value={fullName}
              onChange={(e) => setFullName(e.target.value)}
              placeholder="Budi Santoso"
            />
          </div>
          <div>
            <label className="label-field" htmlFor="u-email">
              Email
            </label>
            <input
              id="u-email"
              type="email"
              className="input"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="budi@contoh.id"
            />
          </div>
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="u-role">
              Role
            </label>
            <select id="u-role" className="input" value={role} onChange={(e) => setRole(e.target.value)}>
              {ROLE_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
            <p className="mt-1 text-label-sm text-text-secondary">
              {ROLE_OPTIONS.find((o) => o.value === role)?.note}
            </p>
          </div>
          <div>
            <label className="label-field" htmlFor="u-team">
              Tim
            </label>
            <select id="u-team" className="input" value={teamId} onChange={(e) => setTeamId(e.target.value)}>
              <option value="">— Tanpa tim —</option>
              {teams.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </div>
        </div>

        {!isEdit && (
          <div>
            <label className="label-field" htmlFor="u-password">
              Password awal
            </label>
            <input
              id="u-password"
              type="password"
              className="input"
              required
              minLength={8}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="minimal 8 karakter"
            />
          </div>
        )}

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="u-tg">
              Telegram chat ID
            </label>
            <input
              id="u-tg"
              className="input mono"
              value={telegram}
              onChange={(e) => setTelegram(e.target.value)}
              placeholder="opsional"
            />
          </div>
          <div>
            <label className="label-field" htmlFor="u-wa">
              Nomor WhatsApp
            </label>
            <input
              id="u-wa"
              className="input mono"
              value={waNumber}
              onChange={(e) => setWaNumber(e.target.value)}
              placeholder="62812xxxxxxx"
            />
          </div>
        </div>

        {isEdit && (
          <p className="text-label-sm text-text-secondary">
            Mengubah role atau menonaktifkan user akan mencabut seluruh sesi aktifnya.
          </p>
        )}
      </form>
    </Modal>
  )
}

function ResetPasswordForm({
  target,
  onClose,
  onSaved,
  onError,
}: {
  target: ApiUser
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await api.resetUserPassword(target.id, password)
      onSaved()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Terjadi kesalahan')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title={`Reset Password — ${target.username}`}
      width="sm"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || password.length < 8}>
            {busy ? 'Menyimpan…' : 'Reset Password'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3">
        <div>
          <label className="label-field" htmlFor="rp-password">
            Password baru
          </label>
          <input
            id="rp-password"
            type="password"
            className="input"
            required
            minLength={8}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="minimal 8 karakter"
          />
        </div>
        <div className="flex gap-2.5 rounded-control border border-outline-variant bg-warning-container/60 p-3 text-body-sm text-on-warning-container">
          <span className="material-symbols-outlined text-[18px] shrink-0">info</span>
          <span>Seluruh sesi user ini akan dicabut dan ia harus login ulang.</span>
        </div>
      </form>
    </Modal>
  )
}
