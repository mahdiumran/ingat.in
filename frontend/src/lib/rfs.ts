/**
 * Helper alur Aktivasi/EWO (RFS).
 *
 * Alur status yang dipakai produk:
 *   new          → dibuat admin/sales (belum dikerjakan)
 *   in_progress  → penangan menekan "Mulai Kerjakan" setelah jadwal tiba
 *   activated    → penangan menekan "Close Ticket" (Closed / Completed)
 *
 * Tombol "Mulai Kerjakan" sengaja tidak aktif sebelum jadwal (expire_at) tiba.
 * Penghitungan di sini hanya untuk UX; backend tetap menolak permintaan yang
 * datang terlalu awal (lihat rfsStartGate di backend).
 */

/** Status RFS yang dianggap sudah selesai/batal (tidak dapat dikerjakan lagi). */
export function isRFSClosed(status?: string, installStage?: string): boolean {
  return (
    status === 'activated' ||
    status === 'cancelled' ||
    installStage === 'activated' ||
    installStage === 'cancelled'
  )
}

/**
 * rfsStartGate menentukan apakah RFS sudah boleh mulai dikerjakan.
 *
 * Mengembalikan `{ allowed, reason }`. Bila jadwal belum tiba, `reason` berisi
 * penjelasan siap tampil (mis. pada tooltip tombol).
 */
export function rfsStartGate(
  status?: string,
  expireAt?: string | null,
  installStage?: string,
): { allowed: boolean; reason: string } {
  if (isRFSClosed(status, installStage)) {
    return { allowed: false, reason: 'RFS sudah ditutup atau dibatalkan' }
  }
  // Sudah dikerjakan: tidak ada yang perlu dimulai lagi.
  if (status === 'in_progress' || status === 'in_progress_field') {
    return { allowed: false, reason: 'RFS sudah dalam pengerjaan' }
  }
  if (!expireAt) {
    return { allowed: true, reason: 'Jadwal RFS belum diisi' }
  }
  const ms = new Date(expireAt).getTime()
  if (Number.isNaN(ms)) {
    return { allowed: true, reason: 'Jadwal RFS tidak valid' }
  }
  if (Date.now() < ms) {
    const at = new Intl.DateTimeFormat('id-ID', {
      timeZone: 'Asia/Jakarta',
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(ms))
    return { allowed: false, reason: `Baru dapat dimulai pada ${at} WIB` }
  }
  return { allowed: true, reason: 'Jadwal sudah tiba — mulai kerjakan' }
}
