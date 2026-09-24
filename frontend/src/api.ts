/**
 * Klien API Ingat.in.
 *
 * Tanggung jawab:
 *   - menyimpan access token & refresh token
 *   - menyisipkan Bearer header
 *   - memperbarui access token secara otomatis saat menerima 401 (sekali),
 *     dengan antrean agar banyak permintaan paralel tidak memicu banyak refresh
 *   - mengubah respons non-2xx menjadi Error dengan pesan dari {"error": "..."}
 */

const ACCESS_KEY = 'ingatin_access_token'
const REFRESH_KEY = 'ingatin_refresh_token'
const USER_KEY = 'ingatin_user'

export type ApiUser = {
  id: string
  username: string
  email?: string
  full_name?: string
  role: string
  team_id?: string
  telegram_chat_id?: string
  wa_number?: string
  is_active?: boolean
  last_login_at?: string
  created_at?: string
  /** F22: daftar izin efektif (diisi oleh /auth/me) + penanda super user. */
  permissions?: string[]
  is_super?: boolean
}

export type ApiError = Error & { status?: number }

export function getAccessToken(): string | null {
  return localStorage.getItem(ACCESS_KEY)
}

export function getRefreshToken(): string | null {
  return localStorage.getItem(REFRESH_KEY)
}

export function getStoredUser(): ApiUser | null {
  const raw = localStorage.getItem(USER_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as ApiUser
  } catch {
    return null
  }
}

export function setSession(accessToken: string, refreshToken: string, user?: ApiUser | null): void {
  localStorage.setItem(ACCESS_KEY, accessToken)
  localStorage.setItem(REFRESH_KEY, refreshToken)
  if (user) localStorage.setItem(USER_KEY, JSON.stringify(user))
}

export function clearSession(): void {
  localStorage.removeItem(ACCESS_KEY)
  localStorage.removeItem(REFRESH_KEY)
  localStorage.removeItem(USER_KEY)
}

export function isAuthenticated(): boolean {
  return !!getAccessToken()
}

/* ------------------------------------------------------------------------- */
/* Refresh terkoordinasi                                                     */
/* ------------------------------------------------------------------------- */

let refreshPromise: Promise<string | null> | null = null

/**
 * refreshAccessToken menukar refresh token menjadi access token baru.
 * Bila dipanggil bersamaan, hanya satu permintaan jaringan yang dijalankan.
 */
async function refreshAccessToken(): Promise<string | null> {
  if (refreshPromise) return refreshPromise

  refreshPromise = (async () => {
    const refresh = getRefreshToken()
    if (!refresh) return null

    try {
      const res = await fetch('/api/auth/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: refresh }),
      })
      if (!res.ok) {
        clearSession()
        return null
      }
      const data = (await res.json()) as {
        access_token: string
        refresh_token: string
        user?: ApiUser
      }
      setSession(data.access_token, data.refresh_token, data.user ?? null)
      return data.access_token
    } catch {
      clearSession()
      return null
    } finally {
      refreshPromise = null
    }
  })()

  return refreshPromise
}

/** onUnauthorized dipanggil ketika sesi benar-benar tidak dapat dipulihkan. */
let unauthorizedHandler: (() => void) | null = null
export function setUnauthorizedHandler(fn: (() => void) | null): void {
  unauthorizedHandler = fn
}

/* ------------------------------------------------------------------------- */
/* Request                                                                   */
/* ------------------------------------------------------------------------- */

type RequestOptions = RequestInit & { skipAuthRetry?: boolean }

export async function request<T = unknown>(path: string, init: RequestOptions = {}): Promise<T> {
  const { skipAuthRetry, ...rest } = init

  const headers = new Headers(rest.headers)
  // FormData: biarkan browser menetapkan Content-Type (beserta boundary multipart).
  const isFormData = typeof FormData !== 'undefined' && rest.body instanceof FormData
  if (!headers.has('Content-Type') && rest.body && !isFormData) {
    headers.set('Content-Type', 'application/json')
  }
  const token = getAccessToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(`/api${path}`, { ...rest, headers })

  // Access token kedaluwarsa: coba perbarui sekali lalu ulangi permintaan.
  if (res.status === 401 && !skipAuthRetry && getRefreshToken()) {
    const newToken = await refreshAccessToken()
    if (newToken) {
      return request<T>(path, { ...init, skipAuthRetry: true })
    }
    unauthorizedHandler?.()
  }

  if (res.status === 204) return undefined as T

  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }

  if (!res.ok) {
    const message =
      data && typeof data === 'object' && 'error' in data
        ? String((data as { error: unknown }).error)
        : `Permintaan gagal (${res.status})`
    const err = new Error(message) as ApiError
    err.status = res.status
    throw err
  }

  return data as T
}

/* ------------------------------------------------------------------------- */
/* Endpoint                                                                  */
/* ------------------------------------------------------------------------- */

