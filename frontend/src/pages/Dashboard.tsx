import { ApiUser, workItemsApi } from '../api'
import { ErrorState, LoadingBlock, PageHeader, PriorityBadge, StatCard, StatusBadge, formatWIB } from '../components/ui'
import { useAsync } from '../hooks'
import { statusTone } from '../design/tokens'
import type { View } from '../App'

type HealthState = 'loading' | 'online' | 'degraded' | 'offline'

/**
 * Dashboard (F8) — ringkasan operasional nyata dari GET /api/dashboard.
 *
 * Kartu utama: todo terbuka, lewat tenggat, reminder akan habis (24 jam),
 * RFS 7 hari ke depan, dan notifikasi gagal. Panel "Perlu Perhatian"
 * menampilkan item terdekat supaya NOC tahu apa yang harus dikerjakan.
 */
export default function Dashboard({
  user,
  schemaVersion,
  appVersion,
  health,
  onNavigate,
}: {
  user: ApiUser | null
  schemaVersion: number | null
  appVersion: string
  health: HealthState
  onNavigate: (v: View) => void
}) {
  const data = useAsync(() => workItemsApi.dashboard(), [])

  const healthLabel: Record<HealthState, string> = {
    loading: 'Memeriksa…',
    online: 'Online',
    degraded: 'Bermasalah',
    offline: 'Offline',
  }
  const healthTone: Record<HealthState, string> = {
    loading: 'bg-surface-container text-text-secondary border-outline-variant',
    online: 'bg-success-container text-on-success-container border-outline-variant',
    degraded: 'bg-warning-container text-on-warning-container border-outline-variant',
    offline: 'bg-critical-container text-on-critical-container border-critical',
  }

  const d = data.data

  return (
    <div className="space-y-5">
      <PageHeader
        kicker="Ringkasan"
        title={`Selamat datang${user ? `, ${user.full_name || user.username}` : ''}`}
        description="Status operasional reminder, RFS, dan notifikasi."
        actions={
          <button className="btn-secondary" onClick={data.reload}>
            <span className="material-symbols-outlined text-[18px]">refresh</span>
            Muat ulang
          </button>
        }
      />

      {data.loading && <LoadingBlock label="Memuat ringkasan…" />}
      {data.error && <ErrorState message={data.error} onRetry={data.reload} />}

      {d && (
        <>
          {/* Kartu utama */}
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
            <StatCard
              icon="task_alt"
              label="Todo terbuka"
              value={d.by_type_status?.task
                ? Object.entries(d.by_type_status.task)
                    .filter(([s]) => !['done', 'cancelled'].includes(s))
                    .reduce((n, [, v]) => n + v, 0)
                : 0}
              note="Task belum selesai"
              toneClass="bg-info-container text-on-info-container border-outline-variant"
            />
            <StatCard
              icon="schedule"
              label="Lewat tenggat"
              value={d.overdue_total}
              note={`${d.due_today_total} jatuh tempo hari ini`}
              toneClass={
                d.overdue_total > 0
                  ? 'bg-critical-container text-on-critical-container border-critical'
                  : 'bg-success-container text-on-success-container border-outline-variant'
              }
            />
            <StatCard
              icon="alarm"
              label="Reminder ≤24 jam"
              value={d.expiring_soon}
              note={`${d.expired_total} sudah expire`}
              toneClass={
                d.expiring_soon > 0
                  ? 'bg-warning-container text-on-warning-container border-outline-variant'
                  : 'bg-success-container text-on-success-container border-outline-variant'
              }
            />
            <StatCard
              icon="event_available"
              label="RFS 7 hari"
              value={d.rfs_upcoming}
              note="Perlu persiapan NOC"
              toneClass="bg-warning-container text-on-warning-container border-outline-variant"
            />
            <StatCard
              icon="notifications_off"
              label="Notif gagal 24 jam"
              value={d.notif_failed_24h}
              note={`${d.notif_pending} menunggu`}
              toneClass={
                d.notif_failed_24h > 0
                  ? 'bg-critical-container text-on-critical-container border-critical'
                  : 'bg-success-container text-on-success-container border-outline-variant'
              }
            />
          </div>

          {/* Perlu perhatian */}
          <section className="card">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3.5">
              <div>
                <h3 className="font-headline text-lg font-semibold">Perlu Perhatian</h3>
                <p className="text-body-sm text-text-secondary">
                  Item terdekat berdasarkan tenggat atau waktu expire.
                </p>
              </div>
              <button className="btn-secondary" onClick={() => onNavigate('reminders')}>
                Lihat semua
                <span className="material-symbols-outlined text-[17px]">arrow_forward</span>
              </button>
            </div>

            {d.upcoming.length === 0 ? (
              <div className="flex flex-col items-center gap-2 py-10 text-center">
                <span className="material-symbols-outlined text-[30px] text-success">check_circle</span>
                <div className="font-medium">Tidak ada item mendesak</div>
                <p className="text-body-sm text-text-secondary">
                  Tidak ada todo, reminder, atau RFS yang jatuh tempo dalam waktu dekat.
                </p>
              </div>
            ) : (
              <div className="divide-y divide-border">
                {d.upcoming.map((it) => {
                  const deadline = it.expire_at ?? it.due_at
                  return (
                    <button
                      key={it.id}
                      className="flex w-full items-center gap-3 px-5 py-3 text-left hover:bg-surface-container-low/70"
                      onClick={() => onNavigate(it.item_type === 'rfs' ? 'rfs' : it.item_type === 'reminder' ? 'reminders' : 'todos')}
                    >
                      <span
                        className={`material-symbols-outlined text-[20px] shrink-0 ${
                          it.item_type === 'rfs' ? 'text-primary' : it.item_type === 'reminder' ? 'text-warning' : 'text-info'
                        }`}
                      >
                        {it.item_type === 'rfs' ? 'event_available' : it.item_type === 'reminder' ? 'alarm' : 'task_alt'}
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="mono text-label-md text-text-secondary">{it.ref_no}</span>
                          <StatusBadge status={it.status} tone={statusTone[it.status]} />
                          <PriorityBadge priority={it.priority} />
                        </div>
                        <div className="mt-0.5 truncate font-medium">
                          {it.rfs?.customer_name || it.reminder?.subject_name || it.title}
                        </div>
                      </div>
                      <div className="shrink-0 text-right">
                        <div className="mono text-label-md">{formatWIB(deadline)}</div>
                        <div className="text-label-sm text-text-secondary">
                          {it.rfs?.bandwidth || it.owner_username || '—'}
                        </div>
                      </div>
                    </button>
                  )
                })}
              </div>
            )}
          </section>

          {/* Distribusi per tipe */}
          <section className="card card-pad">
            <h3 className="mb-3 font-headline text-lg font-semibold">Distribusi Work Item</h3>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {(['task', 'reminder', 'rfs'] as const).map((type) => {
                const statuses = d.by_type_status?.[type] ?? {}
                const counts = Object.entries(statuses)
                const totalType = counts.reduce((n, [, v]) => n + v, 0)
                const label = type === 'task' ? 'Todo Task' : type === 'reminder' ? 'Reminder' : 'RFS'
                const icon = type === 'task' ? 'task_alt' : type === 'reminder' ? 'alarm' : 'event_available'
                return (
                  <div key={type} className="rounded-card border border-border p-3.5">
                    <div className="mb-2 flex items-center gap-2">
                      <span className="material-symbols-outlined text-[19px] text-text-secondary">{icon}</span>
                      <span className="font-medium">{label}</span>
                      <span className="mono ml-auto text-label-md text-text-secondary">{totalType}</span>
                    </div>
                    {counts.length === 0 ? (
                      <p className="text-body-sm text-text-secondary">Belum ada item.</p>
                    ) : (
                      <div className="flex flex-wrap gap-1.5">
                        {counts.map(([status, n]) => (
                          <span key={status} className={`badge ${statusTone[status] ?? ''}`}>
                            {status.replace(/_/g, ' ')} {n}
                          </span>
                        ))}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          </section>

          {/* Status sistem */}
          <section className="card card-pad">
            <h3 className="mb-3 font-headline text-lg font-semibold">Status Sistem</h3>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <SystemField label="Backend" value={healthLabel[health]} tone={healthTone[health]} />
              <SystemField
                label="Versi skema"
                value={schemaVersion === null ? '—' : String(schemaVersion)}
                tone="bg-surface-container text-text-secondary border-outline-variant"
              />
              <SystemField label="Versi aplikasi" value={appVersion} tone="bg-surface-container text-text-secondary border-outline-variant" />
              <SystemField label="Zona waktu" value="WIB (Asia/Jakarta)" tone="bg-info-container text-on-info-container border-outline-variant" />
            </div>
          </section>
        </>
      )}
    </div>
  )
}

function SystemField({ label, value, tone }: { label: string; value: string; tone: string }) {
  return (
    <div className="flex items-center justify-between gap-2 rounded-control border border-border px-3 py-2">
      <div>
        <div className="kicker">{label}</div>
        <div className="mt-0.5 text-body-sm font-medium">{value}</div>
      </div>
      <span className={`badge ${tone}`} aria-hidden />
    </div>
  )
}
