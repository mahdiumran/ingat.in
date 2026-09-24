# DESIGN.md — Bahasa Visual Ingat.in

Dokumen ini adalah **sumber kebenaran** untuk warna, tipografi, bentuk, dan pola
komponen Ingat.in. Nilai warna/font **tidak boleh di-hard-code** di komponen —
gunakan kelas Tailwind (`tailwind.config.js`) atau token di
`frontend/src/design/tokens.ts`.

Diturunkan dari bahasa visual m2c/M2Cloud dan telah diperluas dengan token
semantik khusus Ingat.in (status, prioritas, **warna tag**, snapshot SLA).

---

## 1. Prinsip

1. **Light-first, tenang, padat informasi.** Latar nyaris putih, aksen hijau
   brand. Operator NOC memindai tabel selama berjam-jam; kontras tinggi untuk
   label, area sentuh lega.
2. **Warna bukan satu-satunya pembeda.** Setiap warna selalu disertai label teks
   (badge status/prioritas/tag). Ini penting untuk aksesibilitas & cetak.
3. **Satu tempat untuk nilai.** Warna/font didefinisikan sekali di
   `tailwind.config.js` + `tokens.ts`, tidak tersebar di komponen.
4. **Umpan balik eksplisit.** Setiap aksi tulis memunculkan konfirmasi/notifikasi
   (modal + toast) dengan nada profesional, bukan `alert()` bawaan browser.

---

## 2. Palet Warna

| Token | Nilai | Pemakaian |
|---|---|---|
| `primary` | `#006d36` | Aksi utama, tautan, aksen brand |
| `primary-container` | `#4ade80` | Latar chip/ikon brand |
| `accent-brand` | `#4ADE80` | CTA/brand |
| `accent-soft` | `#BBF7D0` | Border/soft chip |
| `background` | `#f9f9ff` | Latar halaman |
| `surface-container-lowest` | `#ffffff` | Kartu/panel |
| `surface-container` | `#e9edff` | Latar netral/chip |
| `border` | `#E5E7EB` | Garis pemisah |
| `text-primary` | `#111827` | Teks utama |
| `text-secondary` | `#4B5563` | Teks pendukung |

### 2.1 Warna semantik status

| Semantik | Teks | Latar (`*-container`) | Arti |
|---|---|---|---|
| `success` | `#006d36` | `#b4f0c9` | Selesai/aktif/normal |
| `warning` | `#b45309` | `#fde68a` | Perhatian/peringatan |
| `critical` | `#ba1a1a` | `#ffdad6` | Gagal/kritis/terlambat |
| `info` | `#31694b` | `#97d1ac` | Informasi/baru |
| `unknown` | `#6d7b6d` | `#e9edff` | Netral/tak diketahui |

---

## 3. Tag Berwarna

Tag bebas (`work_items.tags`) dapat diberi **warna** agar mudah dipindai.
Warna tag didefinisikan di **Master Data** (kind `tag_color`): setiap entri
memetakan **kode tag → warna**. Tag yang belum didefinisikan memakai warna
netral (`surface-container`).

Palet warna tag yang tersedia (pilih satu per tag):

| Nama | Kode | Kelas (contoh) |
|---|---|---|
| Merah | `red` | `bg-critical-container text-on-critical-container` |
| Oren | `orange` | `bg-[#ffe0c2] text-[#8a4b00]` |
| Kuning | `yellow` | `bg-warning-container text-on-warning-container` |
| Hijau | `green` | `bg-success-container text-on-success-container` |
| Biru | `blue` | `bg-[#d6e4ff] text-[#1b4fd8]` |
| Ungu | `purple` | `bg-[#eadcff] text-[#5b21b6]` |
| Abu (netral) | `gray` | `bg-surface-container text-text-secondary` |

Aturan pemakaian warna tag (rekomendasi, tidak dipaksa sistem):

| Tag | Saran warna |
|---|---|
| `high`, `critical`, `urgent` | **merah** |
| `warning`, `perhatian` | **kuning** |
| `open`, `baru` | **hijau** |
| `maintenance`, `jadwal` | **biru** |
| `vip`, `pelanggan` | **ungu** |
| `follow-up`, `pending` | **oren** |

> Tag dipakai untuk **penambahan tag tiket dan kategori tiket**. Warna membantu
> membedakan kategori/kelompok pada daftar dan detail.

