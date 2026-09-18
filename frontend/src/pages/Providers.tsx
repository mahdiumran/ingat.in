import { useState } from 'react'
import { ApiUser, notificationApi, NotificationProviderRecord } from '../api'
import { EmptyState, ErrorState, LoadingBlock, Modal, PageHeader, formatWIB } from '../components/ui'
import { useAsync } from '../hooks'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

const KIND_LABEL: Record<string, string> = {
  waha: 'WAHA (WhatsApp self-host)',
  fonnte: 'Fonnte',
  wablas: 'Wablas',
  starsender: 'Starsender',
  custom_http: 'HTTP kustom',
  telegram_bot: 'Telegram Bot',
  smtp: 'SMTP',
  slack_webhook: 'Slack webhook',
  discord_webhook: 'Discord webhook',
  generic: 'Generik',
}

/**
 * Providers (F3) — konfigurasi kanal pengiriman.
 *
 * WAHA adalah kanal WhatsApp bawaan. Token Telegram dan API key WhatsApp
 * diisi di sini (tersimpan terenkripsi), sehingga perubahan tidak memerlukan
 * membangun ulang aplikasi.
 */
export default function Providers({ user, toast }: { user: ApiUser | null; toast: Toast }) {
  const providers = useAsync(() => notificationApi.providers(), [])
  const [showCreate, setShowCreate] = useState(false)
  const [editTarget, setEditTarget] = useState<NotificationProviderRecord | null>(null)
  const [testTarget, setTestTarget] = useState<NotificationProviderRecord | null>(null)
  const [sessionTarget, setSessionTarget] = useState<NotificationProviderRecord | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)

  const canWrite = !!user && (user.role === 'admin' || user.role === 'noc')
  const list = providers.data?.providers ?? []

  async function testConnection(p: NotificationProviderRecord) {
    setBusyId(p.id)
    try {
      const res = await notificationApi.testProvider(p.id)
      if (res.ok) {
        toast('success', 'Koneksi berhasil', p.label)
      } else {
        toast('error', 'Koneksi gagal', res.error)
      }
      providers.reload()
    } catch (err) {
      toast('error', 'Gagal menguji koneksi', err instanceof Error ? err.message : undefined)
    } finally {
      setBusyId(null)
    }
  }

  async function makeDefault(p: NotificationProviderRecord) {
    setBusyId(p.id)
    try {
      await notificationApi.updateProvider(p.id, { is_default: true })
      toast('success', 'Provider default diperbarui', p.label)
      providers.reload()
    } catch (err) {
      toast('error', 'Gagal mengubah default', err instanceof Error ? err.message : undefined)
    } finally {
      setBusyId(null)
    }
  }

  async function toggleActive(p: NotificationProviderRecord) {
    setBusyId(p.id)
    try {
      await notificationApi.updateProvider(p.id, { is_active: !p.is_active })
      toast('success', p.is_active ? 'Provider dinonaktifkan' : 'Provider diaktifkan', p.label)
      providers.reload()
    } catch (err) {
      toast('error', 'Gagal mengubah status', err instanceof Error ? err.message : undefined)
    } finally {
      setBusyId(null)
    }
  }

  async function remove(p: NotificationProviderRecord) {
    if (!window.confirm(`Hapus provider "${p.label}"? Binding yang memakainya akan memakai default kanal.`)) return
    setBusyId(p.id)
    try {
      await notificationApi.deleteProvider(p.id)
      toast('success', 'Provider dihapus', p.label)
      providers.reload()
    } catch (err) {
      toast('error', 'Gagal menghapus provider', err instanceof Error ? err.message : undefined)
    } finally {
      setBusyId(null)
    }
  }

  const hasTelegram = list.some((p) => p.channel === 'telegram' && p.is_active)
  const hasWhatsApp = list.some((p) => p.channel === 'whatsapp' && p.is_active)

  return (
    <div>
      <PageHeader
        kicker="Notifikasi"
        title="Providers"
        description="Konfigurasi kanal pengiriman. API key disimpan terenkripsi (AES-256-GCM)."
        actions={
          <>
            <button className="btn-secondary" onClick={providers.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            {canWrite && (
              <button className="btn-primary" onClick={() => setShowCreate(true)}>
                <span className="material-symbols-outlined text-[18px]">add</span>
                Provider Baru
              </button>
            )}
          </>
        }
      />

      {/* Status kesiapan kanal */}
      <div className="mb-5 grid gap-3 sm:grid-cols-2">
        <ReadinessCard
          icon="send"
          label="Telegram"
          ready={hasTelegram}
          hint={
            hasTelegram
              ? 'Provider Telegram aktif.'
              : 'Belum ada provider Telegram aktif. Tambahkan dengan token bot dari @BotFather.'
          }
        />
        <ReadinessCard
          icon="chat"
          label="WhatsApp"
          ready={hasWhatsApp}
          hint={
            hasWhatsApp
              ? 'Provider WhatsApp aktif.'
              : 'Belum ada provider WhatsApp aktif. WAHA sudah berjalan — tambahkan provider dan scan QR.'
          }
        />
      </div>

      {providers.loading && <LoadingBlock />}
      {providers.error && <ErrorState message={providers.error} onRetry={providers.reload} />}
      {!providers.loading && !providers.error && list.length === 0 && (
        <div className="card">
          <EmptyState
            icon="hub"
            title="Belum ada provider"
            body="Tambahkan provider WhatsApp (WAHA) dan/atau Telegram agar notifikasi dapat dikirim."
            action={
              canWrite ? (
                <button className="btn-primary" onClick={() => setShowCreate(true)}>
                  <span className="material-symbols-outlined text-[18px]">add</span>
                  Tambah Provider
                </button>
              ) : undefined
            }
          />
        </div>
      )}

      <div className="space-y-4">
        {list.map((p) => (
          <section key={p.id} className="card">
            <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border px-5 py-3.5">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <h3 className="font-headline text-lg font-semibold">{p.label}</h3>
                  <span className="badge bg-surface-container text-text-secondary border-outline-variant">
                    {p.channel}
                  </span>
                  {p.is_default && (
                    <span className="badge bg-primary-container text-on-primary-container border-outline-variant">
                      default
                    </span>
                  )}
                  <span
                    className={`badge ${
                      p.is_active
                        ? 'bg-success-container text-on-success-container border-outline-variant'
                        : 'bg-surface-container text-text-secondary border-outline-variant'
                    }`}
                  >
                    {p.is_active ? 'aktif' : 'nonaktif'}
                  </span>
                </div>
                <p className="mt-0.5 text-body-sm text-text-secondary">{KIND_LABEL[p.kind] ?? p.kind}</p>
              </div>

              <div className="flex flex-wrap gap-2">
                {canWrite && (
                  <>
                    <button
                      className="btn-secondary"
                      disabled={busyId === p.id}
                      onClick={() => testConnection(p)}
                    >
                      <span className="material-symbols-outlined text-[17px]">
                        {busyId === p.id ? 'progress_activity' : 'network_check'}
                      </span>
                      Test Koneksi
                    </button>
                    <button className="btn-secondary" onClick={() => setTestTarget(p)}>
                      <span className="material-symbols-outlined text-[17px]">send</span>
                      Test Kirim
                    </button>
                    {p.kind === 'waha' && (
                      <button className="btn-secondary" onClick={() => setSessionTarget(p)}>
                        <span className="material-symbols-outlined text-[17px]">qr_code</span>
                        Sesi & QR
                      </button>
                    )}
                    <button className="btn-secondary" onClick={() => setEditTarget(p)}>
                      <span className="material-symbols-outlined text-[17px]">edit</span>
                      Ubah
                    </button>
                  </>
                )}
              </div>
            </div>

            <div className="grid gap-4 px-5 py-4 sm:grid-cols-3">
              <Field label="Base URL" value={p.base_url || '—'} mono />
              <Field label="API key" value={p.has_api_key ? '•••••••• (tersimpan)' : 'belum diisi'} />
              <Field
                label="Uji terakhir"
                value={
                  p.last_test_at
                    ? `${formatWIB(p.last_test_at)} · ${p.last_test_ok ? 'berhasil' : 'gagal'}`
                    : 'belum pernah'
                }
              />
            </div>

            {p.last_test_error && (
              <div className="mx-5 mb-4 flex items-start gap-2 rounded-control border border-critical/30 bg-critical-container/50 p-3 text-body-sm text-on-critical-container">
                <span className="material-symbols-outlined text-[17px] shrink-0">error</span>
                <span className="mono break-all text-label-sm">{p.last_test_error}</span>
              </div>
            )}

            {canWrite && (
              <div className="flex flex-wrap gap-2 border-t border-border px-5 py-3">
                {!p.is_default && p.is_active && (
                  <button className="btn-ghost text-label-md" disabled={busyId === p.id} onClick={() => makeDefault(p)}>
                    <span className="material-symbols-outlined text-[17px]">star</span>
                    Jadikan default kanal
                  </button>
                )}
                <button className="btn-ghost text-label-md" disabled={busyId === p.id} onClick={() => toggleActive(p)}>
                  <span className="material-symbols-outlined text-[17px]">{p.is_active ? 'block' : 'check_circle'}</span>
                  {p.is_active ? 'Nonaktifkan' : 'Aktifkan'}
                </button>
                <button className="btn-ghost text-label-md text-critical" disabled={busyId === p.id} onClick={() => remove(p)}>
                  <span className="material-symbols-outlined text-[17px]">delete</span>
                  Hapus
                </button>
              </div>
            )}
          </section>
        ))}
      </div>

      {showCreate && (
        <ProviderForm
          kindsByChannel={providers.data?.kinds_by_channel ?? {}}
          onClose={() => setShowCreate(false)}
          onSaved={() => {
            setShowCreate(false)
            toast('success', 'Provider dibuat', 'Jalankan Test Koneksi untuk memverifikasi.')
            providers.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membuat provider', msg)}
        />
      )}

      {editTarget && (
        <ProviderForm
          existing={editTarget}
          kindsByChannel={providers.data?.kinds_by_channel ?? {}}
          onClose={() => setEditTarget(null)}
          onSaved={() => {
            setEditTarget(null)
            toast('success', 'Provider diperbarui')
            providers.reload()
          }}
          onError={(msg) => toast('error', 'Gagal memperbarui provider', msg)}
        />
      )}

      {testTarget && (
        <SendTestForm
          provider={testTarget}
          onClose={() => setTestTarget(null)}
          toast={toast}
        />
      )}

      {sessionTarget && (
        <SessionPanel
          provider={sessionTarget}
          onClose={() => setSessionTarget(null)}
          toast={toast}
        />
      )}
    </div>
  )
}

function ReadinessCard({
  icon,
  label,
  ready,
  hint,
}: {
  icon: string
  label: string
  ready: boolean
  hint: string
}) {
  return (
    <div className="card px-4 py-3.5">
      <div className="flex items-start gap-3">
        <span
          className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border ${
            ready ? 'border-outline-variant bg-success-container' : 'border-outline-variant bg-surface-container'
          }`}
        >
          <span className="material-symbols-outlined text-[20px]">{icon}</span>
        </span>
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-semibold">{label}</span>
            <span
              className={`badge ${
                ready
                  ? 'bg-success-container text-on-success-container border-outline-variant'
                  : 'bg-warning-container text-on-warning-container border-outline-variant'
              }`}
            >
              {ready ? 'siap' : 'belum siap'}
            </span>
          </div>
          <p className="mt-0.5 text-body-sm text-text-secondary">{hint}</p>
        </div>
      </div>
    </div>
  )
}

function Field({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <div className="kicker">{label}</div>
      <div className={`mt-0.5 break-all text-body-sm ${mono ? 'mono text-label-md' : ''}`}>{value}</div>
    </div>
  )
}

function ProviderForm({
  existing,
  kindsByChannel,
  onClose,
  onSaved,
  onError,
}: {
  existing?: NotificationProviderRecord
  kindsByChannel: Record<string, string[]>
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const isEdit = !!existing
  const [channel, setChannel] = useState(existing?.channel ?? 'whatsapp')
  const [kind, setKind] = useState(existing?.kind ?? 'waha')
  const [label, setLabel] = useState(existing?.label ?? '')
  const [baseUrl, setBaseUrl] = useState(existing?.base_url ?? '')
  const [apiKey, setApiKey] = useState('')
  const [session, setSession] = useState(
    String((existing?.extra as { session?: string } | undefined)?.session ?? ''),
  )
  const [isDefault, setIsDefault] = useState(existing?.is_default ?? false)
  const [busy, setBusy] = useState(false)

  const kindOptions = kindsByChannel[channel] ?? []

  // Nilai default yang membantu operator.
  function applyKindDefaults(k: string) {
    setKind(k)
    if (!baseUrl || isEdit === false) {
      if (k === 'telegram_bot') setBaseUrl('https://api.telegram.org')
      else if (k === 'waha') setBaseUrl('http://127.0.0.1:8082')
      else if (k === 'fonnte') setBaseUrl('https://api.fonnte.com/send')
      else if (k === 'wablas') setBaseUrl('https://console.wablas.com/api/send-message')
      else if (k === 'starsender') setBaseUrl('https://api.starsender.online/api/send')
    }
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const extra: Record<string, unknown> = {}
      if (kind === 'waha' && session) extra.session = session

      if (isEdit && existing) {
        const payload: Record<string, unknown> = {
          label,
          base_url: baseUrl,
          is_default: isDefault,
          extra: Object.keys(extra).length ? extra : undefined,
        }
        // Hanya kirim api_key bila diisi (kosong = jangan ubah).
        if (apiKey) payload.api_key = apiKey
        await notificationApi.updateProvider(existing.id, payload)
      } else {
        await notificationApi.createProvider({
          channel,
          kind,
          label,
          base_url: baseUrl,
          api_key: apiKey,
          extra,
          is_default: isDefault,
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
      title={isEdit ? `Ubah Provider — ${existing?.label}` : 'Provider Baru'}
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !label.trim()}>
            {busy ? 'Menyimpan…' : isEdit ? 'Simpan Perubahan' : 'Buat Provider'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        {!isEdit && (
          <div className="grid gap-3.5 sm:grid-cols-2">
            <div>
              <label className="label-field" htmlFor="pv-channel">
                Kanal
              </label>
              <select
                id="pv-channel"
                className="input"
                value={channel}
                onChange={(e) => {
                  setChannel(e.target.value)
                  const first = kindsByChannel[e.target.value]?.[0] ?? 'generic'
                  applyKindDefaults(first)
                }}
              >
                <option value="whatsapp">WhatsApp</option>
                <option value="telegram">Telegram</option>
              </select>
            </div>
            <div>
              <label className="label-field" htmlFor="pv-kind">
                Jenis provider
              </label>
              <select id="pv-kind" className="input" value={kind} onChange={(e) => applyKindDefaults(e.target.value)}>
                {kindOptions.map((k) => (
                  <option key={k} value={k}>
                    {KIND_LABEL[k] ?? k}
                  </option>
                ))}
              </select>
            </div>
          </div>
        )}

        <div>
          <label className="label-field" htmlFor="pv-label">
            Label
          </label>
          <input
            id="pv-label"
            className="input"
            required
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="mis. WAHA Utama"
          />
        </div>

        <div>
          <label className="label-field" htmlFor="pv-base">
            Base URL
          </label>
          <input
            id="pv-base"
            className="input mono"
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder="http://127.0.0.1:8082"
          />
        </div>

        <div>
          <label className="label-field" htmlFor="pv-key">
            API key / token {isEdit && <span className="normal-case">(kosongkan bila tidak ingin mengubah)</span>}
          </label>
          <input
            id="pv-key"
            type="password"
            className="input mono"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder={isEdit ? 'biarkan kosong untuk mempertahankan' : 'token bot Telegram / API key WAHA'}
            autoComplete="new-password"
          />
          <p className="mt-1 text-label-sm text-text-secondary">
            Disimpan terenkripsi AES-256-GCM dan tidak pernah ditampilkan kembali.
          </p>
        </div>

        {kind === 'waha' && (
          <div>
            <label className="label-field" htmlFor="pv-session">
              Nama sesi WAHA
            </label>
            <input
              id="pv-session"
              className="input mono"
              value={session}
              onChange={(e) => setSession(e.target.value)}
              placeholder="default"
            />
          </div>
        )}

        <label className="flex cursor-pointer items-center gap-2 text-body-sm text-text-secondary">
          <input
            type="checkbox"
            className="h-4 w-4 rounded border-border text-primary focus:ring-primary/30"
            checked={isDefault}
            onChange={(e) => setIsDefault(e.target.checked)}
          />
          Jadikan provider default untuk kanal ini
        </label>
      </form>
    </Modal>
  )
}

function SendTestForm({
  provider,
  onClose,
  toast,
}: {
  provider: NotificationProviderRecord
  onClose: () => void
  toast: Toast
}) {
  const [destination, setDestination] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; error?: string; message_id?: string } | null>(null)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    setResult(null)
    try {
      const res = await notificationApi.sendTestMessage(provider.id, destination, message)
      setResult(res)
      if (res.ok) {
        toast('success', 'Pesan uji terkirim', `ID: ${res.message_id || '—'}`)
      } else {
        toast('error', 'Pesan uji gagal', res.error)
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Terjadi kesalahan'
      setResult({ ok: false, error: msg })
      toast('error', 'Gagal mengirim pesan uji', msg)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title={`Test Kirim — ${provider.label}`}
      width="sm"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Tutup
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !destination.trim()}>
            {busy ? 'Mengirim…' : 'Kirim Pesan Uji'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <div>
          <label className="label-field" htmlFor="st-dest">
            Tujuan
          </label>
          <input
            id="st-dest"
            className="input mono"
            required
            autoFocus
            value={destination}
            onChange={(e) => setDestination(e.target.value)}
            placeholder={provider.channel === 'telegram' ? '-1001234567890' : '6281234567890'}
          />
        </div>

        <div>
          <label className="label-field" htmlFor="st-msg">
            Pesan (opsional)
          </label>
          <textarea
            id="st-msg"
            className="input h-20 py-2"
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            placeholder="biarkan kosong untuk pesan uji bawaan"
          />
        </div>

        {result && (
          <div
            className={`flex items-start gap-2 rounded-control border p-3 text-body-sm ${
              result.ok
                ? 'border-outline-variant bg-success-container text-on-success-container'
                : 'border-critical/30 bg-critical-container text-on-critical-container'
            }`}
          >
            <span className="material-symbols-outlined text-[18px] shrink-0">
              {result.ok ? 'check_circle' : 'error'}
            </span>
            <div className="min-w-0">
              <div className="font-semibold">{result.ok ? 'Terkirim' : 'Gagal'}</div>
              {result.message_id && <div className="mono break-all text-label-sm">id: {result.message_id}</div>}
              {result.error && <div className="mono break-all text-label-sm">{result.error}</div>}
            </div>
          </div>
        )}
      </form>
    </Modal>
  )
}

function SessionPanel({
  provider,
  onClose,
  toast,
}: {
  provider: NotificationProviderRecord
  onClose: () => void
  toast: Toast
}) {
  const session = useAsync(() => notificationApi.providerSession(provider.id), [provider.id])
  const [busy, setBusy] = useState(false)

  async function act(action: 'start' | 'stop' | 'logout') {
    setBusy(true)
    try {
      await notificationApi.providerSessionAction(provider.id, action)
      toast('success', `Aksi ${action} berhasil`)
      session.reload()
    } catch (err) {
      toast('error', `Gagal menjalankan ${action}`, err instanceof Error ? err.message : undefined)
    } finally {
      setBusy(false)
    }
  }

  const data = session.data
  const sessionsList = (data?.sessions ?? []) as { name?: string; status?: string }[]
  const currentStatus = sessionsList.find((s) => s.name === 'default')?.status ?? sessionsList[0]?.status

  const statusTone: Record<string, string> = {
    WORKING: 'bg-success-container text-on-success-container border-outline-variant',
    SCAN_QR_CODE: 'bg-warning-container text-on-warning-container border-outline-variant',
    STARTING: 'bg-info-container text-on-info-container border-outline-variant',
    STOPPED: 'bg-surface-container text-text-secondary border-outline-variant',
    FAILED: 'bg-critical-container text-on-critical-container border-critical',
  }

  return (
    <Modal
      title={`Sesi WAHA — ${provider.label}`}
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Tutup
          </button>
          <button className="btn-secondary" onClick={() => session.reload()} disabled={busy}>
            <span className="material-symbols-outlined text-[17px]">refresh</span>
            Muat ulang
          </button>
        </>
      }
    >
      {session.loading && <LoadingBlock label="Memeriksa sesi…" />}
      {session.error && <ErrorState message={session.error} onRetry={session.reload} />}

      {data && (
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-body-sm text-text-secondary">Status:</span>
            <span className={`badge ${statusTone[currentStatus ?? ''] ?? ''}`}>
              {currentStatus ?? 'tidak diketahui'}
            </span>
            <span
              className={`badge ${
                data.reachable
                  ? 'bg-success-container text-on-success-container border-outline-variant'
                  : 'bg-critical-container text-on-critical-container border-critical'
              }`}
            >
              {data.reachable ? 'WAHA terjangkau' : 'WAHA tidak terjangkau'}
            </span>
          </div>

          {currentStatus === 'SCAN_QR_CODE' && (
            <div className="flex gap-2.5 rounded-control border border-outline-variant bg-warning-container/70 p-3.5 text-body-sm text-on-warning-container">
              <span className="material-symbols-outlined text-[19px] shrink-0">qr_code_2</span>
              <div>
                Sesi menunggu scan QR. Buka dashboard WAHA, lalu tautkan perangkat dari WhatsApp
                (Perangkat Tertaut → Tautkan Perangkat).
              </div>
            </div>
          )}

          {data.error && (
            <div className="flex gap-2 rounded-control border border-critical/30 bg-critical-container/50 p-3 text-body-sm text-on-critical-container">
              <span className="material-symbols-outlined text-[17px] shrink-0">error</span>
              <span className="mono break-all text-label-sm">{data.error}</span>
            </div>
          )}

          {data.qr_url && (
            <div>
              <div className="kicker">Dashboard QR</div>
              <a
                href={data.qr_url}
                target="_blank"
                rel="noopener noreferrer"
                className="mono mt-0.5 block break-all text-label-md text-primary hover:underline"
              >
                {data.qr_url}
              </a>
              <p className="mt-1 text-label-sm text-text-secondary">
                Halaman ini dilindungi basic auth dan hanya dapat diakses dari LAN.
              </p>
            </div>
          )}

          <div className="flex flex-wrap gap-2 border-t border-border pt-3">
            <button className="btn-secondary" disabled={busy} onClick={() => act('start')}>
              <span className="material-symbols-outlined text-[17px]">play_arrow</span>
              Start
            </button>
            <button className="btn-secondary" disabled={busy} onClick={() => act('stop')}>
              <span className="material-symbols-outlined text-[17px]">stop</span>
              Stop
            </button>
            <button className="btn-danger" disabled={busy} onClick={() => act('logout')}>
              <span className="material-symbols-outlined text-[17px]">logout</span>
              Logout (perlu scan ulang)
            </button>
          </div>
        </div>
      )}
    </Modal>
  )
}