export type LoginResponse = {
  access_token: string
  refresh_token: string
  token_type: string
  expires_in: number
  user: ApiUser
}

export const api = {
  health: () =>
    request<{ status: string; database: string; version: string; mode: string }>('/health', {
      skipAuthRetry: true,
    }),

  version: () =>
    request<{
      app: string
      version: string
      schema_version: number
      timezone: string
      access_token_minutes: number
      refresh_token_days: number
    }>('/version', { skipAuthRetry: true }),

  login: (username: string, password: string) =>
    request<LoginResponse>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
      skipAuthRetry: true,
    }),

  logout: async () => {
    const refresh = getRefreshToken()
    if (refresh) {
      try {
        await request('/auth/logout', {
          method: 'POST',
          body: JSON.stringify({ refresh_token: refresh }),
          skipAuthRetry: true,
        })
      } catch {
        /* logout tetap dianggap berhasil di sisi klien */
      }
    }
    clearSession()
  },

  me: () => request<ApiUser>('/auth/me'),

  changePassword: (currentPassword: string, newPassword: string) =>
    request<{ ok: boolean; message: string }>('/auth/change-password', {
      method: 'POST',
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    }),

  sessions: () =>
    request<{ sessions: { id: string; user_agent: string; ip: string; expires_at: string; created_at: string }[]; total: number }>(
      '/auth/sessions',
    ),

  logoutAll: () => request<{ ok: boolean }>('/auth/logout-all', { method: 'POST' }),

  users: () => request<{ users: ApiUser[]; total: number }>('/users'),

  createUser: (payload: {
    username: string
    password: string
    role: string
    email?: string
    full_name?: string
    team_id?: string
    telegram_chat_id?: string
    wa_number?: string
  }) => request<ApiUser>('/users', { method: 'POST', body: JSON.stringify(payload) }),

  updateUser: (
    id: string,
    payload: {
      email?: string
      full_name?: string
      role?: string
      is_active?: boolean
      team_id?: string
      telegram_chat_id?: string
      wa_number?: string
    },
  ) => request<ApiUser>(`/users/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  resetUserPassword: (id: string, newPassword: string) =>
    request<{ ok: boolean }>(`/users/${id}/reset-password`, {
      method: 'POST',
      body: JSON.stringify({ new_password: newPassword }),
    }),

  deleteUser: (id: string) => request<{ ok: boolean }>(`/users/${id}`, { method: 'DELETE' }),

  teams: () =>
    request<{
      teams: {
        id: string
        name: string
        description: string
        category: string
        target_id?: string
        is_active: boolean
      }[]
      total: number
    }>('/teams'),

  createTeam: (payload: { name: string; description?: string; category?: string; target_id?: string }) =>
    request<{ id: string; name: string }>('/teams', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  updateTeam: (
    id: string,
    payload: {
      name?: string
      description?: string
      category?: string
      is_active?: boolean
      target_id?: string
    },
  ) =>
    request<{ id: string; name: string }>(`/teams/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  deleteTeam: (id: string) => request<{ ok: boolean }>(`/teams/${id}`, { method: 'DELETE' }),

  audit: (params: Record<string, string> = {}) => {
    const qs = new URLSearchParams(params).toString()
    return request<{
      logs: {
        id: number
        username: string
        action: string
        entity_type: string
        entity_id: string
        detail: Record<string, unknown>
        ip: string
        success: boolean
        created_at: string
      }[]
      total: number
    }>(`/audit${qs ? `?${qs}` : ''}`)
  },
}

/* ------------------------------------------------------------------------- */
/* Work items (F5–F7)                                                        */
/* ------------------------------------------------------------------------- */

export type WorkItem = {
  id: string
  ref_no: string
  item_type: string
  title: string
  description: string
  priority: string
  status: string
  stage: string
  owner_username: string
  requester_username: string
  team_id?: string
  target_id?: string
  due_at?: string
  expire_at?: string
  start_at?: string
  device_ref: string
  service_ref: string
  customer_ref: string
  tags: string[]
  created_by: string
  updated_by_username: string
  created_at: string
  updated_at: string
  // F27: waktu penyelesaian + aktor penyelesai (turunan).
  completed_at?: string
  completed_by?: string
  task?: {
    checklist: { text: string; done: boolean }[]
    progress_pct: number
    estimate_minutes?: number
    completion_note?: string
    // F24: jenis daily task + hasil penyelesaian (normal/bermasalah).
    daily_task_type?: string
    result_status?: string
  }
  reminder?: {
    category: string
    subject_name: string
    subject_type: string
    escalation_policy_id?: string
    activated_at?: string
    last_offset_fired: string
    recurrence_rule?: string
  }
  rfs?: {
    customer_name: string
    service_id: string
    service_package: string
    bandwidth: string
    pic_noc: string
    pic_sales: string
    // F25: PIC berupa tim.
    pic_team_id?: string
    site: string
    install_stage: string
    // F23: catatan penanganan (modal Troubleshoot).
    issue_found?: string
    troubleshooting?: string
    action_solution?: string
  }
  // F25: data teknis aktivasi RFS.
  activation?: {
    work_item_id: string
    ip_address: string
    vlan_detail: string
    interface_port: string
    bandwidth_test: string
    ping_test: string
    packet_loss: string
    updated_by: string
    updated_at: string
  }
  ticket?: {
    work_item_id: string
    category: string
    subcategory: string
    incident_type: string
    impact: string
    urgency: string
    assignment_group: string
    escalation_level: number
    reopen_count: number
    issue_found: string
    troubleshooting: string
    action_solution: string
  }
  // F20: penangan tambahan + siklus SLA.
  collaborators?: Collaborator[]
  sla_cycles?: SLACycle[]
  reopen_count?: number
  // SLA turunan (tiket): durasi dari first_response_at sampai closed_at/now.
  sla_seconds?: number
  sla_running?: boolean
  sla_start_at?: string
  sla_end_at?: string
}