---

## 4. Tipografi

| Peran | Font | Kelas |
|---|---|---|
| Headline | Inter | `font-headline` |
| Body | Space Grotesk | `font-body` |
| Label/kode | JetBrains Mono | `mono` |

Skala: `text-headline-md` (judul halaman), `text-body-lg`/`text-body-sm`
(isi), `text-label-md`/`text-label-sm` (meta, kode, timestamp), `kicker`
(judul kecil huruf besar berjarak).

---

## 5. Bentuk & Jarak

- Radius: `rounded-control` (8px, tombol/input), `rounded-card` (8px, kartu),
  `rounded-modal` (12px), `rounded-full` (badge).
- Jarak: grid `gap-3`..`gap-5`; padding kartu `p-4`..`p-5`; tinggi kontrol
  `h-8` (kompak) / `h-9`.

---

## 6. Komponen Inti

- **Badge status/prioritas/tag** — selalu berlabel teks; lihat `StatusBadge`,
  `PriorityBadge`, `TagBadge` di `components/ui.tsx`.
- **Tab** — tiga tab halaman detail: **Ringkasan | Aktivitas | Action**.
  Tab aktif: teks `primary` + garis bawah `primary`; tab non-aktif
  `text-secondary`.
- **Modal** — latar `bg-slate-950/60 backdrop-blur-sm`, panel
  `rounded-modal border-border bg-surface-container-lowest shadow-modal`,
  lebar `sm` 420px / `md` 560px / `lg` 760px.

---

## 7. Pola Notifikasi & Konfirmasi Aksi

Setiap aksi yang mengubah data memberi umpan balik dua lapis:

1. **Modal konfirmasi** (sebelum aksi berisiko: hapus, tutup tiket, tandai
   aktif, ubah status ke state terminal). Judul ringkas, penjelasan konsekuensi,
   tombol **Batal** (`btn-secondary`) dan tombol aksi (`btn-primary`, atau
   `btn-danger` untuk merusak). Ikon semantik: `warning`/`delete`/`help`.
2. **Toast hasil** (setelah aksi selesai): sukses → `success`, gagal →
   `error`, informasi → `info`, sebagian → `warning`. Toast stack di kanan atas,
   hilang otomatis 6 detik, dapat ditutup manual.

**Nada bahasa:** profesional, ringkas, bahasa Indonesia, tanpa emoji pada toast
kecuali konvensi pesan notifikasi WA/Telegram (emoji diizinkan di sana).

Implementasi:
- Modal konfirmasi generik: `ConfirmDialog` (`components/ui.tsx`).
- Toast: `ToastStack` + `pushToast` di `App.tsx`.

---

## 8. Snapshot SLA (Tiket)

Tiket **tidak memakai tenggat waktu**; alih-alih, sistem mengukur **durasi
penanganan (SLA)**: dari `first_response_at` (status pertama kali keluar dari
`new`) sampai `closed_at`. Tampilkan sebagai:
- **berjalan** bila belum closed (durasi hingga sekarang), dan
- **final** bila sudah closed (durasi tetap).

Format durasi: `Xd Yh Zm` (ringkas) dengan tone: hijau `< 4 jam`,
kuning `4–24 jam`, merah `> 24 jam`.

---

## 9. Pola Papan Kanban (Daily Task)

Daily Task ditampilkan sebagai papan **3 kolom** yang mencerminkan workflow
`pending → in_progress → done`:

| Kolom | Status | Aksen batas atas |
|---|---|---|
| Belum Selesai | `pending` | `border-t-outline-variant` |
| Sedang Dikerjakan | `in_progress` | `border-t-primary` |
| Selesai | `done` | `border-t-success` |

Aturan:
- **Tambah langsung per kolom** (*inline quick-add*): tombol garis putus-putus
  `Tambah task` di dasar kolom membuka input judul sebaris; Enter menyimpan,
  Esc membatalkan. Kolom **Selesai** tidak punya quick-add.
- **Tombol perpindahan status** pada tiap kartu (bukan drag) menjaga aksesibilitas
  keyboard: `Kerjakan` (`in_progress`), `Selesai` (`done`), `Belum` (kembali ke
  `pending`). Status terminal diberi warna semantik (`done` → `text-success`).
