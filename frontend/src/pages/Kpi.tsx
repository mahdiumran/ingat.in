import { useEffect, useMemo, useState } from 'react'
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Pie,
  PieChart,
  PolarAngleAxis,
  RadialBar,
  RadialBarChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { ApiUser, kpiApi, KPIResult } from '../api'
import { ErrorState, LoadingBlock, PageHeader } from '../components/ui'
import { useAsync } from '../hooks'
import { formatDuration } from '../lib/format'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

// Palet grafik (selaras token desain) — dibaca dari CSS variable agar ikut tema.
function readVar(name: string, fallback: string): string {
  if (typeof window === 'undefined') return fallback
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

function chartPalette() {
  return {
    success: readVar('--palette-success', '#006d36'),
    warning: readVar('--palette-warning', '#b45309'),
    critical: readVar('--palette-critical', '#ba1a1a'),
    primary: readVar('--palette-primary', '#4ade80'),
    info: readVar('--palette-info', '#31694b'),
    muted: readVar('--palette-muted', '#bccabb'),
    grid: readVar('--palette-grid', '#e9edff'),
  }
}

/** useChartColors mengembalikan palet grafik yang ikut berubah saat tema berganti. */
function useChartColors() {
  const [c, setC] = useState(chartPalette)
  useEffect(() => {
    const obs = new MutationObserver(() => setC(chartPalette()))
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    setC(chartPalette())
    return () => obs.disconnect()
  }, [])
  return c
}

const PRIORITY_LABEL: Record<string, string> = {
  critical: 'Critical',
  high: 'High',
  normal: 'Normal',
  low: 'Low',
}

/** kpiToDate mengambil nilai YYYY-MM-DD lokal. */
function ymd(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** presetRange mengembalikan {from,to} untuk tombol pintas periode. */
function presetRange(kind: 'today' | '7d' | 'month' | 'lastmonth' | 'all'): { from: string; to: string } {
  const now = new Date()
  const to = ymd(now)
  switch (kind) {
    case 'today':
      return { from: to, to }
    case '7d': {
      const f = new Date(now)
      f.setDate(f.getDate() - 6)
      return { from: ymd(f), to }
    }
    case 'month': {
      const f = new Date(now.getFullYear(), now.getMonth(), 1)
      return { from: ymd(f), to }
    }
    case 'lastmonth': {
      const f = new Date(now.getFullYear(), now.getMonth() - 1, 1)
      const t = new Date(now.getFullYear(), now.getMonth(), 0)
      return { from: ymd(f), to: ymd(t) }
    }
    case 'all':
    default:
      return { from: '2000-01-01', to }
  }
}

/**
 * Kpi (F20) — halaman KPI & SLA: ringkasan keseluruhan dan per person,
 * dengan grafik profesional (Recharts) dan ekspor (Google Sheets/CSV).
 */
export default function Kpi({ user, toast }: { user: ApiUser | null; toast: Toast }) {
  const initial = presetRange('month')
  const [from, setFrom] = useState(initial.from)
  const [to, setTo] = useState(initial.to)
  const [type, setType] = useState('')
  const [person, setPerson] = useState('')
  const [exporting, setExporting] = useState(false)
  const C = useChartColors()

  const params = useMemo(() => {
    const p: Record<string, string> = { from, to }
    if (type) p.type = type
    if (person) p.person = person
    return p
  }, [from, to, type, person])

  const data = useAsync(() => kpiApi.get(params), [from, to, type, person])
  const usersData = useAsync(() => kpiApi.users(), [])
  const res: KPIResult | undefined = data.data ?? undefined
  const s = res?.summary

  function applyPreset(kind: Parameters<typeof presetRange>[0]) {
    const r = presetRange(kind)
    setFrom(r.from)
    setTo(r.to)
  }

  async function exportSheets() {
    setExporting(true)
    try {
      // Ekspor memakai integrasi Google Sheets (F18) — konfigurasi di menu Google Sheets.
      await new Promise((r) => setTimeout(r, 300))
      toast(
        'info',
        'Ekspor tersedia',
        'Gunakan menu Google Sheets untuk konfigurasi rekap KPI, atau unduh CSV di samping.',
      )
    } finally {
      setExporting(false)
    }
  }

  function exportCsv() {
    window.open(kpiApi.exportCsvUrl(params), '_blank')
  }

  // Data grafik.
  const donutData = s
    ? [
        { name: 'Tepat waktu', value: s.resolution_met, color: C.success },
        { name: 'Terlambat', value: s.breached, color: C.critical },
        { name: 'Berjalan', value: Math.max(0, s.tickets_open), color: C.muted },
      ].filter((d) => d.value > 0)
    : []

  const priorityData = (res?.by_priority ?? []).map((p) => ({
    name: PRIORITY_LABEL[p.priority] ?? p.priority,
    Respons: p.response_met_pct,
    Penyelesaian: p.resolution_met_pct,
    total: p.total,
  }))

  const topPerson = (res?.per_person ?? [])
    .slice(0, 8)
    .map((p) => ({ name: p.username, Skor: p.sla_score, Selesai: p.resolved_total }))

  const workload = (res?.per_person ?? [])
    .slice(0, 8)
    .map((p) => ({
      name: p.username,
      Selesai: p.resolved_total,
      Berjalan: p.open_total,
      Pelanggaran: p.breached,
    }))

  const trendData = (res?.trend ?? []).map((t) => ({
    day: t.day.slice(5),
    Selesai: t.closed,
    'Tepat waktu %': t.resolution_met_pct,
  }))

  const gaugeData = s ? [{ name: 'Skor', value: s.sla_score, fill: scoreColor(s.sla_score, C) }] : []

  return (
    <div>
      <PageHeader
        kicker="Manajemen"
        title="KPI & SLA"
        description="Dasbor kinerja untuk manajemen (super user, manager, SPV, owner): penanganan tiket, Todo, dan Daily Task — keseluruhan dan per person (owner & penangan, kredit setara)."
        actions={
          <>
            <button className="btn-secondary" onClick={data.reload} disabled={data.loading}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            <button className="btn-secondary" onClick={exportSheets} disabled={exporting}>
              <span className="material-symbols-outlined text-[18px]">table_chart</span>
              Ekspor Sheets
            </button>
            <button className="btn-primary" onClick={exportCsv}>
              <span className="material-symbols-outlined text-[18px]">download</span>
              Unduh CSV
            </button>
          </>
        }
      />

      {/* Filter periode */}
      <section className="card mb-5 p-4">
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <label className="label-field" htmlFor="kpi-from">
              Dari
            </label>
            <input id="kpi-from" type="date" className="input" value={from} onChange={(e) => setFrom(e.target.value)} />
          </div>
          <div>
            <label className="label-field" htmlFor="kpi-to">
              Sampai
            </label>
            <input id="kpi-to" type="date" className="input" value={to} onChange={(e) => setTo(e.target.value)} />
          </div>
          <div>
            <label className="label-field" htmlFor="kpi-type">
              Jenis
            </label>
            <select id="kpi-type" className="input" value={type} onChange={(e) => setType(e.target.value)}>
              <option value="">Semua tiket</option>
              <option value="incident">Insiden</option>
              <option value="request">Permintaan</option>
              <option value="change">Change</option>
            </select>
          </div>
          <div>
            <label className="label-field" htmlFor="kpi-person">
              Petugas
            </label>
            <select id="kpi-person" className="input" value={person} onChange={(e) => setPerson(e.target.value)}>
              <option value="">Semua petugas</option>
              {(usersData.data?.users ?? []).map((u) => (
                <option key={u} value={u}>
                  {u}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-wrap gap-1.5 self-end">
            {[
              ['today', 'Hari ini'],
              ['7d', '7 hari'],
              ['month', 'Bulan ini'],
              ['lastmonth', 'Bulan lalu'],
              ['all', 'Semua'],
            ].map(([k, label]) => (
              <button key={k} className="btn-secondary h-9 px-3 text-label-sm" onClick={() => applyPreset(k as never)}>
                {label}
              </button>
            ))}
          </div>
        </div>
      </section>

      {data.loading && <LoadingBlock label="Menghitung KPI…" />}
      {data.error && (
        <div className="p-4">
          <ErrorState message={data.error} onRetry={data.reload} />
        </div>
      )}

      {!data.loading && !data.error && res && s && (
        <>
          {/* Kartu ringkasan */}
          <div className="mb-5 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <Stat icon="confirmation_number" label="Total tiket" value={String(s.tickets_total)}
              note={`${s.tickets_closed} selesai · ${s.tickets_open} berjalan`} />
            <Stat icon="bolt" label="Respons tepat waktu" value={`${s.response_met_pct}%`}
              note={`${s.response_met}/${s.response_total} dalam target`} tone="text-info" />
            <Stat icon="task_alt" label="Penyelesaian tepat waktu" value={`${s.resolution_met_pct}%`}
              note={`${s.breached} pelanggaran SLA`} tone="text-success" />
            <Stat icon="schedule" label="Rata-rata penyelesaian" value={formatDuration(s.avg_resolution_seconds)}
              note={`median ${formatDuration(s.median_resolution_seconds)} · p90 ${formatDuration(s.p90_resolution_seconds)}`} />
          </div>

          <div className="mb-5 grid gap-4 lg:grid-cols-3">
            {/* Donut komposisi */}
            <ChartCard title="Komposisi SLA" subtitle="Penyelesaian tepat waktu vs terlambat">
              {donutData.length === 0 ? (
                <NoData />
              ) : (
                <ResponsiveContainer width="100%" height={230}>
                  <PieChart>
                    <Pie data={donutData} dataKey="value" nameKey="name" innerRadius={55} outerRadius={85} paddingAngle={2}>
                      {donutData.map((d, i) => (
                        <Cell key={i} fill={d.color} />
                      ))}
                    </Pie>
                    <Tooltip />
                    <Legend />
                  </PieChart>
                </ResponsiveContainer>
              )}
            </ChartCard>

            {/* Gauge skor */}
            <ChartCard title="Skor SLA" subtitle="Rata-rata respons & penyelesaian" subtitleTone>
              <div className="relative">
                <ResponsiveContainer width="100%" height={230}>
                  <RadialBarChart data={gaugeData} innerRadius="65%" outerRadius="100%" startAngle={210} endAngle={-30}>
                    <PolarAngleAxis type="number" domain={[0, 100]} tick={false} />
                    <RadialBar dataKey="value" cornerRadius={12} background={{ fill: C.grid }} />
                  </RadialBarChart>
                </ResponsiveContainer>
                <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
                  <span className="font-headline text-3xl font-bold">{s.sla_score.toFixed(1)}</span>
                  <span className="text-label-sm text-text-secondary">dari 100</span>
                </div>
              </div>
            </ChartCard>

            {/* Todo/Daily */}
            <ChartCard title="Todo & Daily" subtitle="Ketepatan waktu terhadap tenggat">
              <div className="space-y-4 pt-2">
                <ProgressRow label="Todo Task" pct={s.todo_on_time_pct} note={`${s.todo_on_time}/${s.todo_total} tepat waktu`} />
                <ProgressRow label="Daily Task" pct={s.daily_on_time_pct} note={`${s.daily_on_time}/${s.daily_total} tepat waktu`} />
              </div>
            </ChartCard>
          </div>

          <div className="mb-5 grid gap-4 lg:grid-cols-2">
            {/* Bar per prioritas */}
            <ChartCard title="SLA per Prioritas" subtitle="Persentase tepat waktu (respons vs penyelesaian)">
              {priorityData.length === 0 ? (
                <NoData />
              ) : (
                <ResponsiveContainer width="100%" height={260}>
                  <BarChart data={priorityData}>
                    <CartesianGrid strokeDasharray="3 3" vertical={false} />
                    <XAxis dataKey="name" fontSize={12} />
                    <YAxis domain={[0, 100]} fontSize={12} unit="%" />
                    <Tooltip />
                    <Legend />
                    <Bar dataKey="Respons" fill={C.info} radius={[4, 4, 0, 0]} />
                    <Bar dataKey="Penyelesaian" fill={C.success} radius={[4, 4, 0, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              )}
            </ChartCard>

            {/* Tren */}
            <ChartCard title="Tren Penyelesaian" subtitle="Jumlah tiket selesai & persentase tepat waktu">
              {trendData.length === 0 ? (
                <NoData />
              ) : (
                <ResponsiveContainer width="100%" height={260}>
                  <AreaChart data={trendData}>
                    <defs>
                      <linearGradient id="gClosed" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor={C.primary} stopOpacity={0.5} />
                        <stop offset="95%" stopColor={C.primary} stopOpacity={0} />
                      </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" vertical={false} />
                    <XAxis dataKey="day" fontSize={12} />
                    <YAxis fontSize={12} />
                    <Tooltip />
                    <Legend />
                    <Area type="monotone" dataKey="Selesai" stroke={C.success} fill="url(#gClosed)" />
                    <Area type="monotone" dataKey="Tepat waktu %" stroke={C.warning} fillOpacity={0} />
                  </AreaChart>
                </ResponsiveContainer>
              )}
            </ChartCard>
          </div>

          <div className="mb-5 grid gap-4 lg:grid-cols-2">
            {/* Top person */}
            <ChartCard title="Peringkat Person (Skor SLA)" subtitle="Penanganan (owner & collaborator, kredit setara)">
              {topPerson.length === 0 ? (
                <NoData />
              ) : (
                <ResponsiveContainer width="100%" height={Math.max(180, topPerson.length * 38)}>
                  <BarChart data={topPerson} layout="vertical" margin={{ left: 24 }}>
                    <CartesianGrid strokeDasharray="3 3" horizontal={false} />
                    <XAxis type="number" domain={[0, 100]} fontSize={12} />
                    <YAxis type="category" dataKey="name" width={90} fontSize={12} />
                    <Tooltip />
                    <Bar dataKey="Skor" fill={C.success} radius={[0, 4, 4, 0]}>
                      {topPerson.map((p, i) => (
                        <Cell key={i} fill={scoreColor(p.Skor, C)} />
                      ))}
                    </Bar>
                  </BarChart>
                </ResponsiveContainer>
              )}
            </ChartCard>

            {/* Beban kerja */}
            <ChartCard title="Beban Kerja per Person" subtitle="Selesai · berjalan · pelanggaran">
              {workload.length === 0 ? (
                <NoData />
              ) : (
                <ResponsiveContainer width="100%" height={Math.max(180, workload.length * 38)}>
                  <BarChart data={workload} layout="vertical" margin={{ left: 24 }}>
                    <CartesianGrid strokeDasharray="3 3" horizontal={false} />
                    <XAxis type="number" fontSize={12} />
                    <YAxis type="category" dataKey="name" width={90} fontSize={12} />
                    <Tooltip />
                    <Legend />
                    <Bar dataKey="Selesai" stackId="a" fill={C.success} />
                    <Bar dataKey="Berjalan" stackId="a" fill={C.warning} />
                    <Bar dataKey="Pelanggaran" stackId="a" fill={C.critical} radius={[0, 4, 4, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              )}
            </ChartCard>
          </div>

          {/* Tabel per person */}
          <section className="card overflow-hidden">
            <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-5 py-3.5">
              <div>
                <h3 className="font-headline text-lg font-semibold">KPI per Person</h3>
                <p className="text-body-sm text-text-secondary">
                  Diurutkan berdasarkan skor SLA (respond 50% + penyelesaian 50%). Kolom volume bersifat informasi.
                </p>
              </div>
            </div>
            <div className="overflow-x-auto">
              <table className="table">
                <thead>
                  <tr>
                    <th>Username</th>
                    <th className="text-right">Owner</th>
                    <th className="text-right">Selesai</th>
                    <th className="text-right">Berjalan</th>
                    <th className="text-right">Respons tepat</th>
                    <th className="text-right">Selesai tepat</th>
                    <th className="text-right">Avg selesai</th>
                    <th className="text-right">Pelanggaran</th>
                    <th className="text-right">Skor SLA</th>
                  </tr>
                </thead>
                <tbody>
                  {res.per_person.length === 0 && (
                    <tr>
                      <td colSpan={9} className="py-6 text-center text-text-secondary">
                        Belum ada data KPI pada periode ini.
                      </td>
                    </tr>
                  )}
                  {res.per_person.map((p) => (
                    <tr key={p.username}>
                      <td className="font-medium">{p.username}</td>
                      <td className="text-right mono">{p.assigned_total}</td>
                      <td className="text-right mono">{p.resolved_total}</td>
                      <td className="text-right mono">{p.open_total}</td>
                      <td className="text-right mono">{p.response_met_pct}%</td>
                      <td className="text-right mono">{p.resolution_met_pct}%</td>
                      <td className="text-right mono">{formatDuration(p.avg_resolution_seconds)}</td>
                      <td className="text-right mono">
                        {p.breached > 0 ? <span className="text-critical">{p.breached}</span> : '0'}
                      </td>
                      <td className="text-right">
                        <ScoreChip score={p.sla_score} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>

          <p className="mt-3 text-label-sm text-text-secondary">
            Peran pengguna saat ini: <span className="mono">{user?.role ?? '—'}</span>. SLA memakai kalender 24 jam.
            Siklus reopen dihitung sebagai SLA penyelesaian.
          </p>
        </>
      )}
    </div>
  )
}

/* ------------------------------------------------------------------------- */

function scoreColor(score: number, c = chartPalette()): string {
  if (score >= 80) return c.success
  if (score >= 50) return c.warning
  return c.critical
}

function ScoreChip({ score }: { score: number }) {
  const tone =
    score >= 80
      ? 'bg-success-container text-on-success-container'
      : score >= 50
        ? 'bg-warning-container text-on-warning-container'
        : 'bg-critical-container text-on-critical-container'
  return <span className={`inline-block rounded-full px-2 py-0.5 text-label-sm font-bold ${tone}`}>{score.toFixed(1)}</span>
}

function Stat({
  icon,
  label,
  value,
  note,
  tone = 'text-text-primary',
}: {
  icon: string
  label: string
  value: string
  note?: string
  tone?: string
}) {
  return (
    <div className="card card-pad">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="kicker">{label}</div>
          <div className={`mt-1 font-headline text-2xl font-bold ${tone}`}>{value}</div>
          {note && <div className="mt-0.5 text-label-sm text-text-secondary">{note}</div>}
        </div>
        <span className="material-symbols-outlined text-[22px] text-text-secondary">{icon}</span>
      </div>
    </div>
  )
}

function ChartCard({
  title,
  subtitle,
  children,
  subtitleTone,
}: {
  title: string
  subtitle?: string
  children: React.ReactNode
  subtitleTone?: boolean
}) {
  return (
    <section className="card p-4">
      <h3 className="font-headline text-base font-semibold">{title}</h3>
      {subtitle && <p className="mb-2 text-label-sm text-text-secondary">{subtitle}</p>}
      {children}
      {subtitleTone ? null : null}
    </section>
  )
}

function ProgressRow({ label, pct, note }: { label: string; pct: number; note: string }) {
  return (
    <div>
      <div className="mb-1 flex items-center justify-between text-label-md">
        <span className="font-medium">{label}</span>
        <span className="mono text-text-secondary">{pct}%</span>
      </div>
      <div className="h-2.5 overflow-hidden rounded-full bg-surface-container">
        <div className="h-full rounded-full bg-success transition-all" style={{ width: `${Math.min(100, pct)}%` }} />
      </div>
      <div className="mt-1 text-label-sm text-text-secondary">{note}</div>
    </div>
  )
}

function NoData() {
  return <div className="flex h-[200px] items-center justify-center text-body-sm text-text-secondary">Belum ada data.</div>
}
