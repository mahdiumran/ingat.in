import { useMemo, useState } from 'react'
import { ApiUser, MasterDataEntry, MasterDataKind, masterDataApi } from '../api'
import {
  EmptyState,
  ErrorState,
  LoadingBlock,
  Modal,
  PageHeader,
} from '../components/ui'
import { useAsync, useDebounced } from '../hooks'
import { TAG_COLOR_OPTIONS, tagClass } from '../design/tokens'

type Toast = (kind: 'success' | 'error' | 'info' | 'warning', title: string, body?: string) => void

/** Meta pelanggan yang disimpan pada master_data.meta_json. */
export type CustomerMeta = {
  jenis: string
  pic: string
  phone: string
  email: string
  address: string
  capacity: string
}

export function customerMeta(entry: MasterDataEntry): CustomerMeta {
  const m = (entry.meta ?? {}) as Record<string, unknown>
  const str = (k: string) => (m[k] == null ? '' : String(m[k]))
  return {
    jenis: str('jenis'),
    pic: str('pic'),
    phone: str('phone'),
    email: str('email'),
    address: str('address'),
    capacity: str('capacity'),
  }
}

/**
 * MasterDataKindPage (F12) — halaman KELOLA satu kelompok master data.
 *
 * Setiap kelompok dibuka pada halaman tersendiri (bukan tab dalam satu tabel
 * besar) agar pengelolaannya fokus. Untuk kelompok `customer`, form dan tabel
 * memakai kolom terstruktur (jenis, PIC, telepon, surel, alamat, kapasitas).
 */
