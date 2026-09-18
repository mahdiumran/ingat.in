import { ApiUser, MasterDataKind, masterDataApi } from '../api'
import { ErrorState, LoadingBlock, PageHeader } from '../components/ui'
import { useAsync } from '../hooks'

/**
 * MasterData (F12) — hub Master Data.
 *
 * Menampilkan setiap kelompok (kind) sebagai KOTAK pada grid. Mengklik kotak
 * membuka halaman terpisah khusus kelompok tersebut (lihat MasterDataKindPage)
 * sehingga pengelolaan tiap jenis data tidak saling bercampur.
 *
 * Hub juga menampilkan kotak "Role Permissions" untuk matriks izin.
 */
export default function MasterData({
  user,
  onOpenKind,
  onOpenRoles,
  onOpenTemplates,
}: {
  user: ApiUser | null
  onOpenKind: (kind: string) => void
  onOpenRoles: () => void
  onOpenTemplates: () => void
}) {
  const data = useAsync(() => masterDataApi.list(), [])
  const roles = useAsync(() => masterDataApi.rolePermissions(), [])

  const kinds = data.data?.kinds ?? []
  const entries = data.data?.entries ?? []
  const isAdmin = user?.role === 'admin'

  function countOf(kind: string): number {
    return entries.filter((e) => e.kind === kind).length
  }

  return (
    <div>
      <PageHeader
        kicker="Manajemen"
        title="Master Data"
        description="Pilih kelompok data untuk dikelola. Setiap kelompok punya halaman tersendiri."
        actions={
          <button className="btn-secondary" onClick={data.reload}>
            <span className="material-symbols-outlined text-[18px]">refresh</span>
            Muat ulang
          </button>
        }
      />

      {data.loading && <LoadingBlock />}
      {data.error && <ErrorState message={data.error} onRetry={data.reload} />}

      {!data.loading && !data.error && (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {kinds.map((k) => (
            <MasterDataCard
              key={k.kind}
              kind={k}
              count={countOf(k.kind)}
              onClick={() => onOpenKind(k.kind)}
            />
          ))}

          {/* Kotak editor notifikasi Telegram & WA (admin only). */}
          {isAdmin && (
            <button
              onClick={onOpenTemplates}
              className="card group relative flex flex-col items-start gap-3 p-5 text-left transition hover:border-primary hover:shadow-raised"
            >
              <span className="flex h-11 w-11 items-center justify-center rounded-control border border-accent-soft bg-success-container/40 text-primary">
                <span className="material-symbols-outlined text-[24px]">notifications_active</span>
              </span>
              <div className="min-w-0">
                <div className="font-headline text-body-lg font-semibold">Notifikasi Telegram &amp; WA</div>
                <p className="mt-0.5 text-body-sm text-text-secondary">
                  Sunting isi pesan notifikasi per kanal.
                </p>
              </div>
              <span className="badge border-outline-variant bg-surface-container text-text-secondary">
                admin
              </span>
            </button>
          )}

          {/* Kotak Role Permissions (hanya admin yang dapat mengubah). */}
          {isAdmin && (
            <button
              onClick={onOpenRoles}
              className="card group relative flex flex-col items-start gap-3 p-5 text-left transition hover:border-primary hover:shadow-raised"
            >
              <span className="flex h-11 w-11 items-center justify-center rounded-control border border-accent-soft bg-info-container/40 text-info">
                <span className="material-symbols-outlined text-[24px]">admin_panel_settings</span>
              </span>
              <div className="min-w-0">
                <div className="font-headline text-body-lg font-semibold">Role Permissions</div>
                <p className="mt-0.5 text-body-sm text-text-secondary">
                  Matriks izin per peran pengguna.
                </p>
              </div>
              <span className="badge border-outline-variant bg-surface-container text-text-secondary">
                {roles.data?.permissions?.length ?? 0} izin
              </span>
            </button>
          )}
        </div>
      )}
    </div>
  )
}

/* ------------------------------------------------------------------------- */

/** MasterDataCard adalah satu kotak kelompok master data pada hub. */
export function MasterDataCard({
  kind,
  count,
  onClick,
}: {
  kind: MasterDataKind
  count: number
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      title={kind.description}
      className="card group relative flex flex-col items-start gap-3 p-5 text-left transition hover:border-primary hover:shadow-raised"
    >
      <span className="flex h-11 w-11 items-center justify-center rounded-control border border-accent-soft bg-success-container/40 text-primary">
        <span className="material-symbols-outlined text-[24px]">{kind.icon || 'category'}</span>
      </span>
      <div className="min-w-0">
        <div className="font-headline text-body-lg font-semibold">{kind.label}</div>
        <p className="mt-0.5 line-clamp-2 text-body-sm text-text-secondary">
          {kind.description || 'Data referensi.'}
        </p>
      </div>
      <div className="flex items-center gap-2">
        <span className="badge border-outline-variant bg-surface-container text-text-secondary">
          {count} entri
        </span>
        {kind.is_system && (
          <span className="badge border-outline-variant bg-info-container/40 text-on-info-container">
            sistem
          </span>
        )}
      </div>
      <span className="material-symbols-outlined absolute right-4 top-4 text-[20px] text-text-secondary transition group-hover:text-primary">
        chevron_right
      </span>
    </button>
  )
}
