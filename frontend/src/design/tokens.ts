/**
 * Token desain Ingat.in.
 *
 * Diturunkan langsung dari m2c/DESIGN.md + m2c/tailwind.config.js (M2Cloud).
 * Ini adalah SATU-SATUNYA tempat nilai warna/font didefinisikan di kode TS.
 * Komponen harus memakai kelas Tailwind (dari tailwind.config.js) atau
 * referensi dari file ini — jangan menulis nilai hex di komponen.
 *
 * CATATAN (F28): objek `colors` di bawah adalah snapshot Light. Warna runtime
 * sesungguhnya berasal dari CSS variable di src/styles.css (`:root` = Light,
 * `.dark` = Dark) yang dipetakan ke kelas Tailwind. Kelas token otomatis ikut
 * berganti tema; hanya nilai hex yang di-hard-code di sini yang perlu varian
 * `dark:` (lihat tagColorClasses).
 */

export const colors = {
  // --- Palet m2c ---
  primary: '#006d36',
  onPrimary: '#ffffff',
  primaryContainer: '#4ade80',
  onPrimaryContainer: '#005e2d',
  primaryFixed: '#6dfe9c',
  primaryFixedDim: '#4de082',

  accentBrand: '#4ADE80', // CTA/brand m2c
  accentSoft: '#BBF7D0', // chip, border logo

  background: '#f9f9ff',
  onBackground: '#141b2b',
  surface: '#f9f9ff',
  surfaceLowest: '#ffffff',
  surfaceLow: '#f1f3ff',
  surfaceContainer: '#e9edff',
  surfaceHigh: '#e1e8fd',
  surfaceHighest: '#dce2f7',

  textPrimary: '#111827',
  textSecondary: '#4B5563',
  onSurface: '#141b2b',
  onSurfaceVariant: '#3d4a3e',

  border: '#E5E7EB',
  outline: '#6d7b6d',
  outlineVariant: '#bccabb',

  error: '#ba1a1a',
  errorContainer: '#ffdad6',
  onErrorContainer: '#93000a',

  // --- Semantik status Ingat.in ---
  success: '#006d36',
  successContainer: '#b4f0c9',
  warning: '#b45309',
  warningContainer: '#fde68a',
  critical: '#ba1a1a',
  criticalContainer: '#ffdad6',
  info: '#31694b',
  infoContainer: '#97d1ac',
  unknown: '#6d7b6d',
} as const

export const fonts = {
  headline: "Inter, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif",
  body: "'Space Grotesk', Inter, 'Segoe UI', sans-serif",
  label: "'JetBrains Mono', 'SFMono-Regular', Consolas, monospace",
} as const

export const radius = {
  control: '8px',
  card: '8px',
  modal: '12px',
  pill: '9999px',
} as const

export const spacing = {
  base: '8px',
  gap: '16px',
  cardPadding: '24px',
  sidebarExpanded: '232px',
  sidebarCollapsed: '72px',
  topbar: '64px',
} as const

/**
 * Pemetaan status → kelas Tailwind.
 * Status TIDAK boleh dibedakan hanya oleh warna: selalu sertakan label teks
 * (lihat komponen StatusBadge).
 */
export const statusTone: Record<string, string> = {
  // task
  open: 'bg-info-container text-on-info-container border-outline-variant',
  in_progress: 'bg-primary-container text-on-primary-container border-outline-variant',
  blocked: 'bg-warning-container text-on-warning-container border-outline-variant',
  done: 'bg-success-container text-on-success-container border-outline-variant',
  cancelled: 'bg-surface-container text-text-secondary border-outline-variant',
  // reminder
  scheduled: 'bg-surface-container text-text-secondary border-outline-variant',
  active: 'bg-primary-container text-on-primary-container border-outline-variant',
  expiring: 'bg-warning-container text-on-warning-container border-outline-variant',
  expired: 'bg-critical-container text-on-critical-container border-outline-variant',
  // rfs
  planned: 'bg-info-container text-on-info-container border-outline-variant',
  in_progress_field: 'bg-primary-container text-on-primary-container border-outline-variant',
  pending_troubleshoot: 'bg-warning-container text-on-warning-container border-outline-variant',
  activated: 'bg-success-container text-on-success-container border-outline-variant',
  postponed: 'bg-warning-container text-on-warning-container border-outline-variant',
  // ticket (F10)
  new: 'bg-info-container text-on-info-container border-outline-variant',
  assigned: 'bg-primary-container text-on-primary-container border-outline-variant',
  pending_customer: 'bg-warning-container text-on-warning-container border-outline-variant',
  resolved: 'bg-success-container text-on-success-container border-outline-variant',
  closed: 'bg-surface-container text-text-secondary border-outline-variant',
  // daily task (F17)
  pending: 'bg-surface-container text-text-secondary border-outline-variant',
  // F22: menunggu konfirmasi pelanggan
  waiting_customer: 'bg-warning-container text-on-warning-container border-outline-variant',
}