export default function MasterDataKindPage({
  user,
  toast,
  kind,
  onBack,
}: {
  user: ApiUser | null
  toast: Toast
  kind: string
  onBack: () => void
}) {
  const [search, setSearch] = useState('')
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<MasterDataEntry | null>(null)
  const [showKindForm, setShowKindForm] = useState(false)

  const debouncedSearch = useDebounced(search)
  const isCustomer = kind === 'customer'

  const data = useAsync(() => masterDataApi.list({ kind }), [kind])
  const meta = useAsync(() => masterDataApi.list(), [])
  const kindMeta: MasterDataKind | undefined = meta.data?.kinds.find((k) => k.kind === kind)

  const entries = useMemo(() => {
    const all = data.data?.entries ?? []
    if (!debouncedSearch) return all
    const q = debouncedSearch.toLowerCase()
    return all.filter((e) => {
      if (isCustomer) {
        const cm = customerMeta(e)
        return (
          e.label.toLowerCase().includes(q) ||
          e.code.toLowerCase().includes(q) ||
          cm.pic.toLowerCase().includes(q) ||
          cm.phone.toLowerCase().includes(q) ||
          cm.email.toLowerCase().includes(q)
        )
      }
      return e.label.toLowerCase().includes(q) || e.code.toLowerCase().includes(q)
    })
  }, [data.data, debouncedSearch, isCustomer])

  const canWrite = !!user && user.role !== 'viewer'

  async function removeEntry(entry: MasterDataEntry) {
    if (!window.confirm(`Hapus "${entry.label}"?`)) return
    try {
      await masterDataApi.remove(entry.id)
      toast('success', 'Data dihapus', entry.label)
      data.reload()
    } catch (err) {
      toast('error', 'Gagal menghapus', err instanceof Error ? err.message : undefined)
    }
  }

  return (
    <div>
      <PageHeader
        kicker="Master Data"
        title={kindMeta?.label ?? kind}
        description={kindMeta?.description || 'Kelola data referensi kelompok ini.'}
        actions={
          <>
            <button className="btn-ghost" onClick={onBack}>
              <span className="material-symbols-outlined text-[18px]">arrow_back</span>
              Kembali
            </button>
            <button className="btn-secondary" onClick={data.reload}>
              <span className="material-symbols-outlined text-[18px]">refresh</span>
              Muat ulang
            </button>
            {canWrite && (
              <button className="btn-primary" onClick={() => setCreating(true)}>
                <span className="material-symbols-outlined text-[18px]">add</span>
                {isCustomer ? 'Tambah Customer' : 'Tambah Entri'}
              </button>
            )}
          </>
        }
      />

      <section className="card">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3.5">
          <div>
            <h3 className="font-headline text-lg font-semibold">{kindMeta?.label ?? kind}</h3>
            <p className="text-body-sm text-text-secondary">
              {entries.length} entri
              {debouncedSearch ? ' (terfilter)' : ''}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <input
              className="input w-56"
              placeholder={isCustomer ? 'Cari nama / kode / PIC…' : 'Cari label / kode…'}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            {canWrite && !kindMeta?.is_system && (
              <button className="btn-ghost" onClick={() => setShowKindForm(true)} title="Ubah nama kelompok">
                <span className="material-symbols-outlined text-[18px]">edit_note</span>
              </button>
            )}
          </div>
        </div>

        {data.loading && <LoadingBlock />}
        {data.error && (
          <div className="p-4">
            <ErrorState message={data.error} onRetry={data.reload} />
          </div>
        )}

        {!data.loading && !data.error && entries.length === 0 && (
          <EmptyState
            icon={kindMeta?.icon ?? 'category'}
            title={isCustomer ? 'Belum ada customer' : 'Belum ada entri'}
            body={
              debouncedSearch
                ? 'Tidak ada data yang cocok dengan pencarian.'
                : isCustomer
                  ? 'Tambahkan customer pertama. Nomor pelanggan dibuat otomatis.'
                  : `Tambahkan entri pertama untuk ${kindMeta?.label ?? kind}.`
            }
            action={
              canWrite && !debouncedSearch ? (
                <button className="btn-primary" onClick={() => setCreating(true)}>
                  <span className="material-symbols-outlined text-[18px]">add</span>
                  {isCustomer ? 'Tambah Customer' : 'Tambah Entri'}
                </button>
              ) : undefined
            }
          />
        )}

        {!data.loading && !data.error && entries.length > 0 && isCustomer && (
          <CustomerTable entries={entries} canWrite={canWrite} onEdit={setEditing} onDelete={removeEntry} />
        )}

        {!data.loading && !data.error && entries.length > 0 && !isCustomer && (
          <GenericTable entries={entries} canWrite={canWrite} onEdit={setEditing} onDelete={removeEntry} />
        )}
      </section>

      {(creating || editing) &&
        (isCustomer ? (
          <CustomerForm
            entry={editing}
            onClose={() => {
              setCreating(false)
              setEditing(null)
            }}
            onSaved={() => {
              const wasEdit = !!editing
              setCreating(false)
              setEditing(null)
              toast('success', wasEdit ? 'Customer diperbarui' : 'Customer dibuat')
              data.reload()
            }}
            onError={(msg) => toast('error', 'Gagal menyimpan', msg)}
          />
        ) : (
          <EntryForm
            kind={kind}
            kindLabel={kindMeta?.label ?? kind}
            entry={editing}
            allEntries={data.data?.entries ?? []}
            onClose={() => {
              setCreating(false)
              setEditing(null)
            }}
            onSaved={() => {
              const wasEdit = !!editing
              setCreating(false)
              setEditing(null)
              toast('success', wasEdit ? 'Entri diperbarui' : 'Entri dibuat')
              data.reload()
            }}
            onError={(msg) => toast('error', 'Gagal menyimpan', msg)}
          />
        ))}

      {showKindForm && (
        <KindRenameForm
          kind={kind}
          current={kindMeta}
          onClose={() => setShowKindForm(false)}
          onSaved={() => {
            setShowKindForm(false)
            toast('success', 'Kelompok diperbarui')
            meta.reload()
          }}
          onError={(msg) => toast('error', 'Gagal menyimpan kelompok', msg)}
        />
      )}
    </div>
  )
}

/* ------------------------------------------------------------------------- */
/* Tabel generik                                                              */
/* ------------------------------------------------------------------------- */

