import { useState } from 'react'
import { api, ApiUser, clearSession } from '../api'
import { ErrorState, LoadingBlock, PageHeader, formatWIB } from '../components/ui'
import { useAsync } from '../hooks'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

export default function Profile({
  user,
  onUserChange,
  toast,
}: {
  user: ApiUser | null
  onUserChange: (u: ApiUser | null) => void
  toast: Toast
}) {
  const sessions = useAsync(() => api.sessions(), [])

  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function changePassword(e: React.FormEvent) {
    e.preventDefault()
    setError('')

    if (next !== confirm) {
      setError('Konfirmasi password tidak cocok.')
      return
    }
    if (next.length < 8) {
      setError('Password baru minimal 8 karakter.')
      return
    }

    setBusy(true)
    try {
      const res = await api.changePassword(current, next)
      toast('success', 'Password diganti', res.message)
      setCurrent('')
      setNext('')
      setConfirm('')
      // Seluruh sesi dicabut oleh server — pengguna harus login ulang.
      clearSession()
      onUserChange(null)
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Gagal mengganti password'
      setError(msg)
    } finally {
      setBusy(false)
    }
  }

  async function logoutAll() {
    if (!window.confirm('Cabut seluruh sesi Anda? Anda harus login ulang di semua perangkat.')) return
    try {
      await api.logoutAll()
      clearSession()
      onUserChange(null)
    } catch (err) {
      toast('error', 'Gagal mencabut sesi', err instanceof Error ? err.message : undefined)
    }
  }

  return (
    <div>
      <PageHeader
        kicker="Manajemen"
        title="Profil Saya"
        description="Informasi akun, keamanan, dan sesi aktif."
      />

      <div className="grid gap-5 lg:grid-cols-2">
        {/* Info akun */}
        <section className="card card-pad">
          <h3 className="mb-4 font-headline text-lg font-semibold">Informasi Akun</h3>
          <dl className="space-y-3">
            <Field label="Username" value={<span className="mono">{user?.username ?? '—'}</span>} />
            <Field label="Nama lengkap" value={user?.full_name || '—'} />
            <Field label="Email" value={user?.email || '—'} />
            <Field
              label="Role"
              value={<span className="badge bg-primary-container text-on-primary-container border-outline-variant">{user?.role ?? '—'}</span>}
            />
            <Field
              label="Terdaftar"
              value={<span className="mono text-label-md">{formatWIB(user?.created_at)}</span>}
            />
          </dl>

          <div className="mt-5 rounded-control border border-outline-variant bg-info-container/50 p-3.5 text-body-sm text-on-info-container">
            <div className="mb-1 flex items-center gap-1.5 font-medium">
              <span className="material-symbols-outlined text-[17px]">shield</span>
              Masa berlaku sesi
            </div>
            Access token berlaku <strong>72 jam</strong> dan diperbarui otomatis menggunakan refresh token
            (30 hari). Ganti password atau “Cabut semua sesi” akan langsung membatalkan seluruh akses.
          </div>
        </section>

        {/* Ganti password */}
        <section className="card card-pad">
          <h3 className="mb-1 font-headline text-lg font-semibold">Ganti Password</h3>
          <p className="mb-4 text-body-sm text-text-secondary">
            Setelah password diganti, seluruh sesi Anda dicabut dan Anda harus login ulang.
          </p>

          <form onSubmit={changePassword} className="space-y-3.5">
            <div>
              <label className="label-field" htmlFor="p-current">
                Password saat ini
              </label>
              <input
                id="p-current"
                type="password"
                className="input"
                required
                autoComplete="current-password"
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
              />
            </div>
            <div>
              <label className="label-field" htmlFor="p-next">
                Password baru
              </label>
              <input
                id="p-next"
                type="password"
                className="input"
                required
                minLength={8}
                autoComplete="new-password"
                value={next}
                onChange={(e) => setNext(e.target.value)}
                placeholder="minimal 8 karakter"
              />
            </div>
            <div>
              <label className="label-field" htmlFor="p-confirm">
                Konfirmasi password baru
              </label>
              <input
                id="p-confirm"
                type="password"
                className="input"
                required
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
              />
              {confirm && next !== confirm && (
                <p className="mt-1 text-label-sm text-critical">Konfirmasi tidak cocok.</p>
              )}
            </div>

            {error && (
              <div role="alert" className="flex gap-2 rounded-control border border-critical/30 bg-critical-container/60 p-3 text-body-sm text-on-critical-container">
                <span className="material-symbols-outlined text-[18px] shrink-0">error</span>
                <span>{error}</span>
              </div>
            )}

            <button
              type="submit"
              className="btn-primary w-full"
              disabled={busy || !current || next.length < 8 || next !== confirm}
            >
              {busy ? 'Menyimpan…' : 'Ganti Password'}
            </button>
          </form>
        </section>

        {/* Sesi aktif */}
        <section className="card lg:col-span-2">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3.5">
            <div>
              <h3 className="font-headline text-lg font-semibold">Sesi Aktif</h3>
              <p className="text-body-sm text-text-secondary">
                Perangkat yang sedang memiliki akses ke akun Anda.
              </p>
            </div>
            <div className="flex gap-2">
              <button className="btn-secondary" onClick={sessions.reload}>
                <span className="material-symbols-outlined text-[17px]">refresh</span>
                Muat ulang
              </button>
              <button className="btn-danger" onClick={logoutAll}>
                <span className="material-symbols-outlined text-[17px]">logout</span>
                Cabut semua sesi
              </button>
            </div>
          </div>

          {sessions.loading && <LoadingBlock />}
          {sessions.error && (
            <div className="p-4">
              <ErrorState message={sessions.error} onRetry={sessions.reload} />
            </div>
          )}
          {!sessions.loading && !sessions.error && (
            <div className="overflow-x-auto">
              <table className="table">
                <thead>
                  <tr>
                    <th>Dibuat (WIB)</th>
                    <th>User agent</th>
                    <th>IP</th>
                    <th>Kedaluwarsa (WIB)</th>
                  </tr>
                </thead>
                <tbody>
                  {(sessions.data?.sessions ?? []).map((s) => (
                    <tr key={s.id}>
                      <td className="mono whitespace-nowrap text-label-md">{formatWIB(s.created_at, true)}</td>
                      <td className="max-w-[380px] truncate text-text-secondary" title={s.user_agent}>
                        {s.user_agent || '—'}
                      </td>
                      <td className="mono text-label-sm">{s.ip || '—'}</td>
                      <td className="mono whitespace-nowrap text-label-md">{formatWIB(s.expires_at)}</td>
                    </tr>
                  ))}
                  {(sessions.data?.sessions ?? []).length === 0 && (
                    <tr>
                      <td colSpan={4} className="py-6 text-center text-text-secondary">
                        Tidak ada sesi aktif.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
    </div>
  )
}

function Field({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 border-b border-border/70 pb-2.5 last:border-0">
      <dt className="text-body-sm text-text-secondary">{label}</dt>
      <dd className="text-right text-body-sm font-medium">{value}</dd>
    </div>
  )
}