- **Badge Terlambat** (carry-over) muncul pada kartu yang `start_at`-nya sebelum
  tanggal terpilih namun belum selesai — memakai `bg-warning-container`.
- Kartu yang `done` ditampilkan dengan judul bergaris (`line-through`) + warna
  `text-text-secondary` — status tetap dibedakan oleh teks, bukan warna saja.
- Header papan memuat navigasi hari (◀ tanggal ▶ / Hari ini) dan **progress bar**
  `done/total` (`bg-success`).

Implementasi: `pages/DailyTasks.tsx`; data dari `GET /api/items?type=daily_task&date=&carry_over=`.

---

## 10. Pola Integrasi Berbasis Antrean (Google Sheets)

Integrasi keluar (mis. Google Sheets) mengikuti pola yang sama dengan
`notification_outbox`, agar UI tidak pernah menunggu jaringan pihak ketiga:

1. **Aksi pengguna selesai lebih dulu** — saat Todo Task / Daily Task dibuat,
   diubah, atau dihapus, baris ditulis ke antrean DB (`sheet_sync_queue`) sebagai
   *best-effort*. Kegagalan enqueue tidak menggagalkan operasi utama.
2. **Worker mengirim berkala** — antrean diklaim dengan `FOR UPDATE SKIP LOCKED`,
   ditulis ke pihak ketiga, lalu ditandai `sent`/`failed` dengan backoff.
3. **Idempotent** — satu entri per work item (`event_key = sheet:<id>`); payload
   di-*upsert* saat item berubah sehingga perubahan terakhir selalu menang dan
   tidak ada baris ganda.

Panduan UI (halaman *Google Sheets*):
- **Konfigurasi dikelola pengguna**, bukan hardcode: Spreadsheet ID, nama sheet,
  dan kredensial diisi admin dari panel. Nilai awal (`Todo`) hanya saran.
- Kredensial **tidak pernah ditampilkan kembali**; UI menampilkan `client_email`
  + penanda "tersimpan" memakai pola *mask* yang sama dengan provider notifikasi.
- **Kartu status** menampilkan 4 angka antrean (Menunggu/Mengirim/Terkirim/Gagal)
  dengan warna semantik, plus `last_sync_at` dan `last_error` yang dapat dibaca.
- Pesan error pihak ketiga diterjemahkan menjadi bahasa Indonesia yang dapat
  ditindaklanjuti (mis. `403` → "bagikan spreadsheet ke client_email sebagai Editor").

---

## 11. Halaman KPI & Grafik (F20)

Halaman **KPI & SLA** memakai **Recharts** dengan palet dari token desain
(`success`/`warning`/`critical`/`primary`/`info`), konsisten di tema terang/gelap.

| Grafik | Tujuan |
|---|---|
| **Donut** | Komposisi SLA: tepat waktu / terlambat / berjalan |
| **Radial (gauge)** | Skor SLA keseluruhan (0–100) |
| **Bar berkelompok** | met% respons vs penyelesaian **per prioritas** |
| **Area** | Tren penyelesaian harian + persentase tepat waktu |
| **Bar horizontal** | Peringkat person berdasarkan skor |
| **Bar bertumpuk** | Beban kerja per person (selesai/berjalan/pelanggaran) |

Aturan:
- Warna skor: hijau ≥ 80, kuning 50–79, merah < 50 (fungsi `scoreColor`).
- Setiap grafik punya kartu (`ChartCard`) dengan judul + subjudul; tampilkan
  placeholder "Belum ada data" bila kosong agar layout tidak melompat.
- Filter periode memakai rentang tanggal + tombol pintas (Hari ini / 7 hari /
  Bulan ini / Bulan lalu / Semua).

### Catatan penanganan & lampiran (F21)
- Kartu **Catatan Penanganan** pada tab Ringkasan: tiga bagian (Issue ditemukan,
  Troubleshooting, Action/Solusi) dengan mode baca/edit.
- Bagian **Lampiran Pendukung** pada tab Aktivitas: daftar dengan ikon jenis,
  ukuran, pengunggah, dan waktu; unduh/hapus. Tombol unggah menyatu dengan gaya
  `btn-secondary`.
- Panel **Ikut Menangani** menampilkan penangan sebagai chip dengan tombol lepas.