export type Collaborator = {
  id: string
  work_item_id: string
  username: string
  added_by: string
  created_at: string
}

export type SLACycle = {
  id: string
  work_item_id: string
  cycle_no: number
  opened_at: string
  reopened_at?: string
  closed_at?: string
  first_response_at?: string
  handler_username: string
  closed_by: string
}

export type Attachment = {
  id: string
  work_item_id: string
  filename: string
  size_bytes: number
  mime: string
  uploaded_by: string
  created_at: string
}

export type WorkItemEvent = {
  id: number
  work_item_id: string
  event_type: string
  actor_username: string
  from_value: string
  to_value: string
  detail: Record<string, unknown>
  created_at: string
}

export type Comment = {
  id: string
  work_item_id: string
  author_username: string
  body: string
  is_internal: boolean
  created_at: string
}

export type WorkflowInfo = {
  item_type: string
  name: string
  states: string[]
  initial_state: string
  terminal_states: string[]
  transitions: Record<string, string[]>
}

export type Target = {
  id: string
  name: string
  kind: string
  notes: string
  is_active: boolean
  bindings?: Binding[]
}

export type Binding = {
  id: string
  target_id: string
  channel: string
  destination: string
  provider_id?: string
  label: string
  is_primary: boolean
  is_active: boolean
  verified_at?: string
}

export type NotificationProviderRecord = {
  id: string
  channel: string
  kind: string
  label: string
  base_url: string
  has_api_key: boolean
  extra: Record<string, unknown>
  is_default: boolean
  is_active: boolean
  last_test_at?: string
  last_test_ok?: boolean
  last_test_error: string
}

export type NotificationTemplate = {
  id: string
  key: string
  item_type?: string
  channel?: string
  subject_tpl: string
  body_tpl: string
  severity: string
  description: string
  is_active: boolean
}

export type EscalationPolicy = {
  id: string
  name: string
  description: string
  offsets: { label: string; hours_before?: number; hours_after?: number; severity: string }[]
  applies_to_item_types: string[]
  quiet_hours_from?: string
  quiet_hours_to?: string
  max_attempts: number
  is_default: boolean
  is_active: boolean
}

export type DashboardData = {
  total: number
  open_total: number
  overdue_total: number
  due_today_total: number
  expiring_soon: number
  expired_total: number
  rfs_upcoming: number
  notif_failed_24h: number
  notif_pending: number
  by_type_status: Record<string, Record<string, number>>
  upcoming: WorkItem[]
}

export type OutboxRow = {
  id: number
  event_key: string
  channel: string
  destination: string
  offset_label: string
  severity: string
  status: string
  attempts: number
  max_attempts: number
  last_error: string
  provider_message_id: string
  sent_at?: string
  next_attempt_at: string
  created_at: string
}

