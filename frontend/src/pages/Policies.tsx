import { useState } from 'react'
import { ApiUser, EscalationPolicy, notificationApi } from '../api'
import { EmptyState, ErrorState, LoadingBlock, Modal, PageHeader } from '../components/ui'
import { useAsync } from '../hooks'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

type OffsetDraft = { label: string; mode: 'before' | 'after'; hours: number; severity: string }

/**
 * Policies (F4) — policy eskalasi menentukan KAPAN peringatan dikirim
 * relatif terhadap expire_at.
 *
 * Contoh pola trial 3 hari: H-2 (48 jam), H-1 (24 jam), H-0 (tepat), LATE-1H
 * (1 jam setelah). Pola RFS: H-7, H-3, H-1, H-0, LATE-4H.
 */
export default function Policies({ user, toast }: { user: ApiUser | null; toast: Toast }) {
  const policies = useAsync(() => notificationApi.policies(), [])
  const [showCreate, setShowCreate] = useState(false)
  const [editTarget, setEditTarget] = useState<EscalationPolicy | null>(null)

  const canWrite = !!user && (user.role === 'admin' || user.role === 'noc')
  const list = policies.data?.policies ?? []

  async function remove(p: EscalationPolicy) {
    if (!window.confirm(`Hapus policy "${p.name}"?`)) return
    try {
      await notificationApi.deletePolicy(p.id)
      toast('success', 'Policy dihapus', p.name)
      policies.reload()
    } catch (err) {
      toast('error', 'Gagal menghapus policy', err instanceof Error ? err.message : undefined)
    }
  }

  async function makeDefault(p: EscalationPolicy) {
    try {
      await notificationApi.updatePolicy(p.id, { is_default: true })
      toast('success', 'Policy default diperbarui', p.name)
      policies.reload()
    } catch (err) {
      toast('error', 'Gagal mengubah default', err instanceof Error ? err.message : undefined)
    }
  }

  return (
    <div>
      <PageHeader
        kicker="Notifikasi"
        title="Escalation Policies"
        description="Menentukan kapan peringatan dikirim relatif terhadap waktu expire."
        actions={
          <>
            <button className="btn-secondary" onClick={policies.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            {canWrite && (
              <button className="btn-primary" onClick={() => setShowCreate(true)}>
                <span className="material-symbols-outlined text-[18px]">add</span>
                Policy Baru
              </button>
            )}
          </>
        }
      />

      {policies.loading && <LoadingBlock />}
      {policies.error && <ErrorState message={policies.error} onRetry={policies.reload} />}
      {!policies.loading && !policies.error && list.length === 0 && (
        <div className="card">
          <EmptyState
            icon="stairs"
            title="Belum ada policy eskalasi"
            body="Policy menentukan kapan reminder dikirim. Buat policy dengan offset H-2, H-1, H-0, dan eskalasi LATE."
          />
        </div>
      )}

      <div className="space-y-4">
        {list.map((p) => (
          <section key={p.id} className="card">
            <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border px-5 py-3.5">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <h3 className="font-headline text-lg font-semibold">{p.name}</h3>
                  {p.is_default && (
                    <span className="badge bg-primary-container text-on-primary-container border-outline-variant">
                      default
                    </span>
                  )}
                  <span className="badge bg-surface-container text-text-secondary border-outline-variant">
                    {p.offsets.length} offset
                  </span>
                  {p.applies_to_item_types.length > 0 && (
                    <span className="badge bg-info-container text-on-info-container border-outline-variant">
                      {p.applies_to_item_types.join(', ')}
                    </span>
                  )}
                </div>
                {p.description && <p className="mt-0.5 text-body-sm text-text-secondary">{p.description}</p>}
              </div>

              {canWrite && (
                <div className="flex flex-wrap gap-2">
                  {!p.is_default && (
                    <button className="btn-secondary" onClick={() => makeDefault(p)}>
                      <span className="material-symbols-outlined text-[17px]">star</span>
                      Default
                    </button>
                  )}
                  <button className="btn-secondary" onClick={() => setEditTarget(p)}>
                    <span className="material-symbols-outlined text-[17px]">edit</span>
                    Ubah
                  </button>
                  <button className="btn-danger" onClick={() => remove(p)}>
                    <span className="material-symbols-outlined text-[17px]">delete</span>
                    Hapus
                  </button>
                </div>
              )}
            </div>

            {/* Timeline offset */}
            <div className="px-5 py-4">
              <div className="mb-3 kicker">Urutan peringatan</div>
              <div className="flex flex-wrap items-stretch gap-2">
                {[...p.offsets]
                  .sort((a, b) => offsetSortKey(a) - offsetSortKey(b))
                  .map((o, i) => {
                    const isLate = o.hours_after !== undefined
                    return (
                      <div
                        key={`${o.label}-${i}`}
                        className={`min-w-[110px] rounded-card border px-3 py-2 ${
                          isLate
                            ? 'border-critical bg-critical-container/50'
                            : o.severity === 'critical'
                              ? 'border-outline-variant bg-critical-container/40'
                              : o.severity === 'warning'
                                ? 'border-outline-variant bg-warning-container/50'
                                : 'border-outline-variant bg-info-container/40'
                        }`}
                      >
                        <div className="mono text-label-lg font-semibold">{o.label}</div>
                        <div className="text-label-sm text-text-secondary">
                          {isLate
                            ? `${o.hours_after} jam setelah`
                            : o.hours_before === 0
                              ? 'tepat saat expire'
                              : `${o.hours_before} jam sebelum`}
                        </div>
                        <span className="badge mt-1 bg-surface-container-lowest text-text-secondary border-outline-variant">
                          {o.severity}
                        </span>
                      </div>
                    )
                  })}
              </div>

              {(p.quiet_hours_from || p.quiet_hours_to) && (
                <p className="mt-3 text-label-sm text-text-secondary">
                  Jam tenang: <span className="mono">{p.quiet_hours_from ?? '—'}</span> –{' '}
                  <span className="mono">{p.quiet_hours_to ?? '—'}</span> (peringatan non-kritikal ditahan
                  hingga pagi; kritikal selalu dikirim)
                </p>
              )}
            </div>
          </section>
        ))}
      </div>

      {showCreate && (
        <PolicyForm
          onClose={() => setShowCreate(false)}
          onSaved={() => {
            setShowCreate(false)
            toast('success', 'Policy dibuat')
            policies.reload()
          }}
          onError={(msg) => toast('error', 'Gagal membuat policy', msg)}
        />
      )}

      {editTarget && (
        <PolicyForm
          existing={editTarget}
          onClose={() => setEditTarget(null)}
          onSaved={() => {
            setEditTarget(null)
            toast('success', 'Policy diperbarui')
            policies.reload()
          }}
          onError={(msg) => toast('error', 'Gagal memperbarui policy', msg)}
        />
      )}
    </div>
  )
}

/** offsetSortKey mengurutkan offset dari paling awal ke paling akhir. */
function offsetSortKey(o: { hours_before?: number; hours_after?: number }): number {
  if (o.hours_after !== undefined) return 1_000_000 + o.hours_after
  return -(o.hours_before ?? 0)
}

/* ------------------------------------------------------------------------- */

function PolicyForm({
  existing,
  onClose,
  onSaved,
  onError,
}: {
  existing?: EscalationPolicy
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const isEdit = !!existing
  const [name, setName] = useState(existing?.name ?? '')
  const [description, setDescription] = useState(existing?.description ?? '')
  const [itemTypes, setItemTypes] = useState<string[]>(existing?.applies_to_item_types ?? ['reminder'])
  const [quietFrom, setQuietFrom] = useState(existing?.quiet_hours_from ?? '21:00')
  const [quietTo, setQuietTo] = useState(existing?.quiet_hours_to ?? '08:00')
  const [maxAttempts, setMaxAttempts] = useState(existing?.max_attempts ?? 5)
  const [isDefault, setIsDefault] = useState(existing?.is_default ?? false)
  const [busy, setBusy] = useState(false)

  const [offsets, setOffsets] = useState<OffsetDraft[]>(() => {
    if (existing && existing.offsets.length > 0) {
      return existing.offsets.map((o) => ({
        label: o.label,
        mode: o.hours_after !== undefined ? 'after' : 'before',
        hours: o.hours_after ?? o.hours_before ?? 0,
        severity: o.severity,
      }))
    }
    // Default pola trial 3 hari.
    return [
      { label: 'H-2', mode: 'before', hours: 48, severity: 'info' },
      { label: 'H-1', mode: 'before', hours: 24, severity: 'warning' },
      { label: 'H-0', mode: 'before', hours: 0, severity: 'critical' },
      { label: 'LATE-1H', mode: 'after', hours: 1, severity: 'critical' },
    ]
  })

  function addOffset() {
    setOffsets([...offsets, { label: `H-${offsets.length}`, mode: 'before', hours: 12, severity: 'warning' }])
  }

  function updateOffset(i: number, patch: Partial<OffsetDraft>) {
    setOffsets(offsets.map((o, idx) => (idx === i ? { ...o, ...patch } : o)))
  }

  function removeOffset(i: number) {
    setOffsets(offsets.filter((_, idx) => idx !== i))
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()

    if (offsets.length === 0) {
      onError('Policy harus memiliki minimal satu offset')
      return
    }
    const labels = offsets.map((o) => o.label.trim())
    if (labels.some((l) => !l)) {
      onError('Setiap offset harus memiliki label')
      return
    }
    if (new Set(labels).size !== labels.length) {
      onError('Label offset tidak boleh duplikat')
      return
    }

    setBusy(true)
    try {
      const payload = {
        name,
        description,
        offsets: offsets.map((o) => ({
          label: o.label.trim(),
          severity: o.severity,
          ...(o.mode === 'before' ? { hours_before: o.hours } : { hours_after: o.hours }),
        })),
        applies_to_item_types: itemTypes,
        quiet_hours_from: quietFrom || undefined,
        quiet_hours_to: quietTo || undefined,
        max_attempts: maxAttempts,
        is_default: isDefault,
      }

      if (isEdit && existing) {
        await notificationApi.updatePolicy(existing.id, payload)
      } else {
        await notificationApi.createPolicy(payload)
      }
      onSaved()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Terjadi kesalahan')
    } finally {
      setBusy(false)
    }
  }

  function applyPreset(preset: 'trial' | 'rfs') {
    if (preset === 'trial') {
      setName((n) => n || 'TRIAL-3D-CUSTOM')
      setItemTypes(['reminder'])
      setOffsets([
        { label: 'H-2', mode: 'before', hours: 48, severity: 'info' },
        { label: 'H-1', mode: 'before', hours: 24, severity: 'warning' },
        { label: 'H-0', mode: 'before', hours: 0, severity: 'critical' },
        { label: 'LATE-1H', mode: 'after', hours: 1, severity: 'critical' },
      ])
    } else {
      setName((n) => n || 'RFS-CUSTOM')
      setItemTypes(['rfs'])
      setOffsets([
        { label: 'H-7', mode: 'before', hours: 168, severity: 'info' },
        { label: 'H-3', mode: 'before', hours: 72, severity: 'warning' },
        { label: 'H-1', mode: 'before', hours: 24, severity: 'warning' },
        { label: 'H-0', mode: 'before', hours: 0, severity: 'critical' },
        { label: 'LATE-4H', mode: 'after', hours: 4, severity: 'critical' },
      ])
    }
  }

  return (
    <Modal
      title={isEdit ? `Ubah Policy — ${existing?.name}` : 'Policy Eskalasi Baru'}
      width="lg"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !name.trim()}>
            {busy ? 'Menyimpan…' : isEdit ? 'Simpan Perubahan' : 'Buat Policy'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-4">
        {!isEdit && (
          <div className="flex flex-wrap gap-2">
            <button type="button" className="btn-secondary" onClick={() => applyPreset('trial')}>
              <span className="material-symbols-outlined text-[17px]">bolt</span>
              Preset Trial 3 Hari
            </button>
            <button type="button" className="btn-secondary" onClick={() => applyPreset('rfs')}>
              <span className="material-symbols-outlined text-[17px]">event_available</span>
              Preset RFS
            </button>
          </div>
        )}

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="pl-name">
              Nama policy
            </label>
            <input
              id="pl-name"
              className="input mono"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="TRIAL-3D"
            />
          </div>
          <div>
            <label className="label-field" htmlFor="pl-attempts">
              Maks. percobaan kirim
            </label>
            <input
              id="pl-attempts"
              type="number"
              min={1}
              max={20}
              className="input mono"
              value={maxAttempts}
              onChange={(e) => setMaxAttempts(Number(e.target.value) || 5)}
            />
          </div>
        </div>

        <div>
          <label className="label-field" htmlFor="pl-desc">
            Deskripsi
          </label>
          <input
            id="pl-desc"
            className="input"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Kapan policy ini dipakai"
          />
        </div>

        <div>
          <div className="label-field">Berlaku untuk tipe</div>
          <div className="flex flex-wrap gap-3">
            {[
              { v: 'reminder', l: 'Reminder' },
              { v: 'rfs', l: 'RFS' },
            ].map((t) => (
              <label key={t.v} className="flex cursor-pointer items-center gap-2 text-body-sm">
                <input
                  type="checkbox"
                  className="h-4 w-4 rounded border-border text-primary focus:ring-primary/30"
                  checked={itemTypes.includes(t.v)}
                  onChange={(e) =>
                    setItemTypes(
                      e.target.checked ? [...itemTypes, t.v] : itemTypes.filter((x) => x !== t.v),
                    )
                  }
                />
                {t.l}
              </label>
            ))}
          </div>
        </div>

        {/* Editor offset */}
        <div>
          <div className="mb-2 flex items-center justify-between">
            <div className="label-field mb-0">Offset peringatan</div>
            <button type="button" className="btn-ghost text-label-md" onClick={addOffset}>
              <span className="material-symbols-outlined text-[17px]">add</span>
              Tambah offset
            </button>
          </div>

          <div className="space-y-2">
            {offsets.map((o, i) => (
              <div key={i} className="flex flex-wrap items-end gap-2 rounded-control border border-border p-2.5">
                <div className="min-w-[90px] flex-1">
                  <label className="label-field" htmlFor={`of-label-${i}`}>
                    Label
                  </label>
                  <input
                    id={`of-label-${i}`}
                    className="input mono"
                    value={o.label}
                    onChange={(e) => updateOffset(i, { label: e.target.value })}
                    placeholder="H-1"
                  />
                </div>
                <div className="min-w-[120px]">
                  <label className="label-field" htmlFor={`of-mode-${i}`}>
                    Relatif
                  </label>
                  <select
                    id={`of-mode-${i}`}
                    className="input"
                    value={o.mode}
                    onChange={(e) => updateOffset(i, { mode: e.target.value as 'before' | 'after' })}
                  >
                    <option value="before">Sebelum expire</option>
                    <option value="after">Setelah expire</option>
                  </select>
                </div>
                <div className="w-[100px]">
                  <label className="label-field" htmlFor={`of-hours-${i}`}>
                    Jam
                  </label>
                  <input
                    id={`of-hours-${i}`}
                    type="number"
                    min={0}
                    className="input mono"
                    value={o.hours}
                    onChange={(e) => updateOffset(i, { hours: Number(e.target.value) || 0 })}
                  />
                </div>
                <div className="min-w-[110px]">
                  <label className="label-field" htmlFor={`of-sev-${i}`}>
                    Severity
                  </label>
                  <select
                    id={`of-sev-${i}`}
                    className="input"
                    value={o.severity}
                    onChange={(e) => updateOffset(i, { severity: e.target.value })}
                  >
                    <option value="info">Info</option>
                    <option value="warning">Warning</option>
                    <option value="critical">Critical</option>
                  </select>
                </div>
                <button
                  type="button"
                  className="btn-ghost h-9 w-9 px-0 text-critical"
                  onClick={() => removeOffset(i)}
                  title="Hapus offset"
                  disabled={offsets.length <= 1}
                >
                  <span className="material-symbols-outlined text-[18px]">delete</span>
                </button>
              </div>
            ))}
          </div>

          <p className="mt-2 text-label-sm text-text-secondary">
            Contoh: offset <span className="mono">H-1 / sebelum / 24 jam</span> berarti peringatan dikirim
            24 jam sebelum expire. Offset <span className="mono">LATE-1H / setelah / 1 jam</span> berarti
            eskalasi 1 jam setelah expire.
          </p>
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="pl-qf">
              Jam tenang — mulai
            </label>
            <input id="pl-qf" type="time" className="input" value={quietFrom} onChange={(e) => setQuietFrom(e.target.value)} />
          </div>
          <div>
            <label className="label-field" htmlFor="pl-qt">
              Jam tenang — selesai
            </label>
            <input id="pl-qt" type="time" className="input" value={quietTo} onChange={(e) => setQuietTo(e.target.value)} />
          </div>
        </div>

        <label className="flex cursor-pointer items-center gap-2 text-body-sm text-text-secondary">
          <input
            type="checkbox"
            className="h-4 w-4 rounded border-border text-primary focus:ring-primary/30"
            checked={isDefault}
            onChange={(e) => setIsDefault(e.target.checked)}
          />
          Jadikan policy default
        </label>
      </form>
    </Modal>
  )
}
