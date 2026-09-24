import { useState } from 'react'
import { ApiUser, RoleDef, RolePermission, masterDataApi, rolesApi } from '../api'
import { ConfirmDialog, ErrorState, LoadingBlock, Modal, PageHeader } from '../components/ui'
import { useAsync } from '../hooks'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

const ACTION_LABELS: Record<string, string> = {
  'items.write': 'Kelola item (todo/reminder/RFS/tiket)',
  'providers.view': 'Lihat Notification Center',
  'providers.write': 'Kelola provider notifikasi',
  'masterdata.write': 'Kelola master data',
  'users.write': 'Kelola pengguna',
  'teams.write': 'Kelola tim & kategori',
  'roles.write': 'Kelola peran & izin',
  'kpi.view': 'Lihat KPI & SLA (manajemen)',
}

const ACTION_HELP: Record<string, string> = {
  'items.write': 'Membuat/mengubah/menghapus work item dan mengubah status.',
  'providers.view':
    'Melihat Notification Center: Providers, Notification Targets, dan Template notifikasi (Escalation Policies tetap terbuka untuk semua).',
  'providers.write': 'Menambah/mengubah provider notifikasi (WhatsApp/Telegram, dll).',
  'masterdata.write': 'Menambah/mengubah entri dan kelompok master data.',
  'users.write': 'Menambah/mengubah/menghapus pengguna panel.',
  'teams.write': 'Menambah/mengubah/menghapus tim beserta kategorinya.',
  'roles.write': 'Menambah peran baru dan mengubah matriks izin.',
  'kpi.view': 'Mengakses halaman KPI & SLA (khusus manajemen).',
}

