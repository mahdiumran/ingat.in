import { ReactNode, useEffect, useState } from 'react'
import { ApiUser, backupApi, BackupConfig, BackupInfo } from '../api'
import { ConfirmDialog, ErrorState, LoadingBlock, PageHeader, formatWIB } from '../components/ui'
import { useAsync } from '../hooks'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

/** formatBytes mengubah ukuran byte menjadi teks yang mudah dibaca. */
function formatBytes(n: number): string {
  if (!n || n < 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`
}

/**
 * Backup (F34) — cadangan & pemulihan database dari panel (khusus admin),
 * dengan opsi unggah otomatis ke server FTP.
 *
 * Cadangan dibuat dengan pg_dump (format custom) dan dapat dipulihkan kembali
 * memakai pg_restore. Pemulihan bersifat DESTRUKTIF dan meminta konfirmasi.
 */
export default function Backup({ user, toast }: { user: ApiUser | null; toast: Toast }) {
  const status = useAsync(() => backupApi.list(), [])

  const [enabled, setEnabled] = useState(false)
  const [schedule, setSchedule] = useState('0 2 * * *')
  const [keepDays, setKeepDays] = useState(14)
  const [ftpEnabled, setFtpEnabled] = useState(false)
  const [ftpHost, setFtpHost] = useState('')
  const [ftpPort, setFtpPort] = useState(21)
  const [ftpUser, setFtpUser] = useState('')
  const [ftpPass, setFtpPass] = useState('')
  const [ftpDir, setFtpDir] = useState('')
  const [ftpPassive, setFtpPassive] = useState(true)
  const [busy, setBusy] = useState(false)
  // F34: unggah berkas cadangan dari komputer (opsional langsung pulihkan).
  const [uploadFile, setUploadFile] = useState<File | null>(null)
  const [uploading, setUploading] = useState(false)

  const [confirm, setConfirm] = useState<null | {
    title: string
    body?: ReactNode
    confirmLabel?: string
    tone?: 'primary' | 'danger'
    icon?: string
    run: () => Promise<void>
  }>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)

  const isAdmin = user?.role === 'admin' || user?.is_super === true
  const cfg: BackupConfig | undefined = status.data?.config
  const backups: BackupInfo[] = status.data?.backups ?? []
  const tools = status.data?.tools

  // Isi form dari server ketika data tiba (tanpa menimpa ketikan setelahnya).
  useEffect(() => {
    if (!cfg) return
    setEnabled(cfg.enabled)
    setSchedule(cfg.schedule || '0 2 * * *')
    setKeepDays(cfg.keep_days || 14)
    setFtpEnabled(cfg.ftp?.enabled ?? false)
    setFtpHost(cfg.ftp?.host ?? '')
    setFtpPort(cfg.ftp?.port || 21)
    setFtpUser(cfg.ftp?.username ?? '')
    setFtpDir(cfg.ftp?.dir ?? '')
    setFtpPassive(cfg.ftp?.passive ?? true)
    setFtpPass('')
  }, [cfg?.enabled, cfg?.schedule, cfg?.keep_days, cfg?.ftp?.host, cfg?.ftp?.port, cfg?.ftp?.username, cfg?.ftp?.dir, cfg?.ftp?.enabled, cfg?.ftp?.passive]) // eslint-disable-line react-hooks/exhaustive-deps

  async function saveConfig() {
    setBusy(true)
    try {
      const payload: Parameters<typeof backupApi.saveConfig>[0] = {
        enabled,
        schedule: schedule.trim(),
        keep_days: keepDays,
        ftp: {
          enabled: ftpEnabled,
          host: ftpHost.trim(),
          port: ftpPort,
          username: ftpUser.trim(),
          dir: ftpDir.trim(),
          passive: ftpPassive,
        },
      }
      if (ftpPass.trim()) payload.ftp_password = ftpPass.trim()
      await backupApi.saveConfig(payload)
      setFtpPass('')
      toast('success', 'Konfigurasi cadangan disimpan')
      status.reload()
    } catch (err) {
      toast('error', 'Gagal menyimpan', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  async function createBackup() {
    setBusy(true)
    try {
      const res = await backupApi.create()
      toast('success', 'Cadangan dibuat', res.upload)
      status.reload()
    } catch (err) {
      toast('error', 'Gagal membuat cadangan', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  async function testFTP() {
    setBusy(true)
    try {
      const res = await backupApi.testFTP()
      toast('success', 'Koneksi berhasil', res.message)
    } catch (err) {
      toast('error', 'Koneksi FTP gagal', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  async function download(name: string) {
    try {
      await backupApi.download(name)
    } catch (err) {
      toast('error', 'Unduhan gagal', err instanceof Error ? err.message : undefined)
    }
  }

  async function upload(name: string) {
    setBusy(true)
    try {
      const res = await backupApi.upload(name)
      toast('success', 'Diunggah', res.message)
      status.reload()
    } catch (err) {
      toast('error', 'Unggah FTP gagal', err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  // Simpan berkas unggahan ke server (tanpa memulihkan).
  async function doUploadSave() {
    if (!uploadFile) {
      toast('warning', 'Pilih berkas terlebih dahulu')
      return
    }
    setUploading(true)
    try {
      const res = await backupApi.uploadFile(uploadFile, false)
      toast('success', 'Berkas diunggah', res.message)
      setUploadFile(null)
      status.reload()
    } catch (err) {
      toast('error', 'Gagal mengunggah berkas', err instanceof Error ? err.message : undefined)
    } finally {
      setUploading(false)
    }
  }

  // Unggah berkas lalu langsung pulihkan (destruktif) — minta konfirmasi dulu.
  function askUploadRestore() {
    if (!uploadFile) {
      toast('warning', 'Pilih berkas terlebih dahulu')
      return
    }
    const file = uploadFile
    setConfirm({
      title: 'Unggah & pulihkan dari berkas ini?',
      tone: 'danger',
      icon: 'restore',
      confirmLabel: 'Unggah & Pulihkan',
      body: (
        <>
          Berkas <strong>{file.name}</strong> ({formatBytes(file.size)}) akan diunggah, lalu{' '}
          <strong>seluruh data saat ini DIGANTI</strong> dengan isi cadangan. Tindakan ini tidak dapat
          dibatalkan.
        </>
      ),
      run: async () => {
        const res = await backupApi.uploadFile(file, true)
        toast('success', 'Dipulihkan', res.message)
        setUploadFile(null)
        status.reload()
      },
    })
  }

  function askRestore(name: string) {
    setConfirm({
      title: 'Pulihkan database dari cadangan?',
      tone: 'danger',
      icon: 'restore',
      confirmLabel: 'Pulihkan sekarang',
      body: (
        <>
          Seluruh data saat ini pada tabel yang ada di <strong>{name}</strong> akan{' '}
          <strong>DITIMPA</strong>. Tindakan ini tidak dapat dibatalkan. Pastikan Anda sudah membuat
          cadangan terbaru sebelum melanjutkan.
        </>
      ),
      run: async () => {
        const res = await backupApi.restore(name)
        toast('success', 'Database dipulihkan', res.message)
        status.reload()
      },
    })
  }

  function askDelete(name: string) {
    setConfirm({
      title: 'Hapus berkas cadangan?',
      tone: 'danger',
      icon: 'delete',
      confirmLabel: 'Hapus',
      body: (
        <>
          Berkas <strong>{name}</strong> akan dihapus permanen dari server.
        </>
      ),
      run: async () => {
        await backupApi.remove(name)
        toast('success', 'Berkas dihapus')
        status.reload()
      },
    })
  }

  return (
    <div>
      <PageHeader
        kicker="Manajemen"
        title="Backup & Restore"
        description="Buat, unduh, dan pulihkan cadangan database. Cadangan dapat diunggah otomatis ke server FTP."
        actions={
          <div className="flex gap-2">
            <button className="btn-secondary" onClick={status.reload} disabled={status.loading}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            <button className="btn-primary" onClick={createBackup} disabled={!isAdmin || busy}>
              <span className="material-symbols-outlined text-[18px]">backup</span>
              {busy ? 'Memproses…' : 'Buat Cadangan Sekarang'}
            </button>
          </div>
        }
      />

      {status.loading && <LoadingBlock label="Memuat konfigurasi cadangan…" />}
      {status.error && (
        <div className="p-4">
          <ErrorState message={status.error} onRetry={status.reload} />
        </div>
      )}

      {!status.loading && !status.error && (
        <div className="space-y-5">
          {/* Unggah berkas cadangan dari komputer */}
          <section className="card p-5">
            <div className="mb-3 border-b border-border pb-3">
              <h3 className="font-headline text-lg font-semibold">Unggah &amp; Pulihkan dari Berkas</h3>
              <p className="text-body-sm text-text-secondary">
                Pilih berkas <code className="mono">.dump</code> (format custom pg_dump) dari komputer
                Anda. Anda dapat menyimpannya ke server atau langsung memulihkannya.
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-3">
              <input
                type="file"
                accept=".dump,application/octet-stream"
                disabled={!isAdmin || uploading}
                onChange={(e) => setUploadFile(e.target.files?.[0] ?? null)}
                className="input h-auto py-2 file:mr-3 file:rounded file:border-0 file:bg-surface-container file:px-3 file:py-1.5 file:text-label-md"
              />
              <button className="btn-secondary" disabled={!isAdmin || uploading || !uploadFile} onClick={doUploadSave}>
                <span className="material-symbols-outlined text-[18px]">upload_file</span>
                {uploading ? 'Mengunggah…' : 'Simpan ke Server'}
              </button>
              <button
                className="btn-danger"
                disabled={!isAdmin || uploading || !uploadFile}
                onClick={askUploadRestore}
              >
                <span className="material-symbols-outlined text-[18px]">restore</span>
                Unggah &amp; Pulihkan
              </button>
            </div>
            {uploadFile && (
              <p className="mt-2 text-label-sm text-text-secondary">
                Dipilih: <span className="mono">{uploadFile.name}</span> ({formatBytes(uploadFile.size)})
              </p>
            )}
            <p className="mt-3 rounded-control border border-warning/30 bg-warning-container/40 p-2.5 text-label-sm text-on-warning-container">
              <strong>Peringatan:</strong> "Unggah &amp; Pulihkan" akan <strong>mengganti seluruh data</strong>{' '}
              dengan isi berkas cadangan dan tidak dapat dibatalkan.
            </p>
          </section>

          <div className="grid gap-5 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
          {/* Daftar cadangan */}
          <section className="card p-5">
            <div className="mb-4 flex items-center justify-between gap-3 border-b border-border pb-3">
              <div>
                <h3 className="font-headline text-lg font-semibold">Berkas Cadangan</h3>
                <p className="text-body-sm text-text-secondary">
                  Tersimpan di <code className="mono">{status.data?.dir}</code>
                </p>
              </div>
              <span className="badge border-outline-variant bg-surface-container text-text-secondary">
                {backups.length} berkas
              </span>
            </div>

            {tools && (!tools.pg_dump || !tools.pg_restore) && (
              <div className="mb-4 rounded-control border border-critical/30 bg-critical-container/40 p-3 text-label-sm text-on-critical-container">
                Utilitas PostgreSQL tidak lengkap:{' '}
                {!tools.pg_dump && <strong>pg_dump </strong>}
                {!tools.pg_restore && <strong>pg_restore </strong>}
                tidak ditemukan di server. Backup/restore tidak dapat berjalan.
              </div>
            )}

            {backups.length === 0 ? (
              <div className="rounded-control border border-dashed border-border py-10 text-center text-body-sm text-text-secondary">
                Belum ada cadangan. Klik <strong>Buat Cadangan Sekarang</strong> untuk memulai.
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full">
                  <thead>
                    <tr className="border-b border-border text-left">
                      <th className="pb-2 kicker">Berkas</th>
                      <th className="pb-2 kicker">Ukuran</th>
                      <th className="pb-2 kicker">Dibuat</th>
                      <th className="pb-2 kicker">FTP</th>
                      <th className="pb-2 kicker text-right">Aksi</th>
                    </tr>
                  </thead>
                  <tbody>
                    {backups.map((b) => (
                      <tr key={b.name} className="border-b border-border/60 last:border-0">
                        <td className="py-2.5">
                          <div className="mono text-label-md break-all">{b.name}</div>
                        </td>
                        <td className="py-2.5 text-body-sm">{formatBytes(b.size_bytes)}</td>
                        <td className="py-2.5 text-body-sm">{formatWIB(b.created_at)}</td>
                        <td className="py-2.5">
                          {b.upload_error ? (
                            <span
                              className="badge bg-critical-container text-on-critical-container"
                              title={b.upload_error}
                            >
                              gagal
                            </span>
                          ) : b.uploaded ? (
                            <span className="badge bg-success-container text-on-success-container">
                              terunggah
                            </span>
                          ) : (
                            <span className="badge border-outline-variant bg-surface-container text-text-secondary">
                              lokal
                            </span>
                          )}
                        </td>
                        <td className="py-2.5">
                          <div className="flex justify-end gap-1.5">
                            <button
                              className="btn-ghost h-8 px-2"
                              title="Unduh"
                              onClick={() => download(b.name)}
                            >
                              <span className="material-symbols-outlined text-[18px]">download</span>
                            </button>
                            {cfg?.ftp?.enabled && (
                              <button
                                className="btn-ghost h-8 px-2"
                                title="Unggah ke FTP"
                                disabled={busy}
                                onClick={() => upload(b.name)}
                              >
                                <span className="material-symbols-outlined text-[18px]">cloud_upload</span>
                              </button>
                            )}
                            <button
                              className="btn-ghost h-8 px-2 text-critical"
                              title="Pulihkan"
                              disabled={!isAdmin || busy}
                              onClick={() => askRestore(b.name)}
                            >
                              <span className="material-symbols-outlined text-[18px]">restore</span>
                            </button>
                            <button
                              className="btn-ghost h-8 px-2 text-critical"
                              title="Hapus"
                              disabled={!isAdmin || busy}
                              onClick={() => askDelete(b.name)}
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

          {/* Konfigurasi */}
          <div className="space-y-5">
            <section className="card p-5">
              <div className="mb-4 flex items-center justify-between gap-3 border-b border-border pb-3">
                <div>
                  <h3 className="font-headline text-lg font-semibold">Cadangan Otomatis</h3>
                  <p className="text-body-sm text-text-secondary">Hanya admin yang dapat mengubah.</p>
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
                  <label className="label-field" htmlFor="bk-schedule">
                    Jadwal (cron)
                  </label>
                  <input
                    id="bk-schedule"
                    className="input mono"
                    placeholder="0 2 * * *"
                    value={schedule}
                    disabled={!isAdmin || busy}
                    onChange={(e) => setSchedule(e.target.value)}
                  />
                  <p className="mt-1 text-label-sm text-text-secondary">
                    Format 5 kolom: menit jam tanggal bulan hari. Contoh <code className="mono">0 2 * * *</code> = tiap
                    hari pukul 02:00 (waktu server).
                  </p>
                </div>

                <div>
                  <label className="label-field" htmlFor="bk-keep">
                    Simpan berkas (hari)
                  </label>
                  <input
                    id="bk-keep"
                    type="number"
                    min={0}
                    className="input"
                    value={keepDays}
                    disabled={!isAdmin || busy}
                    onChange={(e) => setKeepDays(Number(e.target.value) || 0)}
                  />
                  <p className="mt-1 text-label-sm text-text-secondary">
                    Berkas cadangan lebih tua dari ini akan dihapus. 0 = simpan selamanya.
                  </p>
                </div>
              </div>
            </section>

            <section className="card p-5">
              <div className="mb-4 flex items-center justify-between gap-3 border-b border-border pb-3">
                <div>
                  <h3 className="font-headline text-lg font-semibold">Unggah ke FTP</h3>
                  <p className="text-body-sm text-text-secondary">
                    Cadangan dikirim ke server FTP setelah dibuat.
                  </p>
                </div>
                <label className="flex cursor-pointer items-center gap-2">
                  <input
                    type="checkbox"
                    className="h-4 w-4"
                    checked={ftpEnabled}
                    disabled={!isAdmin || busy}
                    onChange={(e) => setFtpEnabled(e.target.checked)}
                  />
                  <span className="text-label-md font-semibold">{ftpEnabled ? 'Aktif' : 'Nonaktif'}</span>
                </label>
              </div>

              <div className="space-y-4">
                <div className="grid grid-cols-[minmax(0,3fr)_minmax(0,1fr)] gap-3">
                  <div>
                    <label className="label-field" htmlFor="ftp-host">
                      Host
                    </label>
                    <input
                      id="ftp-host"
                      className="input mono"
                      placeholder="ftp.example.com"
                      value={ftpHost}
                      disabled={!isAdmin || busy}
                      onChange={(e) => setFtpHost(e.target.value)}
                    />
                  </div>
                  <div>
                    <label className="label-field" htmlFor="ftp-port">
                      Port
                    </label>
                    <input
                      id="ftp-port"
                      type="number"
                      min={1}
                      max={65535}
                      className="input mono"
                      value={ftpPort}
                      disabled={!isAdmin || busy}
                      onChange={(e) => setFtpPort(Number(e.target.value) || 21)}
                    />
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="label-field" htmlFor="ftp-user">
                      Username
                    </label>
                    <input
                      id="ftp-user"
                      className="input"
                      value={ftpUser}
                      disabled={!isAdmin || busy}
                      onChange={(e) => setFtpUser(e.target.value)}
                    />
                  </div>
                  <div>
                    <label className="label-field" htmlFor="ftp-pass">
                      Password
                    </label>
                    <input
                      id="ftp-pass"
                      type="password"
                      className="input"
                      placeholder={cfg?.ftp?.password_set ? '•••• tersimpan' : ''}
                      value={ftpPass}
                      disabled={!isAdmin || busy}
                      onChange={(e) => setFtpPass(e.target.value)}
                    />
                  </div>
                </div>

                <div>
                  <label className="label-field" htmlFor="ftp-dir">
                    Direktori tujuan
                  </label>
                  <input
                    id="ftp-dir"
                    className="input mono"
                    placeholder="ingatin/backups"
                    value={ftpDir}
                    disabled={!isAdmin || busy}
                    onChange={(e) => setFtpDir(e.target.value)}
                  />
                  <p className="mt-1 text-label-sm text-text-secondary">
                    Dibuat otomatis bila belum ada. Kosongkan untuk direktori akar.
                  </p>
                </div>

                <div className="flex flex-wrap gap-4 border-t border-border pt-4">
                  <label className="flex cursor-pointer items-center gap-2">
                    <input
                      type="checkbox"
                      className="h-4 w-4"
                      checked={ftpPassive}
                      disabled={!isAdmin || busy}
                      onChange={(e) => setFtpPassive(e.target.checked)}
                    />
                    <span className="text-label-md">Mode pasif (PASV)</span>
                  </label>
                </div>

                <div className="flex flex-wrap gap-2">
                  <button className="btn-primary" disabled={!isAdmin || busy} onClick={saveConfig}>
                    {busy ? 'Menyimpan…' : 'Simpan Konfigurasi'}
                  </button>
                  <button className="btn-secondary" disabled={!isAdmin || busy} onClick={testFTP}>
                    <span className="material-symbols-outlined text-[18px]">cable</span>
                    Uji Koneksi FTP
                  </button>
                </div>

                {cfg?.last_run_at && (
                  <div className="rounded-control border border-border bg-surface-container-low p-3 text-label-sm">
                    <div className="kicker mb-1">Jadwal terakhir</div>
                    <div>
                      {formatWIB(cfg.last_run_at)} —{' '}
                      <span className={cfg.last_status === 'ok' ? 'text-success' : 'text-critical'}>
                        {cfg.last_status === 'ok' ? 'berhasil' : 'gagal'}
                      </span>
                      {cfg.last_file && <span className="text-text-secondary"> ({cfg.last_file})</span>}
                    </div>
                    {cfg.last_error && <div className="mt-1 text-critical">{cfg.last_error}</div>}
                  </div>
                )}

                {!isAdmin && (
                  <p className="text-label-sm text-warning">
                    Anda tidak memiliki izin untuk mengubah konfigurasi ini (hanya admin).
                  </p>
                )}
              </div>
            </section>
          </div>
          </div>
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
