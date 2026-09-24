import { useEffect, useState } from 'react'
import { ApiUser, sheetSyncApi, SheetSyncConfig, SheetSyncQueueCounts } from '../api'
import { ConfirmDialog, ErrorState, LoadingBlock, PageHeader, formatWIB } from '../components/ui'
import { useAsync } from '../hooks'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

/**
 * SheetSync (F18) — sinkronisasi Todo Task ke Google Spreadsheet.
 *
 * Seluruh konfigurasi (Spreadsheet ID, nama sheet, service account) dikelola
 * dari halaman ini; tidak ada nilai yang di-hardcode di aplikasi.
 */
export default function SheetSync({ user, toast }: { user: ApiUser | null; toast: Toast }) {
  const status = useAsync(() => sheetSyncApi.get(), [])

  const [enabled, setEnabled] = useState(false)
  const [spreadsheetID, setSpreadsheetID] = useState('')
  const [sheetName, setSheetName] = useState('Todo')
  const [serviceAccount, setServiceAccount] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState<null | {
    title: string
    body?: string
    confirmLabel?: string
    tone?: 'primary' | 'danger'
    icon?: string
    run: () => Promise<void>
  }>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)

  const isAdmin = user?.role === 'admin'
  const cfg: SheetSyncConfig | undefined = status.data?.config
  const queue: SheetSyncQueueCounts = status.data?.queue ?? { pending: 0, sending: 0, sent: 0, failed: 0 }

  // Isi form dari server saat data pertama kali (atau setelah reload) tiba.
  useEffect(() => {
    if (!cfg) return
    setEnabled(cfg.enabled)
    setSpreadsheetID(cfg.spreadsheet_id)
    setSheetName(cfg.sheet_name || 'Todo')
    setServiceAccount('')
  }, [cfg?.enabled, cfg?.spreadsheet_id, cfg?.sheet_name]) // eslint-disable-line react-hooks/exhaustive-deps

  async function save(nextEnabled = enabled) {
    setBusy(true)
    try {
      const payload: Record<string, unknown> = {
        enabled: nextEnabled,
        spreadsheet_id: spreadsheetID.trim(),
        sheet_name: sheetName.trim(),
      }
      if (serviceAccount.trim()) payload.service_account_json = serviceAccount.trim()
      await sheetSyncApi.save(payload)
      setServiceAccount('')
      toast('success', 'Konfigurasi disimpan')
      status.reload()
    } catch (err) {
      toast('error', 'Gagal menyimpan', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  async function testConnection() {
    setBusy(true)
    try {
      const res = await sheetSyncApi.test()
      toast('success', 'Koneksi berhasil', `Baris uji ditulis ke sheet "${res.sheet_name}".`)
      status.reload()
    } catch (err) {
      toast('error', 'Koneksi gagal', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  async function createTab() {
    setBusy(true)
    try {
      const res = await sheetSyncApi.createTab()
      toast('success', 'Sheet tab siap', res.sheet_name)
      status.reload()
    } catch (err) {
      toast('error', 'Gagal membuat tab', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <PageHeader
        kicker="Integrasi"
        title="Google Sheets"
        description="Todo Task dan Daily Task yang dibuat NOC otomatis tercatat sebagai baris di spreadsheet; status diperbarui in-place. Tidak perlu update manual."
        actions={
          <button className="btn-secondary" onClick={status.reload} disabled={status.loading}>
            <span className="material-symbols-outlined text-[18px]">refresh</span>
            Muat ulang
          </button>
        }
      />

      {status.loading && <LoadingBlock label="Memuat konfigurasi…" />}
      {status.error && (
        <div className="p-4">
          <ErrorState message={status.error} onRetry={status.reload} />
        </div>
      )}

      {!status.loading && !status.error && (
        <div className="grid gap-5 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
          {/* Form konfigurasi */}
          <section className="card p-5">
            <div className="mb-4 flex items-center justify-between gap-3 border-b border-border pb-3">
              <div>
                <h3 className="font-headline text-lg font-semibold">Konfigurasi</h3>
                <p className="text-body-sm text-text-secondary">
                  Hanya admin yang dapat mengubah pengaturan ini.
                </p>
              </div>
              <label className="flex cursor-pointer items-center gap-2">
                <input
                  type="checkbox"
                  className="h-4 w-4"
                  checked={enabled}
                  disabled={!isAdmin || busy}
                  onChange={(e) => setEnabled(e.target.checked)}
                />
                <span className="text-label-md font-semibold">{enabled ? 'Aktif' : 'Nonaktif'}</span>
              </label>
            </div>

            <div className="space-y-4">
              <div>
                <label className="label-field" htmlFor="ss-id">
                  Spreadsheet ID
                </label>
                <input
                  id="ss-id"
                  className="input mono"
                  placeholder="1AbC… (bagian di antara /d/ dan /edit pada URL sheet)"
                  value={spreadsheetID}
                  disabled={!isAdmin || busy}
                  onChange={(e) => setSpreadsheetID(e.target.value)}
                />
                <p className="mt-1 text-label-sm text-text-secondary">
                  Salin dari URL: docs.google.com/spreadsheets/d/<strong>&lt;ID&gt;</strong>/edit
                </p>
              </div>

              <div>
                <label className="label-field" htmlFor="ss-name">
                  Nama Sheet (tab)
                </label>
                <input
                  id="ss-name"
                  className="input"
                  placeholder="Todo"
                  value={sheetName}
                  disabled={!isAdmin || busy}
                  onChange={(e) => setSheetName(e.target.value)}
                />
                <p className="mt-1 text-label-sm text-text-secondary">
                  Tab tujuan penulisan. Tidak boleh memuat karakter [ ] * ? / \ : dan maksimal 100 karakter.
                </p>
              </div>

              <div>
                <label className="label-field" htmlFor="ss-sa">
                  Service Account JSON
                </label>
                <textarea
                  id="ss-sa"
                  className="input mono h-32 py-2 text-label-sm"
                  placeholder={
                    cfg?.service_account_set
                      ? '•••••• tersimpan (terenkripsi) — tempel JSON baru hanya bila ingin mengganti ••••••'
                      : '{ "type": "service_account", "client_email": "…", "private_key": "…" }'
                  }
                  value={serviceAccount}
                  disabled={!isAdmin || busy}
                  onChange={(e) => setServiceAccount(e.target.value)}
                />
                <p className="mt-1 text-label-sm text-text-secondary">
                  Tersimpan terenkripsi AES-256-GCM dan tidak pernah ditampilkan kembali.
                </p>
              </div>

              {cfg?.client_email && (
                <div className="rounded-control border border-border bg-surface-container-low p-3">
                  <div className="kicker mb-1">Bagikan spreadsheet ke</div>
                  <code className="mono break-all text-label-md">{cfg.client_email}</code>
                  <p className="mt-1 text-label-sm text-text-secondary">
                    Beri akses <strong>Editor</strong> di Google Sheets.
                  </p>
                </div>
              )}

              <div className="flex flex-wrap gap-2 border-t border-border pt-4">
                <button className="btn-primary" disabled={!isAdmin || busy} onClick={() => save()}>
                  {busy ? 'Menyimpan…' : 'Simpan'}
                </button>
                <button className="btn-secondary" disabled={!isAdmin || busy} onClick={testConnection}>
                  <span className="material-symbols-outlined text-[18px]">cable</span>
                  Uji Koneksi
                </button>
                <button className="btn-secondary" disabled={!isAdmin || busy} onClick={createTab}>
                  <span className="material-symbols-outlined text-[18px]">add_box</span>
                  Buat Sheet Tab
                </button>
              </div>

              {!isAdmin && (
                <p className="text-label-sm text-warning">
                  Anda tidak memiliki izin untuk mengubah konfigurasi ini (hanya admin).
                </p>
              )}
            </div>
          </section>

          {/* Status */}
          <section className="card p-5">
            <h3 className="mb-3 font-headline text-lg font-semibold">Status</h3>

            <dl className="space-y-3">
              <div className="flex items-center justify-between gap-3">
                <dt className="text-body-sm text-text-secondary">Kondisi</dt>
                <dd>
                  <span
                    className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-label-sm font-semibold ${
                      cfg?.enabled ? 'bg-success-container text-on-success-container' : 'bg-surface-container text-text-secondary'
                    }`}
                  >
                    <span className={`h-2 w-2 rounded-full ${cfg?.enabled ? 'bg-success' : 'bg-unknown'}`} />
                    {cfg?.enabled ? 'Aktif' : 'Nonaktif'}
                  </span>
                </dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="text-body-sm text-text-secondary">Kredensial</dt>
                <dd className="text-label-md font-semibold">{cfg?.service_account_set ? 'Terpasang' : 'Belum diisi'}</dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="text-body-sm text-text-secondary">Header tertulis</dt>
                <dd className="text-label-md font-semibold">{cfg?.header_written ? 'Ya' : 'Belum'}</dd>
              </div>
              <div className="flex items-center justify-between gap-3">
                <dt className="text-body-sm text-text-secondary">Sinkron terakhir</dt>
                <dd className="text-label-md font-semibold">{cfg?.last_sync_at ? formatWIB(cfg.last_sync_at) : '—'}</dd>
              </div>
            </dl>

            <div className="mt-4 grid grid-cols-2 gap-2 border-t border-border pt-4">
              <StatBox label="Menunggu" value={queue.pending} tone="bg-info-container text-on-info-container" />
              <StatBox label="Mengirim" value={queue.sending} tone="bg-warning-container text-on-warning-container" />
              <StatBox label="Terkirim" value={queue.sent} tone="bg-success-container text-on-success-container" />
              <StatBox label="Gagal" value={queue.failed} tone="bg-critical-container text-on-critical-container" />
            </div>

            {cfg?.last_error && (
              <div className="mt-4 rounded-control border border-critical/30 bg-critical-container/40 p-3">
                <div className="kicker mb-1 text-on-critical-container">Error terakhir</div>
                <p className="break-words text-label-sm text-on-critical-container">{cfg.last_error}</p>
              </div>
            )}

            <p className="mt-4 border-t border-border pt-4 text-label-sm text-text-secondary">
              Sinkronisasi berlaku untuk <strong>Todo Task</strong> dan <strong>Daily Task</strong> yang
              baru dibuat serta perubahannya. Data lama tidak diisi otomatis.
            </p>
          </section>
        </div>
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

function StatBox({ label, value, tone }: { label: string; value: number; tone: string }) {
  return (
    <div className={`rounded-control border border-outline-variant px-3 py-2 ${tone}`}>
      <div className="text-[10px] uppercase tracking-wider opacity-80">{label}</div>
      <div className="font-headline text-xl font-bold">{value}</div>
    </div>
  )
}
