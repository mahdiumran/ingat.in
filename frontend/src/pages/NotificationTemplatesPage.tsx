import { useMemo, useState } from 'react'
import { ApiUser, notificationApi, NotificationTemplate } from '../api'
import { EmptyState, ErrorState, LoadingBlock, Modal, PageHeader } from '../components/ui'
import { useAsync } from '../hooks'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

/** Kanal yang dapat diedit dari halaman ini. */
const EDITABLE_CHANNELS = ['telegram', 'whatsapp'] as const

const CHANNEL_LABEL: Record<string, string> = {
  telegram: 'Telegram',
  whatsapp: 'WhatsApp',
  smtp: 'SMTP',
  slack: 'Slack',
  discord: 'Discord',
}

const SEVERITY_TONE: Record<string, string> = {
  info: 'bg-info-container text-on-info-container border-outline-variant',
  warning: 'bg-warning-container text-on-warning-container border-outline-variant',
  critical: 'bg-critical-container text-on-critical-container border-critical',
}

/**
 * NotificationTemplatesPage — editor template notifikasi Telegram & WhatsApp.
 *
 * Hanya admin yang boleh membuka & mengubah (dijaga juga di backend:
 * PATCH /api/templates/{id} berada di grup requireAdmin). Halaman ini dibuka
 * dari kotak "Notifikasi Telegram & WA" pada hub Master Data.
 */
export default function NotificationTemplatesPage({
  user,
  toast,
  onBack,
}: {
  user: ApiUser | null
  toast: Toast
  onBack: () => void
}) {
  // Backend sudah menolak non-admin, tetapi UI tetap menyembunyikan aksi tulis.
  const isAdmin = user?.role === 'admin'

  const [channel, setChannel] = useState<string>('telegram')
  const [editing, setEditing] = useState<NotificationTemplate | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)

  const data = useAsync(() => notificationApi.templates(), [])

  const templates = useMemo(() => {
    const all = data.data?.templates ?? []
    // Tampilkan template kanal terpilih, plus baris channel NULL (berlaku semua)
    // yang relevan sebagai fallback.
    return all.filter((t) => t.channel === channel || t.channel === null || t.channel === '')
  }, [data.data, channel])

  const grouped = useMemo(() => {
    const map = new Map<string, NotificationTemplate[]>()
    for (const t of templates) {
      const arr = map.get(t.key) ?? []
      arr.push(t)
      map.set(t.key, arr)
    }
    return Array.from(map.entries())
  }, [templates])

  async function toggleActive(tpl: NotificationTemplate) {
    setBusyId(tpl.id)
    try {
      await notificationApi.updateTemplate(tpl.id, { is_active: !tpl.is_active })
      toast('success', tpl.is_active ? 'Template dinonaktifkan' : 'Template diaktifkan', tpl.key)
      data.reload()
    } catch (err) {
      toast('error', 'Gagal mengubah status template', err instanceof Error ? err.message : undefined)
    } finally {
      setBusyId(null)
    }
  }

  return (
    <div>
      <PageHeader
        kicker="Master Data"
        title="Notifikasi Telegram & WA"
        description="Sunting isi pesan notifikasi per kanal. Placeholder memakai kurung kurawal, mis. {{.RefNo}}, {{.Title}}."
        actions={
          <>
            <button className="btn-secondary" onClick={onBack}>
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

      {!isAdmin && (
        <div className="mb-4 flex items-start gap-2 rounded-control border border-outline-variant bg-warning-container/60 p-3.5 text-body-sm text-on-warning-container">
          <span className="material-symbols-outlined text-[19px] shrink-0">lock</span>
          <div>Hanya administrator yang dapat mengubah template notifikasi. Anda hanya dapat melihat.</div>
        </div>
      )}

      {/* Pemilih kanal */}
      <div className="mb-5 flex flex-wrap gap-2">
        {EDITABLE_CHANNELS.map((c) => (
          <button
            key={c}
            onClick={() => setChannel(c)}
            className={c === channel ? 'btn-primary' : 'btn-secondary'}
          >
            <span className="material-symbols-outlined text-[18px]">
              {c === 'telegram' ? 'send' : 'chat'}
            </span>
            {CHANNEL_LABEL[c] ?? c}
          </button>
        ))}
      </div>

      {data.loading && <LoadingBlock />}
      {data.error && <ErrorState message={data.error} onRetry={data.reload} />}
      {!data.loading && !data.error && grouped.length === 0 && (
        <div className="card">
          <EmptyState
            icon="notifications_off"
            title="Belum ada template"
            body={`Belum ada template notifikasi untuk kanal ${CHANNEL_LABEL[channel] ?? channel}.`}
          />
        </div>
      )}

      <div className="space-y-4">
        {grouped.map(([key, list]) => (
          <section key={key} className="card">
            <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-5 py-3.5">
              <div>
                <h3 className="font-headline text-lg font-semibold mono">{key}</h3>
                <p className="text-body-sm text-text-secondary">{list[0]?.description || 'Template notifikasi.'}</p>
              </div>
            </div>

            <div className="divide-y divide-border">
              {list.map((tpl) => (
                <div key={tpl.id} className="px-5 py-4">
                  <div className="mb-2 flex flex-wrap items-center gap-2">
                    <span className="badge bg-surface-container text-text-secondary border-outline-variant">
                      {tpl.channel ? (CHANNEL_LABEL[tpl.channel] ?? tpl.channel) : 'semua kanal'}
                    </span>
                    <span className={`badge ${SEVERITY_TONE[tpl.severity] ?? ''}`}>{tpl.severity}</span>
                    <span
                      className={`badge ${
                        tpl.is_active
                          ? 'bg-success-container text-on-success-container border-outline-variant'
                          : 'bg-surface-container text-text-secondary border-outline-variant'
                      }`}
                    >
                      {tpl.is_active ? 'aktif' : 'nonaktif'}
                    </span>
                    {tpl.item_type && (
                      <span className="badge bg-surface-container text-text-secondary border-outline-variant">
                        {tpl.item_type}
                      </span>
                    )}
                  </div>

                  {tpl.subject_tpl && (
                    <div className="mb-1.5">
                      <div className="kicker">Subjek</div>
                      <p className="whitespace-pre-wrap text-body-sm">{tpl.subject_tpl}</p>
                    </div>
                  )}

                  <div>
                    <div className="kicker">Isi pesan</div>
                    <pre className="mono mt-0.5 whitespace-pre-wrap break-words rounded-control border border-border bg-surface-container-low p-3 text-label-md text-text-secondary">
                      {tpl.body_tpl || '(kosong)'}
                    </pre>
                  </div>

                  {isAdmin && (
                    <div className="mt-3 flex flex-wrap gap-2">
                      <button className="btn-secondary" onClick={() => setEditing(tpl)}>
                        <span className="material-symbols-outlined text-[17px]">edit</span>
                        Sunting
                      </button>
                      <button
                        className="btn-secondary"
                        disabled={busyId === tpl.id}
                        onClick={() => toggleActive(tpl)}
                      >
                        <span className="material-symbols-outlined text-[17px]">
                          {tpl.is_active ? 'block' : 'check_circle'}
                        </span>
                        {tpl.is_active ? 'Nonaktifkan' : 'Aktifkan'}
                      </button>
                    </div>
                  )}
                </div>
              ))}
            </div>
          </section>
        ))}
      </div>

      {editing && (
        <TemplateEditForm
          template={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            toast('success', 'Template diperbarui')
            data.reload()
          }}
          onError={(msg) => toast('error', 'Gagal menyimpan template', msg)}
        />
      )}
    </div>
  )
}