/**
 * RolePermissionsPage (F12/F22) — kelola peran dinamis + matriks izin detail.
 *
 * Dua bagian: (1) daftar peran (tambah/ubah/hapus), (2) matriks izin per peran.
 * Super user (mis. admin) selalu memiliki seluruh izin dan tidak dapat dicabut.
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
  const roleList = useAsync(() => rolesApi.list(), [])
  const [busyCell, setBusyCell] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [editTarget, setEditTarget] = useState<RoleDef | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<RoleDef | null>(null)

  const isAdmin = user?.role === 'admin' || user?.is_super
  const canWrite = isAdmin || (user?.permissions ?? []).includes('roles.write')
  const roles = data.data?.roles ?? []
  const actions = data.data?.actions ?? []
  const perms = data.data?.permissions ?? []
  const superMap = data.data?.role_is_super ?? {}
  const labelMap = data.data?.role_labels ?? {}

  function reloadAll() {
    data.reload()
    roleList.reload()
  }

  function allowed(role: string, action: string): boolean {
    if (superMap[role]) return true
    return perms.find((p: RolePermission) => p.role === role && p.action === action)?.allowed ?? false
  }

  async function toggle(role: string, action: string, next: boolean) {
    const cell = role + ':' + action
    setBusyCell(cell)
    try {
      await masterDataApi.setRolePermission(role, action, next)
      toast('success', 'Izin diperbarui', `${labelMap[role] ?? role} · ${ACTION_LABELS[action] ?? action} → ${next ? 'diizinkan' : 'ditolak'}`)
      data.reload()
    } catch (err) {
      toast('error', 'Gagal mengubah izin', err instanceof Error ? err.message : undefined)
    } finally {
      setBusyCell('')
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return
    try {
      await rolesApi.remove(deleteTarget.role)
      toast('success', 'Peran dihapus', deleteTarget.label)
      setDeleteTarget(null)
      reloadAll()
    } catch (err) {
      toast('error', 'Gagal menghapus peran', err instanceof Error ? err.message : undefined)
      setDeleteTarget(null)
    }
  }

  return (
    <div>
      <PageHeader
        kicker="Manajemen"
        title="Peran & Izin"
        description="Kelola peran (tambah/ubah/hapus) dan matriks izin detail. Super user memiliki seluruh izin."
        actions={
          <>
            <button className="btn-ghost" onClick={onBack}>
              <span className="material-symbols-outlined text-[18px]">arrow_back</span>
              Kembali
            </button>
            <button className="btn-secondary" onClick={reloadAll}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            {canWrite && (
              <button className="btn-primary" onClick={() => setShowCreate(true)}>
                <span className="material-symbols-outlined text-[18px]">add</span>
                Tambah Peran
              </button>
            )}
          </>
        }
      />

      {/* Daftar peran */}
      <section className="card mb-5">
        <div className="border-b border-border px-5 py-3.5">
          <h3 className="font-headline text-lg font-semibold">Daftar Peran</h3>
          <p className="text-body-sm text-text-secondary">
            Peran bawaan (is_system) dan super user tidak dapat dihapus. Peran baru dapat ditambah sesuai kebutuhan.
          </p>
        </div>
        {roleList.loading && <LoadingBlock />}
        {roleList.error && (
          <div className="p-4">
            <ErrorState message={roleList.error} onRetry={roleList.reload} />
          </div>
        )}
        {!roleList.loading && !roleList.error && (
          <div className="overflow-x-auto">
            <table className="table">
              <thead>
                <tr>
                  <th>Peran</th>
                  <th>Slug</th>
                  <th>Deskripsi</th>
                  <th className="text-center">Urutan</th>
                  <th className="text-center">Tipe</th>
                  <th className="text-right">Aksi</th>
                </tr>
              </thead>
              <tbody>
                {(roleList.data?.roles ?? []).map((r) => (
                  <tr key={r.role}>
                    <td className="font-medium">{r.label}</td>
                    <td className="mono text-label-md text-text-secondary">{r.role}</td>
                    <td className="text-text-secondary">{r.description || '—'}</td>
                    <td className="text-center mono">{r.rank}</td>
                    <td className="text-center">
                      {r.is_super ? (
                        <span className="badge bg-critical-container text-on-critical-container border-critical">super</span>
                      ) : r.is_system ? (
                        <span className="badge bg-surface-container text-text-secondary border-outline-variant">bawaan</span>
                      ) : (
                        <span className="badge bg-info-container text-on-info-container border-outline-variant">kustom</span>
                      )}
                    </td>
                    <td>
                      <div className="flex justify-end gap-1">
                        <button
                          className="btn-ghost h-8 w-8 px-0"
                          title={canWrite ? 'Ubah peran' : 'Hanya pengelola izin'}
                          disabled={!canWrite}
                          onClick={() => setEditTarget(r)}
                        >
                          <span className="material-symbols-outlined text-[18px]">edit</span>
                        </button>
                        <button
                          className="btn-ghost h-8 w-8 px-0 text-critical"
                          title={r.is_system || r.is_super ? 'Peran bawaan tidak dapat dihapus' : 'Hapus peran'}
                          disabled={!canWrite || r.is_system || r.is_super}
                          onClick={() => setDeleteTarget(r)}
                        >
                          <span className="material-symbols-outlined text-[18px]">delete</span>
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Matriks izin */}
      <section className="card">
        <div className="border-b border-border px-5 py-3.5">
          <h3 className="font-headline text-lg font-semibold">Matriks Izin</h3>
          <p className="text-body-sm text-text-secondary">
            Klik sel untuk mengizinkan/mencabut. Hanya pengelola izin yang dapat mengubah.
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
                  <th className="sticky left-0 bg-surface-container-lowest">Peran</th>
                  {actions.map((a) => (
                    <th key={a} className="text-center" title={ACTION_HELP[a] ?? a}>
                      {ACTION_LABELS[a] ?? a}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {roles.map((role) => {
                  const isSuper = superMap[role]
                  return (
                    <tr key={role}>
                      <td className="sticky left-0 bg-surface-container-lowest font-medium">
                        {labelMap[role] ?? role}
                        {isSuper && <span className="ml-1.5 badge bg-critical-container text-on-critical-container border-critical">super</span>}
                      </td>
                      {actions.map((action) => {
                        const on = allowed(role, action)
                        const locked = isSuper || !canWrite
                        const cell = role + ':' + action
                        return (
                          <td key={action} className="text-center">
                            <button
                              disabled={locked || busyCell === cell}
                              onClick={() => toggle(role, action, !on)}
                              title={
                                isSuper
                                  ? 'Super user selalu diizinkan'
                                  : !canWrite
                                    ? 'Hanya pengelola izin yang dapat mengubah'
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
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {showCreate && (
        <RoleForm
          onClose={() => setShowCreate(false)}
          onSaved={() => {
            setShowCreate(false)
            toast('success', 'Peran dibuat')
            reloadAll()
          }}
          onError={(msg) => toast('error', 'Gagal membuat peran', msg)}
        />
      )}

      {editTarget && (
        <RoleForm
          existing={editTarget}
          onClose={() => setEditTarget(null)}
          onSaved={() => {
            setEditTarget(null)
            toast('success', 'Peran diperbarui')
            reloadAll()
          }}
          onError={(msg) => toast('error', 'Gagal memperbarui peran', msg)}
        />
      )}

      <ConfirmDialog
        open={!!deleteTarget}
        title="Hapus peran?"
        body={deleteTarget ? `Peran "${deleteTarget.label}" akan dihapus permanen. Pastikan tidak ada pengguna yang memakainya.` : ''}
        confirmLabel="Hapus"
        tone="danger"
        onCancel={() => setDeleteTarget(null)}
        onConfirm={confirmDelete}
      />
    </div>
  )
}

function RoleForm({
  existing,
  onClose,
  onSaved,
  onError,
}: {
  existing?: RoleDef
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const isEdit = !!existing
  const [role, setRole] = useState(existing?.role ?? '')
  const [label, setLabel] = useState(existing?.label ?? '')
  const [description, setDescription] = useState(existing?.description ?? '')
  const [rank, setRank] = useState(String(existing?.rank ?? 100))
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const rankNum = parseInt(rank, 10)
      if (isEdit && existing) {
        await rolesApi.update(existing.role, {
          label,
          description,
          rank: Number.isFinite(rankNum) ? rankNum : undefined,
        })
      } else {
        await rolesApi.create({
          role: role.trim(),
          label: label.trim(),
          description,
          rank: Number.isFinite(rankNum) ? rankNum : 100,
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
      title={isEdit ? `Ubah Peran — ${existing?.label}` : 'Tambah Peran'}
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy}>
            {busy ? 'Menyimpan…' : isEdit ? 'Simpan Perubahan' : 'Buat Peran'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        {!isEdit && (
          <div>
            <label className="label-field" htmlFor="r-slug">
              Slug peran
            </label>
            <input
              id="r-slug"
              className="input mono"
              required
              value={role}
              onChange={(e) => setRole(e.target.value)}
              placeholder="mis. hr_ops"
            />
            <p className="mt-1 text-label-sm text-text-secondary">2–40 karakter: huruf kecil, angka, underscore.</p>
          </div>
        )}
        <div>
          <label className="label-field" htmlFor="r-label">
            Nama tampilan
          </label>
          <input
            id="r-label"
            className="input"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="mis. HR & Operasional"
          />
        </div>
        <div>
          <label className="label-field" htmlFor="r-desc">
            Deskripsi
          </label>
          <input
            id="r-desc"
            className="input"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Ringkasan tanggung jawab peran"
          />
        </div>
        <div>
          <label className="label-field" htmlFor="r-rank">
            Urutan (rank)
          </label>
          <input
            id="r-rank"
            type="number"
            className="input mono"
            value={rank}
            onChange={(e) => setRank(e.target.value)}
          />
          <p className="mt-1 text-label-sm text-text-secondary">Angka lebih kecil tampil lebih atas.</p>
        </div>
      </form>
    </Modal>
  )
}