function GenericTable({
  entries,
  canWrite,
  onEdit,
  onDelete,
}: {
  entries: MasterDataEntry[]
  canWrite: boolean
  onEdit: (e: MasterDataEntry) => void
  onDelete: (e: MasterDataEntry) => void
}) {
  return (
    <div className="overflow-x-auto">
      <table className="table">
        <thead>
          <tr>
            <th>Kode</th>
            <th>Label</th>
            <th>Keterangan</th>
            <th>Urutan</th>
            <th>Status</th>
            <th className="text-right">Aksi</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((e) => (
            <tr key={e.id}>
              <td className="mono text-label-md">{e.code}</td>
              <td className="font-medium">{e.label}</td>
              <td className="max-w-[320px] truncate text-text-secondary">{e.description || '—'}</td>
              <td className="mono text-label-md text-text-secondary">{e.sort_order}</td>
              <td>
                <ActiveBadge active={e.is_active} />
              </td>
              <td>
                <RowActions canWrite={canWrite} onEdit={() => onEdit(e)} onDelete={() => onDelete(e)} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/* ------------------------------------------------------------------------- */
/* Tabel customer                                                             */
/* ------------------------------------------------------------------------- */

function CustomerTable({
  entries,
  canWrite,
  onEdit,
  onDelete,
}: {
  entries: MasterDataEntry[]
  canWrite: boolean
  onEdit: (e: MasterDataEntry) => void
  onDelete: (e: MasterDataEntry) => void
}) {
  return (
    <div className="overflow-x-auto">
      <table className="table">
        <thead>
          <tr>
            <th>No. Pelanggan</th>
            <th>Nama</th>
            <th>Jenis</th>
            <th>PIC</th>
            <th>Telepon</th>
            <th>Email</th>
            <th>Kapasitas</th>
            <th>Alamat</th>
            <th>Status</th>
            <th className="text-right">Aksi</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((e) => {
            const cm = customerMeta(e)
            const corporate = cm.jenis === 'corporate'
            return (
              <tr key={e.id}>
                <td className="mono whitespace-nowrap text-label-md">{e.code}</td>
                <td className="font-medium">{e.label}</td>
                <td>
                  <span
                    className={`badge ${
                      corporate
                        ? 'border-outline-variant bg-info-container/50 text-on-info-container'
                        : 'border-outline-variant bg-surface-container text-text-secondary'
                    }`}
                  >
                    {corporate ? 'Corporate' : 'Personal'}
                  </span>
                </td>
                <td className="text-text-secondary">{corporate ? cm.pic || '—' : '—'}</td>
                <td className="mono whitespace-nowrap text-label-md">{cm.phone || '—'}</td>
                <td className="text-text-secondary">{cm.email || '—'}</td>
                <td className="mono whitespace-nowrap text-label-md">{cm.capacity || '—'}</td>
                <td className="max-w-[220px] truncate text-text-secondary">{cm.address || '—'}</td>
                <td>
                  <ActiveBadge active={e.is_active} />
                </td>
                <td>
                  <RowActions canWrite={canWrite} onEdit={() => onEdit(e)} onDelete={() => onDelete(e)} />
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/* ------------------------------------------------------------------------- */
/* Potongan kecil bersama                                                     */
/* ------------------------------------------------------------------------- */

function ActiveBadge({ active }: { active: boolean }) {
  return (
    <span
      className={`badge ${
        active
          ? 'border-outline-variant bg-success-container text-on-success-container'
          : 'border-outline-variant bg-surface-container text-text-secondary'
      }`}
    >
      {active ? 'aktif' : 'nonaktif'}
    </span>
  )
}

function RowActions({
  canWrite,
  onEdit,
  onDelete,
}: {
  canWrite: boolean
  onEdit: () => void
  onDelete: () => void
}) {
  if (!canWrite) return <span className="text-text-secondary">—</span>
  return (
    <div className="flex justify-end gap-1">
      <button className="btn-ghost h-8 w-8 px-0" title="Edit" onClick={onEdit}>
        <span className="material-symbols-outlined text-[18px]">edit</span>
      </button>
      <button className="btn-ghost h-8 w-8 px-0 text-critical" title="Hapus" onClick={onDelete}>
        <span className="material-symbols-outlined text-[18px]">delete</span>
      </button>
    </div>
  )
}

/* ------------------------------------------------------------------------- */
/* Form customer                                                              */
/* ------------------------------------------------------------------------- */

function CustomerForm({
  entry,
  onClose,
  onSaved,
  onError,
}: {
  entry: MasterDataEntry | null
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const isEdit = !!entry
  const initial = entry ? customerMeta(entry) : null

  const [name, setName] = useState(entry?.label ?? '')
  const [jenis, setJenis] = useState(initial?.jenis || 'personal')
  const [pic, setPic] = useState(initial?.pic ?? '')
  const [phone, setPhone] = useState(initial?.phone ?? '')
  const [email, setEmail] = useState(initial?.email ?? '')
  const [address, setAddress] = useState(initial?.address ?? '')
  const [capacity, setCapacity] = useState(initial?.capacity ?? '')
  const [isActive, setIsActive] = useState(entry?.is_active ?? true)
  const [busy, setBusy] = useState(false)

  const corporate = jenis === 'corporate'

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const metaPayload: CustomerMeta = {
        jenis,
        // PIC hanya relevan untuk corporate.
        pic: corporate ? pic : '',
        phone,
        email,
        address,
        capacity,
      }
      if (isEdit && entry) {
        await masterDataApi.update(entry.id, {
          label: name,
          description: `${jenis === 'corporate' ? 'Corporate' : 'Personal'}${capacity ? ' · ' + capacity : ''}`,
          meta: metaPayload as unknown as Record<string, unknown>,
          is_active: isActive,
        })
      } else {
        await masterDataApi.create({
          kind: 'customer',
          label: name,
          description: `${jenis === 'corporate' ? 'Corporate' : 'Personal'}${capacity ? ' · ' + capacity : ''}`,
          meta: metaPayload as unknown as Record<string, unknown>,
          is_active: isActive,
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
      title={isEdit ? `Edit Customer — ${entry?.code}` : 'Customer Baru'}
      onClose={onClose}
      width="md"
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button
            className="btn-primary"
            onClick={submit}
            disabled={busy || !name.trim() || (corporate && !pic.trim())}
          >
            {busy ? 'Menyimpan…' : isEdit ? 'Simpan Perubahan' : 'Tambah Customer'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        {isEdit && (
          <div>
            <label className="label-field">No. Pelanggan</label>
            <input className="input mono" value={entry?.code ?? ''} disabled />
          </div>
        )}

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="cu-name">
              Nama{corporate ? ' Perusahaan' : ''}
            </label>
            <input
              id="cu-name"
              className="input"
              required
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={corporate ? 'mis. PT Contoh Jaya' : 'mis. Budi Santoso'}
            />
          </div>
          <div>
            <label className="label-field" htmlFor="cu-jenis">
              Jenis
            </label>
            <select
              id="cu-jenis"
              className="input"
              value={jenis}
              onChange={(e) => setJenis(e.target.value)}
            >
              <option value="personal">Personal</option>
              <option value="corporate">Corporate</option>
            </select>
          </div>
        </div>

        {corporate && (
          <div>
            <label className="label-field" htmlFor="cu-pic">
              Nama PIC <span className="text-critical">*</span>
            </label>
            <input
              id="cu-pic"
              className="input"
              required
              value={pic}
              onChange={(e) => setPic(e.target.value)}
              placeholder="Nama penanggung jawab"
            />
            <p className="mt-1 text-label-sm text-text-secondary">
              Wajib untuk pelanggan corporate.
            </p>
          </div>
        )}

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="cu-phone">
              Nomor Telepon
            </label>
            <input
              id="cu-phone"
              className="input mono"
              value={phone}
              onChange={(e) => setPhone(e.target.value)}
              placeholder="0812xxxxxxx"
            />
          </div>
          <div>
            <label className="label-field" htmlFor="cu-email">
              Email
            </label>
            <input
              id="cu-email"
              type="email"
              className="input"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="nama@contoh.id"
            />
          </div>
        </div>

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="cu-capacity">
              Kapasitas
            </label>
            <input
              id="cu-capacity"
              className="input"
              value={capacity}
              onChange={(e) => setCapacity(e.target.value)}
              placeholder="mis. 100 Mbps"
            />
          </div>
          <div className="flex items-end">
            <label className="flex items-center gap-2 text-body-md">
              <input type="checkbox" checked={isActive} onChange={(e) => setIsActive(e.target.checked)} />
              Aktif
            </label>
          </div>
        </div>

        <div>
          <label className="label-field" htmlFor="cu-address">
            Alamat
          </label>
          <textarea
            id="cu-address"
            className="input h-20 py-2"
            value={address}
            onChange={(e) => setAddress(e.target.value)}
            placeholder="Alamat lengkap"
          />
        </div>
      </form>
    </Modal>
  )
}

/* ------------------------------------------------------------------------- */
/* Form entri generik                                                         */
/* ------------------------------------------------------------------------- */

function EntryForm({
  kind,
  kindLabel,
  entry,
  allEntries,
  onClose,
  onSaved,
  onError,
}: {
  kind: string
  kindLabel: string
  entry: MasterDataEntry | null
  allEntries: MasterDataEntry[]
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const isEdit = !!entry
  const [label, setLabel] = useState(entry?.label ?? '')
  const [code, setCode] = useState(entry?.code ?? '')
  const [description, setDescription] = useState(entry?.description ?? '')
  const [sortOrder, setSortOrder] = useState(entry?.sort_order ?? 0)
  const [isActive, setIsActive] = useState(entry?.is_active ?? true)
  // Meta khusus per kind.
  const [color, setColor] = useState(
    String((entry?.meta as { color?: string } | undefined)?.color ?? 'gray'),
  )
  const [parentId, setParentId] = useState(entry?.parent_id ?? '')
  const [busy, setBusy] = useState(false)
  const [autoCode, setAutoCode] = useState(!isEdit)

  const isTagColor = kind === 'tag_color'
  const isTicketCategory = kind === 'ticket_category'
  // Kandidat induk (untuk hierarki kategori → subkategori).
  const parentOptions = allEntries.filter((e) => !e.parent_id && e.id !== entry?.id)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const payloadCode = autoCode ? undefined : code
      const meta = isTagColor ? { color } : undefined
      if (isEdit && entry) {
        await masterDataApi.update(entry.id, {
          label,
          code: payloadCode ?? undefined,
          description,
          sort_order: sortOrder,
          is_active: isActive,
          ...(meta ? { meta } : {}),
          ...(isTicketCategory ? { parent_id: parentId || undefined, clear_parent: !parentId } : {}),
        })
      } else {
        await masterDataApi.create({
          kind,
          label,
          code: payloadCode,
          description,
          sort_order: sortOrder,
          is_active: isActive,
          ...(meta ? { meta } : {}),
          ...(isTicketCategory && parentId ? { parent_id: parentId } : {}),
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
      title={isEdit ? `Edit — ${entry?.label}` : `Tambah ${kindLabel}`}
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !label.trim()}>
            {busy ? 'Menyimpan…' : isEdit ? 'Simpan Perubahan' : 'Tambah'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <div>
          <label className="label-field" htmlFor="md-label">
            Label
          </label>
          <input
            id="md-label"
            className="input"
            required
            autoFocus
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="mis. Instalasi Baru"
          />
        </div>

        <div>
          <label className="label-field" htmlFor="md-code">
            Kode
          </label>
          <div className="flex items-center gap-2">
            <input
              id="md-code"
              className="input mono"
              value={autoCode ? '' : code}
              disabled={autoCode}
              onChange={(e) => setCode(e.target.value)}
              placeholder={autoCode ? 'otomatis dari label' : 'KODE_UNIK'}
            />
            <label className="flex shrink-0 items-center gap-1.5 text-body-sm">
              <input type="checkbox" checked={autoCode} onChange={(e) => setAutoCode(e.target.checked)} />
              Otomatis
            </label>
          </div>
        </div>

        <div>
          <label className="label-field" htmlFor="md-desc">
            Keterangan
          </label>
          <textarea
            id="md-desc"
            className="input h-20 py-2"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Penjelasan singkat (opsional)"
          />
        </div>

        {isTagColor && (
          <div>
            <label className="label-field" htmlFor="md-color">
              Warna
            </label>
            <div className="flex flex-wrap items-center gap-2">
              {TAG_COLOR_OPTIONS.map((c) => (
                <button
                  key={c.value}
                  type="button"
                  onClick={() => setColor(c.value)}
                  title={c.label}
                  className={`badge border ${tagClass(c.value)} ${
                    color === c.value ? 'ring-2 ring-primary ring-offset-1' : ''
                  }`}
                >
                  {c.label}
                </button>
              ))}
            </div>
            <p className="mt-1 text-label-sm text-text-secondary">
              Warna dipakai saat tag ini muncul pada daftar & detail.
            </p>
          </div>
        )}

        {isTicketCategory && (
          <div>
            <label className="label-field" htmlFor="md-parent">
              Induk (opsional)
            </label>
            <select id="md-parent" className="input" value={parentId} onChange={(e) => setParentId(e.target.value)}>
              <option value="">— jadikan kategori utama —</option>
              {parentOptions.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.label}
                </option>
              ))}
            </select>
            <p className="mt-1 text-label-sm text-text-secondary">
              Pilih induk untuk menjadikan entri ini subkategori dari kategori tersebut.
            </p>
          </div>
        )}

        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="md-sort">
              Urutan
            </label>
            <input
              id="md-sort"
              type="number"
              className="input mono"
              value={sortOrder}
              onChange={(e) => setSortOrder(Number(e.target.value) || 0)}
            />
          </div>
          <div className="flex items-end">
            <label className="flex items-center gap-2 text-body-md">
              <input type="checkbox" checked={isActive} onChange={(e) => setIsActive(e.target.checked)} />
              Aktif
            </label>
          </div>
        </div>
      </form>
    </Modal>
  )
}

/* ------------------------------------------------------------------------- */
/* Form ubah kelompok (rename label/ikon)                                     */
/* ------------------------------------------------------------------------- */

function KindRenameForm({
  kind,
  current,
  onClose,
  onSaved,
  onError,
}: {
  kind: string
  current?: MasterDataKind
  onClose: () => void
  onSaved: () => void
  onError: (msg: string) => void
}) {
  const [label, setLabel] = useState(current?.label ?? '')
  const [description, setDescription] = useState(current?.description ?? '')
  const [icon, setIcon] = useState(current?.icon ?? 'category')
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await masterDataApi.createKind({ kind, label, description, icon })
      onSaved()
    } catch (err) {
      onError(err instanceof Error ? err.message : 'Terjadi kesalahan')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title="Ubah Kelompok"
      onClose={onClose}
      footer={
        <>
          <button className="btn-secondary" onClick={onClose} disabled={busy}>
            Batal
          </button>
          <button className="btn-primary" onClick={submit} disabled={busy || !label.trim()}>
            {busy ? 'Menyimpan…' : 'Simpan'}
          </button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-3.5">
        <div>
          <label className="label-field">Kunci kelompok (kind)</label>
          <input className="input mono" value={kind} disabled />
        </div>
        <div>
          <label className="label-field" htmlFor="kr-label">
            Nama tampil
          </label>
          <input id="kr-label" className="input" required value={label} onChange={(e) => setLabel(e.target.value)} />
        </div>
        <div className="grid gap-3.5 sm:grid-cols-2">
          <div>
            <label className="label-field" htmlFor="kr-icon">
              Ikon
            </label>
            <input id="kr-icon" className="input mono" value={icon} onChange={(e) => setIcon(e.target.value)} />
          </div>
          <div>
            <label className="label-field" htmlFor="kr-desc">
              Keterangan
            </label>
            <input
              id="kr-desc"
              className="input"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
        </div>
      </form>
    </Modal>
  )
}