/** statusLabel memberi label manusiawi; fallback ganti "_" dengan spasi. */
export const STATUS_LABELS: Record<string, string> = {
  planned: 'Planned',
  in_progress: 'In Progress',
  in_progress_field: 'Penanganan Lapangan',
  pending_troubleshoot: 'Pending Troubleshooting',
  activated: 'Closed / Completed',
  postponed: 'Postponed',
  cancelled: 'Cancelled',
}

export function statusLabel(status: string): string {
  return STATUS_LABELS[status] ?? status.replace(/_/g, ' ')
}
export const priorityTone: Record<string, string> = {
  low: 'bg-surface-container text-text-secondary border-outline-variant',
  normal: 'bg-info-container text-on-info-container border-outline-variant',
  high: 'bg-warning-container text-on-warning-container border-outline-variant',
  critical: 'bg-critical-container text-on-critical-container border-critical',
}

/**
 * Warna tag yang dapat dipilih operator (disimpan di Master Data kind
 * `tag_color` pada meta.color). Tag yang belum dipetakan memakai `gray`.
 */
export const tagColorClasses: Record<string, string> = {
  red: 'bg-critical-container text-on-critical-container border-critical/40',
  orange:
    'bg-[#ffe0c2] text-[#8a4b00] border-[#f0b37a] dark:bg-[#4a2f12] dark:text-[#ffd9ae] dark:border-[#8a5a25]',
  yellow: 'bg-warning-container text-on-warning-container border-outline-variant',
  green: 'bg-success-container text-on-success-container border-outline-variant',
  blue: 'bg-[#d6e4ff] text-[#1b4fd8] border-[#9dbcf7] dark:bg-[#172a52] dark:text-[#aec8ff] dark:border-[#2f4d8a]',
  purple:
    'bg-[#eadcff] text-[#5b21b6] border-[#c9aef5] dark:bg-[#2e1e4d] dark:text-[#d6bcff] dark:border-[#5b3fa0]',
  gray: 'bg-surface-container text-text-secondary border-outline-variant',
}

/** Daftar warna tag untuk form pemilihan (kode -> label). */
export const TAG_COLOR_OPTIONS: { value: string; label: string }[] = [
  { value: 'red', label: 'Merah' },
  { value: 'orange', label: 'Oren' },
  { value: 'yellow', label: 'Kuning' },
  { value: 'green', label: 'Hijau' },
  { value: 'blue', label: 'Biru' },
  { value: 'purple', label: 'Ungu' },
  { value: 'gray', label: 'Abu' },
]

/** tagClass mengembalikan kelas Tailwind untuk sebuah warna tag. */
export function tagClass(color?: string): string {
  return tagColorClasses[color ?? ''] ?? tagColorClasses.gray
}

export const itemTypeLabel: Record<string, string> = {
  task: 'Todo Task',
  reminder: 'Reminder',
  rfs: 'RFS',
  incident: 'Insiden',
  request: 'Permintaan',
  change: 'Change',
  daily_task: 'Daily Task',
}

export const itemTypePrefix: Record<string, string> = {
  task: 'TSK',
  reminder: 'REM',
  rfs: 'RFS',
  incident: 'INC',
  request: 'REQ',
  change: 'CHG',
  daily_task: 'DTK',
}

export const tokens = { colors, fonts, radius, spacing, statusTone, priorityTone, tagColorClasses, tagClass, TAG_COLOR_OPTIONS, itemTypeLabel, itemTypePrefix }
export default tokens
