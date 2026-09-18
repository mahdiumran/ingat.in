import { useState } from 'react'
import { ApiUser, RolePermission, masterDataApi } from '../api'
import { ErrorState, LoadingBlock, PageHeader } from '../components/ui'
import { useAsync } from '../hooks'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

const ACTION_LABELS: Record<string, string> = {
  'items.write': 'Kelola item (todo/reminder/RFS)',
  'providers.write': 'Kelola provider notifikasi',
  'masterdata.write': 'Kelola master data',
  'users.write': 'Kelola pengguna & tim',
}

/**
 * RolePermissionsPage (F12) — halaman tersendiri untuk matriks izin per peran.
 *
 * Dipisah dari hub Master Data agar pengelolaan izin tidak bercampur dengan
 * data referensi. Admin selalu memiliki seluruh izin dan tidak dapat dicabut.
 */
export default function RolePermissionsPage({
  user,
  toast,
  onBack,
}: {
  user: ApiUser | null
  toast: Toast
  onBack: () => void
}) {
  const data = useAsync(() => masterDataApi.rolePermissions(), [])
  const [busyCell, setBusyCell] = useState('')

  const isAdmin = user?.role === 'admin'
  const roles = data.data?.roles ?? []
  const actions = data.data?.actions ?? []
  const perms = data.data?.permissions ?? []

  function allowed(role: string, action: string): boolean {
    if (role === 'admin') return true
    return perms.find((p: RolePermission) => p.role === role && p.action === action)?.allowed ?? false
  }

  async function toggle(role: string, action: string, next: boolean) {
    const cell = role + ':' + action
    setBusyCell(cell)
    try {
      await masterDataApi.setRolePermission(role, action, next)
      toast('success', 'Izin diperbarui', `${role} · ${action} → ${next ? 'diizinkan' : 'ditolak'}`)
      data.reload()
    } catch (err) {
      toast('error', 'Gagal mengubah izin', err instanceof Error ? err.message : undefined)
    } finally {
      setBusyCell('')
    }
  }

  return (
    <div>
      <PageHeader
        kicker="Master Data"
        title="Role Permissions"
        description="Matriks izin per peran. Admin selalu memiliki seluruh izin."
        actions={
          <>
            <button className="btn-ghost" onClick={onBack}>
              <span className="material-symbols-outlined text-[18px]">arrow_back</span>
              Kembali
            </button>
            <button className="btn-secondary" onClick={data.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
          </>
        }
      />

      <section className="card">
        <div className="border-b border-border px-5 py-3.5">
          <h3 className="font-headline text-lg font-semibold">Matriks Izin</h3>
          <p className="text-body-sm text-text-secondary">
            Klik sel untuk mengizinkan/mencabut. Hanya admin yang dapat mengubah.
          </p>
        </div>

        {data.loading && <LoadingBlock />}
        {data.error && (
          <div className="p-4">
            <ErrorState message={data.error} onRetry={data.reload} />
          </div>
        )}

        {!data.loading && !data.error && (
          <div className="overflow-x-auto">
            <table className="table">
              <thead>
                <tr>
                  <th>Peran</th>
                  {actions.map((a) => (
                    <th key={a} className="text-center">
                      {ACTION_LABELS[a] ?? a}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {roles.map((role) => (
                  <tr key={role}>
                    <td className="font-medium capitalize">{role}</td>
                    {actions.map((action) => {
                      const on = allowed(role, action)
                      const locked = role === 'admin' || !isAdmin
                      const cell = role + ':' + action
                      return (
                        <td key={action} className="text-center">
                          <button
                            disabled={locked || busyCell === cell}
                            onClick={() => toggle(role, action, !on)}
                            title={
                              role === 'admin'
                                ? 'Admin selalu diizinkan'
                                : !isAdmin
                                  ? 'Hanya admin yang dapat mengubah'
                                  : on
                                    ? 'Klik untuk mencabut'
                                    : 'Klik untuk memberi'
                            }
                            className={`inline-flex h-7 w-7 items-center justify-center rounded-full border transition ${
                              on
                                ? 'border-outline-variant bg-success-container text-on-success-container'
                                : 'border-outline-variant bg-surface-container text-text-secondary'
                            } ${locked ? 'cursor-not-allowed opacity-70' : 'hover:scale-105'}`}
                          >
                            <span className="material-symbols-outlined text-[18px]">
                              {busyCell === cell ? 'progress_activity' : on ? 'check' : 'close'}
                            </span>
                          </button>
                        </td>
                      )
                    })}
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