export const workItemsApi = {
  list: (params: Record<string, string> = {}) => {
    const qs = new URLSearchParams(params).toString()
    return request<{ items: WorkItem[]; total: number }>(`/items${qs ? `?${qs}` : ''}`)
  },

  get: (id: string) =>
    request<{
      item: WorkItem
      events: WorkItemEvent[]
      comments: Comment[]
      workflow: WorkflowInfo | null
    }>(`/items/${id}`),

  create: (payload: Record<string, unknown>) =>
    request<WorkItem>('/items', { method: 'POST', body: JSON.stringify(payload) }),

  update: (id: string, payload: Record<string, unknown>) =>
    request<WorkItem>(`/items/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  changeStatus: (id: string, status: string, note = '', resultStatus = '') =>
    request<WorkItem>(`/items/${id}/status`, {
      method: 'POST',
      body: JSON.stringify({ status, note, ...(resultStatus ? { result_status: resultStatus } : {}) }),
    }),

  /**
   * escalateTicket (F24) — membuat tiket incident/request dari Daily Task
   * "bermasalah" dan menautkannya (idempotent).
   */
  escalateTicket: (
    id: string,
    payload: {
      ticket_type: 'incident' | 'request'
      title?: string
      note?: string
      priority?: string
      ticket?: Record<string, unknown>
    },
  ) =>
    request<WorkItem>(`/items/${id}/escalate-ticket`, {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  /** forceStatus (admin) — melompati aturan transisi; alasan wajib. */
  forceStatus: (id: string, status: string, reason: string) =>
    request<WorkItem>(`/items/${id}/force-status`, {
      method: 'POST',
      body: JSON.stringify({ status, note: reason }),
    }),

  /** forceUnlock (admin): buka tiket yang sudah closed/completed tanpa aturan transisi. */
  forceUnlock: (id: string, status: string, note: string) =>
    request<WorkItem>(`/items/${id}/force-unlock`, {
      method: 'POST',
      body: JSON.stringify({ status, note }),
    }),

  /**
   * assign menetapkan owner (penanggung jawab utama). F25: bila owner sudah
   * terisi, hanya admin yang boleh mengganti dan `reason` wajib diisi.
   */
  assign: (id: string, ownerUsername: string, opts: { reason?: string; force?: boolean } = {}) =>
    request<WorkItem>(`/items/${id}/assign`, {
      method: 'POST',
      body: JSON.stringify({
        owner_username: ownerUsername,
        ...(opts.reason ? { reason: opts.reason } : {}),
        ...(opts.force ? { force: true } : {}),
      }),
    }),

  /** updateRFS menyimpan detail RFS termasuk PIC team + data aktivasi (F25). */
  updateRFS: (id: string, rfs: Record<string, unknown>) =>
    request<WorkItem>(`/items/${id}`, {
      method: 'PATCH',
      body: JSON.stringify({ rfs }),
    }),

  addCollaborator: (id: string, username: string) =>
    request<{ ok: boolean; inserted: boolean; collaborators: Collaborator[] }>(
      `/items/${id}/collaborators`,
      { method: 'POST', body: JSON.stringify({ username }) },
    ),

  removeCollaborator: (id: string, username: string) =>
    request<{ ok: boolean; removed: boolean; collaborators: Collaborator[] }>(
      `/items/${id}/collaborators/${encodeURIComponent(username)}`,
      { method: 'DELETE' },
    ),

  remove: (id: string) => request<{ ok: boolean }>(`/items/${id}`, { method: 'DELETE' }),

  /** F23: batalkan Aktivasi/EWO (RFS). */
  rfsCancel: (id: string, reason = '') =>
    request<WorkItem>(`/items/${id}/rfs-cancel`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    }),

  /** F23: hapus Aktivasi/EWO dengan alasan wajib. */
  rfsDelete: (id: string, reason: string) =>
    request<{ ok: boolean; deleted: number }>(`/items/${id}/rfs-delete`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    }),

  addComment: (id: string, body: string) =>
    request<Comment>(`/items/${id}/comments`, {
      method: 'POST',
      body: JSON.stringify({ body, is_internal: true }),
    }),
  triggerNotification: (
    id: string,
    payload: {
      template_key?: string
      offset_label?: string
      severity?: string
      note?: string
      reset_reminder_offset?: boolean
    } = {},
  ) =>
    request<{ ok: boolean; added: number; skipped: number; message: string }>(`/items/${id}/notify`, {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  dashboard: () => request<DashboardData>('/dashboard'),
}

/* ------------------------------------------------------------------------- */
/* Daily Task (F17) — checklist harian bergaya todo list                     */
/* ------------------------------------------------------------------------- */

/** Status Daily Task (sinkron dengan workitems.DefaultWorkflows["daily_task"]). */
export const DAILY_TASK_STATUSES = ['pending', 'in_progress', 'done', 'canceled'] as const

export const dailyTaskApi = {
  /**
   * listForDay mengambil daily task satu hari (zona WIB, YYYY-MM-DD) plus
   * carry-over task yang belum selesai dari hari-hari sebelumnya.
   */
  listForDay: (date: string, opts: { owner?: string; q?: string; status?: string; carryOver?: boolean; teamId?: string } = {}) => {
    const params: Record<string, string> = { type: 'daily_task', date, limit: '200' }
    if (opts.owner) params.owner = opts.owner
    if (opts.q) params.q = opts.q
    if (opts.status) params.status = opts.status
    if (opts.carryOver) params.carry_over = 'true'
    if (opts.teamId) params.team_id = opts.teamId
    const qs = new URLSearchParams(params).toString()
    return request<{ items: WorkItem[]; total: number }>(`/items?${qs}`)
  },

  create: (payload: {
    title: string
    description?: string
    priority?: string
    owner_username?: string
    date: string
    parent_id?: string
    target_id?: string
    tags?: string[]
    status?: string
    // F24: jenis daily task (kode master data `daily_task_type`).
    daily_task_type?: string
    // F31: tim pemilik (admin saja; non-admin dipaksa ke tim sendiri di server).
    team_id?: string
  }) => {
    // start_at = awal hari WIB, due_at = akhir hari WIB (label konsisten).
    const start = new Date(`${payload.date}T00:00:00+07:00`).toISOString()
    const end = new Date(`${payload.date}T23:59:00+07:00`).toISOString()
    return request<WorkItem>('/items', {
      method: 'POST',
      body: JSON.stringify({
        item_type: 'daily_task',
        title: payload.title,
        description: payload.description ?? '',
        priority: payload.priority ?? 'normal',
        owner_username: payload.owner_username ?? '',
        start_at: start,
        due_at: end,
        parent_id: payload.parent_id ?? '',
        target_id: payload.target_id ?? '',
        ...(payload.team_id ? { team_id: payload.team_id } : {}),
        tags: payload.tags ?? [],
        ...(payload.status ? { status: payload.status } : {}),
        ...(payload.daily_task_type
          ? { task: { daily_task_type: payload.daily_task_type } }
          : {}),
      }),
    })
  },

  /** types (F24) — daftar jenis Daily Task dari master data + flag can-ticket. */
  types: async (): Promise<{ code: string; label: string; canTicket: boolean }[]> => {
    const res = await masterDataApi.list({ kind: 'daily_task_type', active: 'true' })
    return res.entries.map((e) => ({
      code: e.code,
      label: e.label,
      canTicket: Boolean((e.meta as { dapat_membuat_tiket?: boolean })?.dapat_membuat_tiket),
    }))
  },
}

/* ------------------------------------------------------------------------- */
/* Google Sheets sync (F18)                                                  */
/* ------------------------------------------------------------------------- */

export type SheetSyncConfig = {
  enabled: boolean
  spreadsheet_id: string
  sheet_name: string
  service_account_set: boolean
  client_email: string
  header_written: boolean
  last_sync_at?: string
  last_error: string
}

export type SheetSyncQueueCounts = {
  pending: number
  sending: number
  sent: number
  failed: number
}

export const sheetSyncApi = {
  get: () => request<{ config: SheetSyncConfig; queue: SheetSyncQueueCounts }>('/sheet-sync'),

  save: (payload: {
    enabled?: boolean
    spreadsheet_id?: string
    sheet_name?: string
    service_account_json?: string
    reset_header?: boolean
  }) =>
    request<{ config: SheetSyncConfig; queue: SheetSyncQueueCounts }>('/sheet-sync', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  test: () =>
    request<{ ok: boolean; client_email: string; sheet_name: string }>('/sheet-sync/test', {
      method: 'POST',
    }),

  createTab: () =>
    request<{ ok: boolean; sheet_name: string }>('/sheet-sync/create-tab', { method: 'POST' }),
}

/* ------------------------------------------------------------------------- */
/* Cadangan & pemulihan (F34, admin only)                                    */
/* ------------------------------------------------------------------------- */

export type BackupInfo = {
  name: string
  size_bytes: number
  created_at: string
  uploaded: boolean
  uploaded_at?: string
  upload_error?: string
}

export type BackupFTPConfig = {
  enabled: boolean
  host: string
  port: number
  username: string
  password_set: boolean
  dir: string
  passive: boolean
}

export type BackupConfig = {
  enabled: boolean
  schedule: string
  keep_days: number
  ftp: BackupFTPConfig
  last_run_at?: string
  last_status: string
  last_error: string
  last_file: string
}

export const backupApi = {
  list: () =>
    request<{
      config: BackupConfig
      backups: BackupInfo[]
      dir: string
      tools: { pg_dump: boolean; pg_restore: boolean }
    }>('/backup'),

  create: () =>
    request<{ ok: boolean; backup: BackupInfo; upload?: string }>('/backup', { method: 'POST' }),

  saveConfig: (payload: {
    enabled?: boolean
    schedule?: string
    keep_days?: number
    ftp_password?: string
    ftp?: {
      enabled?: boolean
      host?: string
      port?: number
      username?: string
      dir?: string
      passive?: boolean
    }
  }) =>
    request<{ config: BackupConfig; backups: BackupInfo[]; dir: string }>('/backup/config', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  testFTP: () =>
    request<{ ok: boolean; message: string }>('/backup/ftp/test', { method: 'POST' }),

  /** uploadFile mengunggah berkas .dump dari komputer; opsional langsung restore. */
  uploadFile: (file: File, restore: boolean) => {
    const fd = new FormData()
    fd.append('file', file)
    if (restore) {
      fd.append('restore', 'true')
      fd.append('confirm', 'RESTORE')
    }
    return request<{ ok: boolean; backup: BackupInfo; restored?: boolean; message: string }>(
      '/backup/upload',
      { method: 'POST', body: fd },
    )
  },

  upload: (name: string) =>
    request<{ ok: boolean; message: string }>(`/backup/${encodeURIComponent(name)}/upload`, {
      method: 'POST',
    }),

  restore: (name: string) =>
    request<{ ok: boolean; message: string }>(`/backup/${encodeURIComponent(name)}/restore`, {
      method: 'POST',
      body: JSON.stringify({ confirm: 'RESTORE' }),
    }),

  remove: (name: string) =>
    request<{ ok: boolean }>(`/backup/${encodeURIComponent(name)}`, { method: 'DELETE' }),

  /** download mengunduh berkas cadangan (perlu header Authorization). */
  download: async (name: string) => {
    const token = getAccessToken()
    const res = await fetch(`/api/backup/${encodeURIComponent(name)}`, {
      headers: token ? { Authorization: `Bearer ${token}` } : undefined,
    })
    if (!res.ok) {
      let message = `Unduhan gagal (${res.status})`
      try {
        const data = await res.json()
        if (data && typeof data === 'object' && 'error' in data) message = String(data.error)
      } catch {
        /* abaikan */
      }
      throw new Error(message)
    }
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = name
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  },
}

/* ------------------------------------------------------------------------- */
/* Master data & role permissions (F12)                                      */
/* ------------------------------------------------------------------------- */

export type MasterDataEntry = {
  id: string
  kind: string
  code: string
  label: string
  description: string
  parent_id?: string
  sort_order: number
  meta: Record<string, unknown>
  is_active: boolean
  created_by: string
  created_at: string
  updated_at: string
}

export type MasterDataKind = {
  kind: string
  label: string
  description: string
  icon: string
  is_system: boolean
  sort_order: number
}

export type RolePermission = {
  role: string
  action: string
  allowed: boolean
  updated_by: string
  updated_at: string
}

export const masterDataApi = {
  list: (params: Record<string, string> = {}) => {
    const qs = new URLSearchParams(params).toString()
    return request<{ entries: MasterDataEntry[]; kinds: MasterDataKind[]; total: number }>(
      `/master-data${qs ? `?${qs}` : ''}`,
    )
  },

  create: (payload: {
    kind: string
    code?: string
    label: string
    description?: string
    parent_id?: string
    sort_order?: number
    meta?: Record<string, unknown>
    is_active?: boolean
  }) => request<MasterDataEntry>('/master-data', { method: 'POST', body: JSON.stringify(payload) }),

  update: (
    id: string,
    payload: {
      code?: string
      label?: string
      description?: string
      parent_id?: string
      clear_parent?: boolean
      sort_order?: number
      meta?: Record<string, unknown>
      is_active?: boolean
    },
  ) => request<MasterDataEntry>(`/master-data/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  remove: (id: string) => request<{ ok: boolean }>(`/master-data/${id}`, { method: 'DELETE' }),

  createKind: (payload: { kind: string; label: string; description?: string; icon?: string }) =>
    request<{ ok: boolean; kind: string }>('/master-data/kinds', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  rolePermissions: () =>
    request<{
      permissions: RolePermission[]
      roles: string[]
      role_labels?: Record<string, string>
      role_is_super?: Record<string, boolean>
      actions: string[]
    }>('/role-permissions'),

  setRolePermission: (role: string, action: string, allowed: boolean) =>
    request<{ ok: boolean }>('/role-permissions', {
      method: 'POST',
      body: JSON.stringify({ role, action, allowed }),
    }),
}

/* ------------------------------------------------------------------------- */
/* F22 — Peran dinamis (roles)                                              */
/* ------------------------------------------------------------------------- */

export type RoleDef = {
  role: string
  label: string
  description: string
  is_super: boolean
  is_system: boolean
  rank: number
  created_at: string
  updated_at: string
}

export const rolesApi = {
  list: () => request<{ roles: RoleDef[]; total: number }>('/roles'),

  create: (payload: { role: string; label: string; description?: string; rank?: number }) =>
    request<RoleDef>('/roles', { method: 'POST', body: JSON.stringify(payload) }),

  update: (role: string, payload: { label?: string; description?: string; rank?: number }) =>
    request<RoleDef>(`/roles/${role}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  remove: (role: string) => request<{ ok: boolean }>(`/roles/${role}`, { method: 'DELETE' }),
}

/* ------------------------------------------------------------------------- */
/* F22 — Ringkasan tugas harian (daily summary)                             */
/* ------------------------------------------------------------------------- */

export type DailySummarySettings = {
  mode: 'on_change' | 'interval' | 'off'
  interval_minutes: number
  modes: string[]
}

export const dailySummaryApi = {
  get: () => request<DailySummarySettings>('/settings/daily-summary'),

  save: (payload: { mode: string; interval_minutes?: number }) =>
    request<DailySummarySettings>('/settings/daily-summary', {
      method: 'POST',
      body: JSON.stringify(payload),
    }),

  preview: () => request<{ ok: boolean; added: number }>('/settings/daily-summary/preview', { method: 'POST' }),
}

/* ------------------------------------------------------------------------- */
/* Notifikasi sidebar                                                        */
/* ------------------------------------------------------------------------- */

export type NotificationFeedItem = {
  id: number
  work_item_id: string
  ref_no: string
  title: string
  item_type: string
  event_type: string
  actor_username: string
  from_value: string
  to_value: string
  detail: Record<string, unknown>
  created_at: string
}

export const notificationsApi = {
  list: (params: Record<string, string> = {}) => {
    const qs = new URLSearchParams(params).toString()
    return request<{
      notifications: NotificationFeedItem[]
      total: number
      max_id: number
      server_time: string
    }>(`/notifications${qs ? `?${qs}` : ''}`)
  },

  markRead: (lastSeenId: number) =>
    request<{ ok: boolean; last_seen_id: number }>('/notifications/read', {
      method: 'POST',
      body: JSON.stringify({ last_seen_id: lastSeenId }),
    }),
}

export const notificationApi = {
  providers: () =>
    request<{
      providers: NotificationProviderRecord[]
      total: number
      supported_kinds: string[]
      kinds_by_channel: Record<string, string[]>
    }>('/providers'),

  createProvider: (payload: Record<string, unknown>) =>
    request<NotificationProviderRecord>('/providers', { method: 'POST', body: JSON.stringify(payload) }),

  updateProvider: (id: string, payload: Record<string, unknown>) =>
    request<NotificationProviderRecord>(`/providers/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  deleteProvider: (id: string) => request<{ ok: boolean }>(`/providers/${id}`, { method: 'DELETE' }),

  testProvider: (id: string) =>
    request<{ ok: boolean; error?: string }>(`/providers/${id}/test`, { method: 'POST' }),

  providerSession: (id: string) =>
    request<{ reachable: boolean; sessions?: Record<string, unknown>[]; error?: string; qr_url: string }>(
      `/providers/${id}/session`,
    ),

  providerSessionAction: (id: string, action: 'start' | 'stop' | 'logout') =>
    request<{ ok: boolean }>(`/providers/${id}/session/${action}`, { method: 'POST' }),

  sendTestMessage: (id: string, destination: string, message = '') =>
    request<{ ok: boolean; error?: string; message_id?: string; retryable?: boolean }>(
      `/providers/${id}/send-test`,
      { method: 'POST', body: JSON.stringify({ destination, message }) },
    ),

  targets: () => request<{ targets: Target[]; total: number }>('/targets'),

  /**
   * targetOptions (F32) — daftar target ringan (id+nama) untuk dropdown form.
   * Terbuka untuk semua role; hanya nama yang dibagikan.
   */
  targetOptions: () =>
    request<{ targets: { id: string; name: string; kind: string; is_active: boolean }[]; total: number }>(
      '/targets/options',
    ),

  createTarget: (payload: { name: string; kind: string; notes?: string }) =>
    request<Target>('/targets', { method: 'POST', body: JSON.stringify(payload) }),

  updateTarget: (id: string, payload: Record<string, unknown>) =>
    request<Target>(`/targets/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  deleteTarget: (id: string) => request<{ ok: boolean }>(`/targets/${id}`, { method: 'DELETE' }),

  createBinding: (targetId: string, payload: Record<string, unknown>) =>
    request<Binding>(`/targets/${targetId}/bindings`, { method: 'POST', body: JSON.stringify(payload) }),

  updateBinding: (id: string, payload: Record<string, unknown>) =>
    request<Binding>(`/bindings/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  deleteBinding: (id: string) => request<{ ok: boolean }>(`/bindings/${id}`, { method: 'DELETE' }),

  testBinding: (id: string) =>
    request<{ ok: boolean; error?: string; provider?: string; message_id?: string }>(`/bindings/${id}/test`, {
      method: 'POST',
    }),

  templates: () => request<{ templates: NotificationTemplate[]; total: number }>('/templates'),

  updateTemplate: (id: string, payload: Record<string, unknown>) =>
    request<NotificationTemplate>(`/templates/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  policies: () => request<{ policies: EscalationPolicy[]; total: number }>('/policies'),

  createPolicy: (payload: Record<string, unknown>) =>
    request<EscalationPolicy>('/policies', { method: 'POST', body: JSON.stringify(payload) }),

  updatePolicy: (id: string, payload: Record<string, unknown>) =>
    request<EscalationPolicy>(`/policies/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  deletePolicy: (id: string) => request<{ ok: boolean }>(`/policies/${id}`, { method: 'DELETE' }),

  outbox: (params: Record<string, string> = {}) => {
    const qs = new URLSearchParams(params).toString()
    return request<{ outbox: OutboxRow[]; total: number; by_status: Record<string, number> }>(
      `/outbox${qs ? `?${qs}` : ''}`,
    )
  },
}

/* ------------------------------------------------------------------------- */
/* Lampiran (F21)                                                            */
/* ------------------------------------------------------------------------- */

export const attachmentsApi = {
  list: (itemId: string) =>
    request<{ attachments: Attachment[]; max_bytes: number }>(`/items/${itemId}/attachments`),

  upload: (itemId: string, file: File) => {
    const fd = new FormData()
    fd.append('file', file)
    return request<Attachment>(`/items/${itemId}/attachments`, { method: 'POST', body: fd })
  },

  /** deleteUrl/unduh memakai endpoint ber-auth (fetch + blob). */
  remove: (attachmentId: string) =>
    request<{ ok: boolean }>(`/attachments/${attachmentId}`, { method: 'DELETE' }),

  downloadUrl: (attachmentId: string) => `/api/attachments/${attachmentId}`,
}

/* ------------------------------------------------------------------------- */
/* KPI & SLA (F20)                                                           */
/* ------------------------------------------------------------------------- */

export type KPISummary = {
  tickets_total: number
  tickets_closed: number
  tickets_open: number
  response_met: number
  response_total: number
  response_met_pct: number
  resolution_met: number
  resolution_total: number
  resolution_met_pct: number
  avg_resolution_seconds: number
  median_resolution_seconds: number
  p90_resolution_seconds: number
  breached: number
  sla_score: number
  todo_total: number
  todo_on_time: number
  todo_on_time_pct: number
  daily_total: number
  daily_on_time: number
  daily_on_time_pct: number
}

export type KPIPerson = {
  username: string
  assigned_total: number
  resolved_total: number
  open_total: number
  response_met_pct: number
  resolution_met_pct: number
  avg_resolution_seconds: number
  breached: number
  sla_score: number
  todo_total: number
  todo_on_time: number
  daily_total: number
  daily_on_time: number
}

export type KPIByPriority = {
  priority: string
  total: number
  response_met_pct: number
  resolution_met_pct: number
  avg_resolution_seconds: number
}

export type KPITrendPoint = {
  day: string
  closed: number
  resolution_met: number
  resolution_met_pct: number
}

export type KPIResult = {
  period: { from: string; to: string }
  summary: KPISummary
  by_priority: KPIByPriority[]
  trend: KPITrendPoint[]
  per_person: KPIPerson[]
}

export const kpiApi = {
  get: (params: Record<string, string> = {}) => {
    const qs = new URLSearchParams(params).toString()
    return request<KPIResult>(`/kpi/sla${qs ? `?${qs}` : ''}`)
  },
  exportCsvUrl: (params: Record<string, string> = {}) => {
    const qs = new URLSearchParams(params).toString()
    return `/api/kpi/sla/export.csv${qs ? `?${qs}` : ''}`
  },
  /** users mengembalikan daftar username untuk filter KPI. */
  users: () => request<{ users: string[]; total: number }>('/kpi/users'),
}

/* ------------------------------------------------------------------------- */
/* F26 — Catatan (sticky notes) dengan tim pemilik + berbagi antar tim        */
/* ------------------------------------------------------------------------- */

export type NoteSharedTeam = { team_id: string; name: string }

export type Note = {
  id: string
  title: string
  body: string
  visibility: 'internal' | 'eksternal'
  owner_team_id?: string
  owner_team_name?: string
  color: string
  pinned: boolean
  created_by: string
  updated_by_username: string
  created_at: string
  updated_at: string
  shared_team_ids?: string[]
  shared_teams?: NoteSharedTeam[]
}

export const notesApi = {
  list: (params: { visibility?: string; q?: string; limit?: number } = {}) => {
    const qs = new URLSearchParams()
    if (params.visibility) qs.set('visibility', params.visibility)
    if (params.q) qs.set('q', params.q)
    if (params.limit) qs.set('limit', String(params.limit))
    const s = qs.toString()
    return request<{ notes: Note[]; total: number }>(`/notes${s ? `?${s}` : ''}`)
  },

  create: (payload: {
    title?: string
    body?: string
    visibility?: 'internal' | 'eksternal'
    owner_team_id?: string
    color?: string
    pinned?: boolean
    shared_team_ids?: string[]
  }) => request<Note>('/notes', { method: 'POST', body: JSON.stringify(payload) }),

  update: (
    id: string,
    payload: {
      title?: string
      body?: string
      visibility?: 'internal' | 'eksternal'
      owner_team_id?: string
      color?: string
      pinned?: boolean
      shared_team_ids?: string[]
    },
  ) => request<Note>(`/notes/${id}`, { method: 'PATCH', body: JSON.stringify(payload) }),

  setShares: (id: string, teamIds: string[]) =>
    request<Note>(`/notes/${id}/shares`, {
      method: 'PATCH',
      body: JSON.stringify({ team_ids: teamIds }),
    }),

  remove: (id: string) => request<{ ok: boolean }>(`/notes/${id}`, { method: 'DELETE' }),
}
