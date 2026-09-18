import { useState } from 'react'
import { api, ApiUser } from '../api'
import { EmptyState, ErrorState, LoadingBlock, PageHeader, formatWIB } from '../components/ui'
import { useAsync } from '../hooks'

const ACTION_TONE: Record<string, string> = {
  'auth.login': 'bg-success-container text-on-success-container border-outline-variant',
  'auth.logout': 'bg-surface-container text-text-secondary border-outline-variant',
  'auth.refresh_reuse': 'bg-critical-container text-on-critical-container border-critical',
  'auth.change_password': 'bg-warning-container text-on-warning-container border-outline-variant',
}

function actionTone(action: string): string {
  if (action.endsWith('.delete')) return 'bg-critical-container text-on-critical-container border-critical'
  if (action.endsWith('.create')) return 'bg-primary-container text-on-primary-container border-outline-variant'
  if (action.endsWith('.update') || action.endsWith('.reset_password'))
    return 'bg-info-container text-on-info-container border-outline-variant'
  return ACTION_TONE[action] ?? 'bg-surface-container text-text-secondary border-outline-variant'
}

export default function Audit({ user }: { user: ApiUser | null }) {
  const [username, setUsername] = useState('')
  const [action, setAction] = useState('')
  const [successFilter, setSuccessFilter] = useState('')
  const [limit, setLimit] = useState(50)
  const [offset, setOffset] = useState(0)

  const logs = useAsync(
    () =>
      api.audit({
        ...(username ? { username } : {}),
        ...(action ? { action } : {}),
        ...(successFilter ? { success: successFilter } : {}),
        limit: String(limit),
        offset: String(offset),
      }),
    [username, action, successFilter, limit, offset],
  )

  if (user?.role !== 'admin') {
    return (
      <ErrorState message="Audit trail hanya dapat diakses oleh administrator." />
    )
  }

  const rows = logs.data?.logs ?? []
  const total = logs.data?.total ?? 0
  const page = Math.floor(offset / limit) + 1
  const maxPage = Math.max(1, Math.ceil(total / limit))

  return (
    <div>
      <PageHeader
        kicker="Manajemen"
        title="Audit Trail"
        description="Riwayat seluruh aksi penting: login, perubahan user, dan operasi work item."
        actions={
          <button className="btn-secondary" onClick={logs.reload}>
            <span className="material-symbols-outlined text-[18px]">refresh</span>
            Muat ulang
          </button>
        }
      />

      {/* Filter */}
      <section className="card mb-5 p-4">
        <div className="grid gap-3 sm:grid-cols-4">
          <div>
            <label className="label-field" htmlFor="a-username">
              Username
            </label>
            <input
              id="a-username"
              className="input mono"
              value={username}
              placeholder="semua"
              onChange={(e) => {
                setUsername(e.target.value)
                setOffset(0)
              }}
            />
          </div>
          <div>
            <label className="label-field" htmlFor="a-action">
              Aksi
            </label>
            <input
              id="a-action"
              className="input mono"
              value={action}
              placeholder="mis. auth.login"
              onChange={(e) => {
                setAction(e.target.value)
                setOffset(0)
              }}
            />
          </div>
          <div>
            <label className="label-field" htmlFor="a-success">
              Hasil
            </label>
            <select
              id="a-success"
              className="input"
              value={successFilter}
              onChange={(e) => {
                setSuccessFilter(e.target.value)
                setOffset(0)
              }}
            >
              <option value="">Semua</option>
              <option value="true">Berhasil</option>
              <option value="false">Gagal</option>
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="a-limit">
              Baris per halaman
            </label>
            <select
              id="a-limit"
              className="input"
              value={limit}
              onChange={(e) => {
                setLimit(Number(e.target.value))
                setOffset(0)
              }}
            >
              {[25, 50, 100, 200].map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </div>
        </div>
      </section>

      <section className="card">
        <div className="flex items-center justify-between gap-3 border-b border-border px-5 py-3.5">
          <div>
            <h3 className="font-headline text-lg font-semibold">Log Aktivitas</h3>
            <p className="text-body-sm text-text-secondary">
              {total} catatan · halaman {page} dari {maxPage}
            </p>
          </div>
          <div className="flex gap-2">
            <button
              className="btn-secondary"
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - limit))}
            >
              Sebelumnya
            </button>
            <button
              className="btn-secondary"
              disabled={offset + limit >= total}
              onClick={() => setOffset(offset + limit)}
            >
              Berikutnya
            </button>
          </div>
        </div>

        {logs.loading && <LoadingBlock />}
        {logs.error && (
          <div className="p-4">
            <ErrorState message={logs.error} onRetry={logs.reload} />
          </div>
        )}
        {!logs.loading && !logs.error && rows.length === 0 && (
          <EmptyState
            icon="history"
            title="Belum ada catatan audit"
            body="Aktivitas akan tercatat di sini setelah pengguna mulai menggunakan panel."
          />
        )}

        {!logs.loading && !logs.error && rows.length > 0 && (
          <div className="overflow-x-auto">
            <table className="table">
              <thead>
                <tr>
                  <th>Waktu (WIB)</th>
                  <th>User</th>
                  <th>Aksi</th>
                  <th>Entitas</th>
                  <th>IP</th>
                  <th>Hasil</th>
                  <th>Detail</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((l) => (
                  <tr key={l.id}>
                    <td className="mono whitespace-nowrap text-label-md">{formatWIB(l.created_at, true)}</td>
                    <td className="mono text-label-md">{l.username || '—'}</td>
                    <td>
                      <span className={`badge ${actionTone(l.action)}`}>{l.action}</span>
                    </td>
                    <td className="text-text-secondary">
                      {l.entity_type ? (
                        <span className="mono text-label-sm">
                          {l.entity_type}
                          {l.entity_id ? `:${l.entity_id.slice(0, 8)}` : ''}
                        </span>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td className="mono text-label-sm text-text-secondary">{l.ip || '—'}</td>
                    <td>
                      <span
                        className={`badge ${
                          l.success
                            ? 'bg-success-container text-on-success-container border-outline-variant'
                            : 'bg-critical-container text-on-critical-container border-critical'
                        }`}
                      >
                        {l.success ? 'berhasil' : 'gagal'}
                      </span>
                    </td>
                    <td className="max-w-[260px]">
                      {l.detail && Object.keys(l.detail).length > 0 ? (
                        <span className="mono block truncate text-label-sm text-text-secondary" title={JSON.stringify(l.detail)}>
                          {JSON.stringify(l.detail)}
                        </span>
                      ) : (
                        <span className="text-text-secondary">—</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  )
}
