import { useState } from 'react'
import { ApiUser, Binding, notificationApi, NotificationProviderRecord, Target } from '../api'
import { EmptyState, ErrorState, LoadingBlock, Modal, PageHeader } from '../components/ui'
import { useAsync } from '../hooks'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

const CHANNEL_ICON: Record<string, string> = {
  whatsapp: 'chat',
  telegram: 'send',
  email: 'mail',
  sms: 'sms',
  slack: 'tag',
  discord: 'forum',
  webhook: 'webhook',
}

const CHANNEL_TONE: Record<string, string> = {
  whatsapp: 'bg-success-container text-on-success-container border-outline-variant',
  telegram: 'bg-info-container text-on-info-container border-outline-variant',
  email: 'bg-surface-container text-text-secondary border-outline-variant',
  sms: 'bg-surface-container text-text-secondary border-outline-variant',
  slack: 'bg-warning-container text-on-warning-container border-outline-variant',
  discord: 'bg-surface-container text-text-secondary border-outline-variant',
  webhook: 'bg-surface-container text-text-secondary border-outline-variant',
}

/**
 * Targets (F4) — grup/tim atau personal penerima notifikasi, beserta binding
 * per kanal. Setiap binding dapat diuji kirim dan ditandai terverifikasi.
 */
export default function Targets({ user, toast }: { user: ApiUser | null; toast: Toast }) {
  const targets = useAsync(() => notificationApi.targets(), [])
  const providers = useAsync(() => notificationApi.providers(), [])

  const [showCreate, setShowCreate] = useState(false)
  const [bindingTarget, setBindingTarget] = useState<Target | null>(null)

  const canWrite = !!user && (user.role === 'admin' || user.role === 'noc')

  async function deleteTarget(t: Target) {
    if (!window.confirm(`Hapus target "${t.name}" beserta seluruh binding-nya?`)) return
    try {
      await notificationApi.deleteTarget(t.id)
      toast('success', 'Target dihapus', t.name)
      targets.reload()
    } catch (err) {
      toast('error', 'Gagal menghapus target', err instanceof Error ? err.message : undefined)
    }
  }

  async function testBinding(b: Binding) {
    try {
      const res = await notificationApi.testBinding(b.id)
      if (res.ok) {
        toast('success', 'Pesan uji terkirim', `${b.channel} → ${b.destination}`)
      } else {
        toast('error', 'Pesan uji gagal', res.error)
      }
      targets.reload()
    } catch (err) {
      toast('error', 'Gagal mengirim pesan uji', err instanceof Error ? err.message : undefined)
    }
  }

  async function deleteBinding(b: Binding) {
    if (!window.confirm(`Hapus binding ${b.channel} → ${b.destination}?`)) return
    try {
      await notificationApi.deleteBinding(b.id)
      toast('success', 'Binding dihapus')
      targets.reload()
    } catch (err) {
      toast('error', 'Gagal menghapus binding', err instanceof Error ? err.message : undefined)
    }
  }

  const list = targets.data?.targets ?? []
  const unverified = list.reduce(
    (n, t) => n + (t.bindings ?? []).filter((b) => b.is_active && !b.verified_at).length,
    0,
  )

  return (
    <div>
      <PageHeader
        kicker="Notifikasi"
        title="Notification Targets"
        description="Tujuan notifikasi: grup/tim atau personal, dengan binding per kanal."
        actions={
          <>
            <button className="btn-secondary" onClick={targets.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            {canWrite && (
              <button className="btn-primary" onClick={() => setShowCreate(true)}>
                <span className="material-symbols-outlined text-[18px]">group_add</span>
                Target Baru
              </button>
            )}
          </>
        }
      />

      {unverified > 0 && (
        <div className="mb-4 flex items-start gap-2.5 rounded-card border border-outline-variant bg-warning-container/70 p-3.5 text-body-sm text-on-warning-container">
          <span className="material-symbols-outlined text-[19px] shrink-0">warning</span>
          <div>
            <strong>{unverified} binding belum terverifikasi.</strong> Gunakan tombol{' '}
            <em>Kirim Uji</em> pada tiap binding untuk memastikan nomor/chat id benar sebelum
            mengandalkannya untuk notifikasi produksi.
          </div>
        </div>
      )}

      {targets.loading && <LoadingBlock />}
      {targets.error && <ErrorState message={targets.error} onRetry={targets.reload} />}
      {!targets.loading && !targets.error && list.length === 0 && (
        <div className="card">
          <EmptyState
            icon="group"
            title="Belum ada target notifikasi"
            body="Buat target (mis. NOC-Team) lalu tambahkan binding Telegram dan/atau WhatsApp."
          />
        </div>
      )}

      <div className="space-y-4">
        {list.map((t) => (
          <section key={t.id} className="card">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3.5">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <h3 className="font-headline text-lg font-semibold">{t.name}</h3>
                  <span className="badge bg-surface-container text-text-secondary border-outline-variant">{t.kind}</span>
                  <span
                    className={`badge ${
                      t.is_active
                        ? 'bg-success-container text-on-success-container border-outline-variant'
                        : 'bg-surface-container text-text-secondary border-outline-variant'
                    }`}
                  >
                    {t.is_active ? 'aktif' : 'nonaktif'}
                  </span>
                </div>
                {t.notes && <p className="mt-0.5 text-body-sm text-text-secondary">{t.notes}</p>}
              </div>
              <div className="flex flex-wrap gap-2">
                {canWrite && (
                  <button className="btn-secondary" onClick={() => setBindingTarget(t)}>
                    <span className="material-symbols-outlined text-[17px]">add_link</span>
                    Binding
                  </button>
                )}
                {canWrite && (
                  <button className="btn-danger" onClick={() => deleteTarget(t)}>
                    <span className="material-symbols-outlined text-[17px]">delete</span>
                    Hapus
                  </button>
                )}
              </div>
            </div>

            {(t.bindings ?? []).length === 0 ? (
              <div className="px-5 py-8 text-center">
                <p className="text-body-sm text-text-secondary">
                  Belum ada binding. Target tanpa binding aktif tidak akan menerima notifikasi.
                </p>
                {canWrite && (
                  <button className="btn-primary mt-3" onClick={() => setBindingTarget(t)}>
                    <span className="material-symbols-outlined text-[17px]">add_link</span>
                    Tambah Binding
                  </button>
                )}
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="table">
                  <thead>
                    <tr>
                      <th>Kanal</th>
                      <th>Destination</th>
                      <th>Provider</th>
                      <th>Status</th>
                      <th>Verifikasi</th>
                      <th className="text-right">Aksi</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(t.bindings ?? []).map((b) => {
                      const provider = (providers.data?.providers ?? []).find(
                        (p: NotificationProviderRecord) => p.id === b.provider_id,
                      )
                      return (
                        <tr key={b.id}>
                          <td>
                            <span className={`badge ${CHANNEL_TONE[b.channel] ?? ''}`}>
                              <span className="material-symbols-outlined text-[13px]">
                                {CHANNEL_ICON[b.channel] ?? 'link'}
                              </span>
                              {b.channel}
                            </span>
                          </td>
                          <td className="mono text-label-md">{b.destination}</td>
                          <td className="text-text-secondary">
                            {provider ? provider.label : <em>default kanal</em>}
                          </td>
                          <td>
                            <span
                              className={`badge ${
                                b.is_active
                                  ? 'bg-success-container text-on-success-container border-outline-variant'
                                  : 'bg-surface-container text-text-secondary border-outline-variant'
                              }`}
                            >
                              {b.is_active ? 'aktif' : 'nonaktif'}
                            </span>
                          </td>
                          <td>
                            {b.verified_at ? (
                              <span className="badge bg-success-container text-on-success-container border-outline-variant">
                                terverifikasi
                              </span>
                            ) : (
                              <span className="badge bg-warning-container text-on-warning-container border-outline-variant">
                                belum diuji
                              </span>
                            )}
                          </td>
                          <td>
                            <div className="flex justify-end gap-1">
                              {canWrite && (
                                <>
                                  <button
                                    className="btn-ghost h-8 px-2 text-label-md"
                                    title="Kirim pesan uji"
                                    onClick={() => testBinding(b)}
                                  >
                                    <span className="material-symbols-outlined text-[17px]">send</span>
                                    Kirim Uji
                                  </button>
                                  <button
                                    className="btn-ghost h-8 w-8 px-0 text-critical"
                                    title="Hapus binding"
                                    onClick={() => deleteBinding(b)}
                                  >
                                    <span className="material-symbols-outlined text-[17px]">delete</span>
                                  </button>
                                </>
                              )}
                            </div>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        ))}
      </div>

      {showCreate && (
        <TargetForm
          onClose={() => setShowCreate(false)}
          onSaved={() => {
            setShowCreate(false)
            toast('success', 'Target dibuat', 'Tambahkan binding agar dapat menerima notifikasi.')
            targets.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membuat target', msg)}
        />
      )}

      {bindingTarget && (
        <BindingForm
          target={bindingTarget}
          providers={providers.data?.providers ?? []}
          onClose={() => setBindingTarget(null)}
          onSaved={() => {
            setBindingTarget(null)
            toast('success', 'Binding ditambahkan', 'Gunakan Kirim Uji untuk memverifikasi.')
            targets.reload()
          }}
          onError={(msg) => toast('error', 'Gagal menambah binding', msg)}
        />
      )}
    </div>
  )
}

function TargetForm({
  onClose,
  onSaved,
  onError,
}: {
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const [name, setName] = useState('')
  const [kind, setKind] = useState('group')
  const [notes, setNotes] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await notificationApi.createTarget({ name, kind, notes })
      onSaved()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Terjadi kesalahan')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title="Target Notifikasi Baru"
      width="sm"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !name.trim()}>
            {busy ? 'Menyimpan…' : 'Buat Target'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <div>
          <label className="label-field" htmlFor="tg-name">
            Nama target
          </label>
          <input
            id="tg-name"
            className="input"
            required
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="mis. NOC-Team"
          />
        </div>
        <div>
          <label className="label-field" htmlFor="tg-kind">
            Jenis
          </label>
          <select id="tg-kind" className="input" value={kind} onChange={(e) => setKind(e.target.value)}>
            <option value="group">Grup / tim</option>
            <option value="personal">Personal</option>
          </select>
        </div>
        <div>
          <label className="label-field" htmlFor="tg-notes">
            Catatan
          </label>
          <textarea
            id="tg-notes"
            className="input h-16 py-2"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            placeholder="Siapa saja yang menerima, kapan dipakai, dsb."
          />
        </div>
      </form>
    </Modal>
  )
}

function BindingForm({
  target,
  providers,
  onClose,
  onSaved,
  onError,
}: {
  target: Target
  providers: NotificationProviderRecord[]
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const [channel, setChannel] = useState('telegram')
  const [destination, setDestination] = useState('')
  const [providerId, setProviderId] = useState('')
  const [label, setLabel] = useState('')
  const [busy, setBusy] = useState(false)

  // Provider yang cocok dengan kanal terpilih.
  const candidates = providers.filter((p) => p.channel === channel && p.is_active)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await notificationApi.createBinding(target.id, {
        channel,
        destination,
        provider_id: providerId || undefined,
        label,
      })
      onSaved()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Terjadi kesalahan')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title={`Binding Baru — ${target.name}`}
      width="sm"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !destination.trim()}>
            {busy ? 'Menyimpan…' : 'Tambah Binding'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <div>
          <label className="label-field" htmlFor="bd-channel">
            Kanal
          </label>
          <select
            id="bd-channel"
            className="input"
            value={channel}
            onChange={(e) => {
              setChannel(e.target.value)
              setProviderId('')
              setDestination('')
            }}
          >
            <option value="telegram">Telegram</option>
            <option value="whatsapp">WhatsApp</option>
          </select>
        </div>

        <div>
          <label className="label-field" htmlFor="bd-dest">
            Destination
          </label>
          <input
            id="bd-dest"
            className={`input mono ${channel === 'whatsapp' ? '' : ''}`}
            required
            value={destination}
            onChange={(e) => setDestination(e.target.value)}
            placeholder={
              channel === 'telegram'
                ? '-1001234567890 atau @channelusername'
                : '6281234567890 atau 120363000000000000@g.us'
            }
          />
          <p className="mt-1 text-label-sm text-text-secondary">
            {channel === 'telegram'
              ? 'Chat ID grup dapat dilihat dengan mengirim pesan ke bot lalu memeriksa getUpdates.'
              : 'Nomor format internasional. Awalan 08 akan dinormalisasi otomatis menjadi 628.'}
          </p>
        </div>

        <div>
          <label className="label-field" htmlFor="bd-provider">
            Provider
          </label>
          <select id="bd-provider" className="input" value={providerId} onChange={(e) => setProviderId(e.target.value)}>
            <option value="">— Default kanal —</option>
            {candidates.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label} ({p.kind}){p.is_default ? ' · default' : ''}
              </option>
            ))}
          </select>
          {candidates.length === 0 && (
            <p className="mt-1 text-label-sm text-warning">
              Belum ada provider aktif untuk kanal ini. Tambahkan di halaman Providers terlebih dahulu.
            </p>
          )}
        </div>

        <div>
          <label className="label-field" htmlFor="bd-label">
            Label
          </label>
          <input
            id="bd-label"
            className="input"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="mis. Grup NOC Utama"
          />
        </div>
      </form>
    </Modal>
  )
}