/* ------------------------------------------------------------------------- */

function TemplateEditForm({
  template,
  onClose,
  onSaved,
  onError,
}: {
  template: NotificationTemplate
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const [subject, setSubject] = useState(template.subject_tpl ?? '')
  const [body, setBody] = useState(template.body_tpl ?? '')
  const [severity, setSeverity] = useState(template.severity || 'info')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await notificationApi.updateTemplate(template.id, {
        subject_tpl: subject,
        body_tpl: body,
        severity,
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
      title={`Sunting Template — ${template.key}`}
      width="lg"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !body.trim()}>
            {busy ? 'Menyimpan…' : 'Simpan Perubahan'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <div>
          <label className="label-field" htmlFor="tpl-subject">
            Subjek <span className="normal-case">(opsional)</span>
          </label>
          <input
            id="tpl-subject"
            className="input mono"
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            placeholder="kosongkan untuk notifikasi tanpa subjek"
          />
        </div>

        <div>
          <label className="label-field" htmlFor="tpl-body">
            Isi pesan
          </label>
          <textarea
            id="tpl-body"
            className="input mono h-56 py-2"
            required
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="✅ {{.RefNo}} — {{.Title}}"
          />
          <p className="mt-1 text-label-sm text-text-secondary">
            Placeholder yang tersedia antara lain:{' '}
            <span className="mono">
              {'{{.RefNo}} {{.Title}} {{.Priority}} {{.Owner}} {{.DueAt}} {{.ExpireAt}} {{.CreatedBy}}'}
            </span>
            . Emoji dapat dipakai langsung.
          </p>
        </div>

        <div>
          <label className="label-field" htmlFor="tpl-sev">
            Tingkat kepentingan
          </label>
          <select id="tpl-sev" className="input" value={severity} onChange={(e) => setSeverity(e.target.value)}>
            <option value="info">Info</option>
            <option value="warning">Warning</option>
            <option value="critical">Critical</option>
          </select>
        </div>
      </form>
    </Modal>
  )
}
