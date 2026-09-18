/**
 * Helper format durasi & SLA.
 *
 * SLA tiket = durasi dari `sla_start_at` (first_response_at) sampai closed_at
 * (final) atau sekarang (berjalan). Lihat DESIGN.md §8.
 */

/** formatDuration mengubah detik menjadi "Xd Yh Zm" / "Yh Zm" / "Zm". */
export function formatDuration(seconds?: number): string {
  if (seconds == null || seconds < 0) return '—'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}h ${h}j ${m}m`
  if (h > 0) return `${h}j ${m}m`
  return `${m}m`
}

/**
 * slaTone mengembalikan kelas warna untuk durasi SLA (dalam detik):
 * hijau < 4 jam, kuning 4–24 jam, merah > 24 jam.
 */
export function slaTone(seconds?: number): string {
  if (seconds == null) return 'text-text-secondary'
  if (seconds < 4 * 3600) return 'text-success font-semibold'
  if (seconds < 24 * 3600) return 'text-warning font-semibold'
  return 'text-critical font-semibold'
}
