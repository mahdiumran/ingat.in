/**
 * Helper alur Aktivasi/EWO (RFS).
 *
 * Alur status yang dipakai produk:
 *   new          → dibuat admin/sales (belum dikerjakan)
 *   in_progress  → penangan menekan "Mulai Kerjakan"
 *   activated    → penangan menekan "Close Ticket" (Closed / Completed)
 *
 * Jadwal RFS (expire_at) bersifat pengingat, bukan penghalang mutlak: aktivasi
 * bisa diminta pelanggan lebih awal. Bila dikerjakan sebelum jadwal, penangan
 * wajib mengisi alasan (`needsReason`) dan backend mencatatnya sebagai
 * pengecualian (`early_start`) pada riwayat.
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
 * rfsStartGate menentukan apakah tombol "Mulai Kerjakan" boleh dipakai dan
 * apakah alasan wajib diisi.
 *
 * Mengembalikan `{ allowed, needsReason, reason }`:
 *   - `allowed`     : aksi boleh dilakukan sekarang;
 *   - `needsReason` : jadwal belum tiba → wajib isi alasan lebih dulu;
 *   - `reason`      : penjelasan siap tampil (tooltip/konfirmasi).
 */
export function rfsStartGate(
  status?: string,
  expireAt?: string | null,
  installStage?: string,
): { allowed: boolean; needsReason: boolean; reason: string } {
  if (isRFSClosed(status, installStage)) {
    return { allowed: false, needsReason: false, reason: 'RFS sudah ditutup atau dibatalkan' }
  }
  // Sudah dikerjakan: tidak ada yang perlu dimulai lagi.
  if (status === 'in_progress' || status === 'in_progress_field') {
    return { allowed: false, needsReason: false, reason: 'RFS sudah dalam pengerjaan' }
  }
  if (!expireAt) {
    return { allowed: true, needsReason: false, reason: 'Jadwal RFS belum diisi' }
  }
  const ms = new Date(expireAt).getTime()
  if (Number.isNaN(ms)) {
    return { allowed: true, needsReason: false, reason: 'Jadwal RFS tidak valid' }
  }
  if (Date.now() < ms) {
    const at = new Intl.DateTimeFormat('id-ID', {
      timeZone: 'Asia/Jakarta',
      dateStyle: 'medium',
      timeStyle: 'short',
    }).format(new Date(ms))
    return {
      allowed: true,
      needsReason: true,
      reason: `RFS dijadwalkan ${at} WIB — boleh dikerjakan lebih awal, tetapi isi alasan agar tercatat`,
    }
  }
  return { allowed: true, needsReason: false, reason: 'Jadwal sudah tiba — mulai kerjakan' }
}
