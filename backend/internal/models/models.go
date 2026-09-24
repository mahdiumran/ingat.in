// Package models berisi struct domain yang dipakai bersama oleh repository,
// service, dan API.
package models

import (
	"time"

	"github.com/google/uuid"
)

// Role user.
//
// Role kini dinamis (tabel `roles`); konstanta di bawah adalah peran bawaan
// yang punya perlakuan khusus di kode. Peran lain dapat dibuat dari panel.
const (
	RoleAdmin    = "admin"   // super user — selalu seluruh izin
	RoleManager  = "manager" // manajerial (admin di bawah super user)
	RoleSPV      = "spv"     // supervisor operasional
	RoleOwner    = "owner"   // pemilik/pimpinan
	RoleAgent    = "agent"
	RoleNOC      = "noc"
	RoleSales    = "sales"
	RoleViewer   = "viewer"
	RoleCustomer = "customer"
)

// DefaultRoleSet adalah peran manajerial yang boleh melihat KPI/SLA.
// Selaras dengan permission action "kpi.view" (admin selalu boleh).
var DefaultRoleSet = []string{RoleAdmin, RoleManager, RoleSPV, RoleOwner, RoleNOC, RoleAgent, RoleSales, RoleViewer, RoleCustomer}

// Role adalah satu peran dinamis (tabel roles).
type Role struct {
	Role        string    `json:"role"`
	Label       string    `json:"label"`
	Description string    `json:"description"`
	IsSuper     bool      `json:"is_super"`
	IsSystem    bool      `json:"is_system"`
	Rank        int       `json:"rank"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ItemType work item. Menentukan extension yang dipakai.
const (
	ItemTask     = "task"
	ItemReminder = "reminder"
	ItemRFS      = "rfs"
	ItemIncident = "incident"
	ItemRequest  = "request"
	ItemChange   = "change"
	// ItemDailyTask adalah checklist harian bergaya todo list (F17).
	// Menumpang work_items agar memperoleh notifikasi/komentar/audit.
	ItemDailyTask = "daily_task"
)

// Channel notifikasi.
const (
	ChannelWhatsApp = "whatsapp"
	ChannelTelegram = "telegram"
	ChannelEmail    = "email"
	ChannelSMS      = "sms"
	ChannelSlack    = "slack"
	ChannelDiscord  = "discord"
	ChannelWebhook  = "webhook"
)

// Severity notifikasi.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// Status outbox.
const (
	OutboxPending    = "pending"
	OutboxSending    = "sending"
	OutboxSent       = "sent"
	OutboxFailed     = "failed"
	OutboxSuppressed = "suppressed"
)

// SLAState adalah nilai kolom work_items.sla_state.
// Harus sinkron dengan CHECK constraint di 00001_init.sql.
// Mesin SLA baru aktif pada F11, tetapi konstanta disediakan sejak awal agar
// tidak ada kode yang menulis nilai di luar daftar ini.
const (
	SLAStateNone     = "none"     // belum dievaluasi / tidak ber-SLA
	SLAStateOnTrack  = "on_track" // masih dalam batas
	SLAStateAtRisk   = "at_risk"  // melewati warning_pct
	SLAStateBreached = "breached" // melewati target
	SLAStatePaused   = "paused"   // clock berhenti (mis. menunggu customer)
)

// InstallStage adalah tahapan instalasi pada rfs_details.
// Harus sinkron dengan CHECK constraint di 00001_init.sql.
const (
	RFSStagePlanned   = "planned"
	RFSStageProvision = "provisioning"
	RFSStageDelivered = "delivered"
	RFSStageActivated = "activated"
	RFSStageCancelled = "cancelled"
	RFSStagePostponed = "postponed"
)

// ReminderCategory adalah kategori pada reminder_details.
// Harus sinkron dengan CHECK constraint di 00001_init.sql.
const (
	ReminderCategoryTrial       = "trial"
	ReminderCategoryRFS         = "rfs"
	ReminderCategoryMaintenance = "maintenance"
	ReminderCategoryGeneric     = "generic"
	ReminderCategoryCustom      = "custom"
)

// SubjectType adalah jenis subjek pada reminder_details.
const (
	SubjectTypeCustomer = "customer"
	SubjectTypeService  = "service"
	SubjectTypeDevice   = "device"
	SubjectTypeNone     = "none"
)

// Impact/urgency pada ticket_details.
const (
	LevelLow    = "low"
	LevelMedium = "medium"
	LevelHigh   = "high"
)

// Organization adalah tenant ringan. Default satu baris "Internal".
type Organization struct {
	ID        uuid.UUID      `json:"id"`
	Name      string         `json:"name"`
	Code      string         `json:"code"`
	Contacts  map[string]any `json:"contacts"`
	IsActive  bool           `json:"is_active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Team adalah grup kerja (NOC, Sales, Field).
type Team struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	// F25: target notifikasi default tim (resolusi target item tanpa target).
	TargetID  *uuid.UUID `json:"target_id,omitempty"`
	IsActive  bool       `json:"is_active"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// User adalah akun panel.
type User struct {
	ID             uuid.UUID  `json:"id"`
	Username       string     `json:"username"`
	Email          string     `json:"email"`
	FullName       string     `json:"full_name"`
	PasswordHash   string     `json:"-"`
	Role           string     `json:"role"`
	TeamID         *uuid.UUID `json:"team_id,omitempty"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
	TelegramChatID string     `json:"telegram_chat_id"`
	WANumber       string     `json:"wa_number"`
	TokenVersion   int        `json:"token_version"`
	IsActive       bool       `json:"is_active"`
	LastLoginAt    *time.Time `json:"last_login_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// RefreshToken adalah sesi panjang yang dapat dicabut.
type RefreshToken struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	TokenHash   string     `json:"-"`
	UserAgent   string     `json:"user_agent"`
	IP          string     `json:"ip"`
	ExpiresAt   time.Time  `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	RotatedFrom *uuid.UUID `json:"rotated_from,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// WorkflowDefinition mendefinisikan state machine per item_type.
type WorkflowDefinition struct {
	ID             uuid.UUID           `json:"id"`
	ItemType       string              `json:"item_type"`
	Name           string              `json:"name"`
	States         []string            `json:"states"`
	Transitions    map[string][]string `json:"transitions"`
	InitialState   string              `json:"initial_state"`
	TerminalStates []string            `json:"terminal_states"`
	IsDefault      bool                `json:"is_default"`
	IsActive       bool                `json:"is_active"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

// NotificationProvider adalah konfigurasi pengiriman per kanal.
type NotificationProvider struct {
	ID            uuid.UUID      `json:"id"`
	Channel       string         `json:"channel"`
	Kind          string         `json:"kind"`
	Label         string         `json:"label"`
	BaseURL       string         `json:"base_url"`
	APIKeyEnc     string         `json:"-"`
	HasAPIKey     bool           `json:"has_api_key"`
	Extra         map[string]any `json:"extra"`
	IsDefault     bool           `json:"is_default"`
	IsActive      bool           `json:"is_active"`
	LastTestAt    *time.Time     `json:"last_test_at,omitempty"`
	LastTestOK    *bool          `json:"last_test_ok,omitempty"`
	LastTestError string         `json:"last_test_error"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// NotificationTarget adalah grup/tim atau personal penerima notifikasi.
type NotificationTarget struct {
	ID             uuid.UUID       `json:"id"`
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	Notes          string          `json:"notes"`
	OrganizationID *uuid.UUID      `json:"organization_id,omitempty"`
	IsActive       bool            `json:"is_active"`
	Bindings       []TargetBinding `json:"bindings,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// TargetBinding adalah satu alamat tujuan pada satu kanal.
type TargetBinding struct {
	ID          uuid.UUID  `json:"id"`
	TargetID    uuid.UUID  `json:"target_id"`
	Channel     string     `json:"channel"`
	Destination string     `json:"destination"`
	ProviderID  *uuid.UUID `json:"provider_id,omitempty"`
	Label       string     `json:"label"`
	IsPrimary   bool       `json:"is_primary"`
	IsActive    bool       `json:"is_active"`
	VerifiedAt  *time.Time `json:"verified_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// EscalationOffset adalah satu titik notifikasi dalam policy eskalasi.
type EscalationOffset struct {
	Label       string `json:"label"`
	HoursBefore *int   `json:"hours_before,omitempty"`
	HoursAfter  *int   `json:"hours_after,omitempty"`
	Severity    string `json:"severity"`
}

// EscalationPolicy mengatur titik notifikasi berjenjang.
type EscalationPolicy struct {
	ID                 uuid.UUID          `json:"id"`
	Name               string             `json:"name"`
	Description        string             `json:"description"`
	Offsets            []EscalationOffset `json:"offsets"`
	AppliesToItemTypes []string           `json:"applies_to_item_types"`
	QuietHoursFrom     *string            `json:"quiet_hours_from,omitempty"`
	QuietHoursTo       *string            `json:"quiet_hours_to,omitempty"`
	MaxAttempts        int                `json:"max_attempts"`
	IsDefault          bool               `json:"is_default"`
	IsActive           bool               `json:"is_active"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
}

// NotificationTemplate adalah template pesan (text/template).
type NotificationTemplate struct {
	ID          uuid.UUID `json:"id"`
	Key         string    `json:"key"`
	ItemType    *string   `json:"item_type,omitempty"`
	Channel     *string   `json:"channel,omitempty"`
	SubjectTpl  string    `json:"subject_tpl"`
	BodyTpl     string    `json:"body_tpl"`
	Severity    string    `json:"severity"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
	UpdatedBy   string    `json:"updated_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WorkItem adalah inti generik untuk task | reminder | rfs | incident | request | change.
type WorkItem struct {
	ID          uuid.UUID `json:"id"`
	RefNo       string    `json:"ref_no"`
	ItemType    string    `json:"item_type"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Priority    string    `json:"priority"`
	Status      string    `json:"status"`
	Stage       string    `json:"stage"`

	OwnerUsername     string     `json:"owner_username"`
	RequesterUsername string     `json:"requester_username"`
	TeamID            *uuid.UUID `json:"team_id,omitempty"`
	OrganizationID    *uuid.UUID `json:"organization_id,omitempty"`
	TargetID          *uuid.UUID `json:"target_id,omitempty"`
	Source            string     `json:"source"`
	ParentID          *uuid.UUID `json:"parent_id,omitempty"`

	StartAt  *time.Time `json:"start_at,omitempty"`
	DueAt    *time.Time `json:"due_at,omitempty"`
	ExpireAt *time.Time `json:"expire_at,omitempty"`

	SLAPolicyID        *uuid.UUID `json:"sla_policy_id,omitempty"`
	SLAState           string     `json:"sla_state"`
	SLABreachedAt      *time.Time `json:"sla_breached_at,omitempty"`
	FirstResponseAt    *time.Time `json:"first_response_at,omitempty"`
	ResolvedAt         *time.Time `json:"resolved_at,omitempty"`
	ClosedAt           *time.Time `json:"closed_at,omitempty"`
	PausedTotalSeconds int64      `json:"paused_total_seconds"`

	DeviceRef   string   `json:"device_ref"`
	ServiceRef  string   `json:"service_ref"`
	CustomerRef string   `json:"customer_ref"`
	Tags        []string `json:"tags"`

	IsDeleted         bool      `json:"is_deleted"`
	CreatedBy         string    `json:"created_by"`
	UpdatedByUsername string    `json:"updated_by_username"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`

	// F27: turunan waktu penyelesaian & aktornya (tidak disimpan di kolom).
	// CompletedAt = resolved_at (fallback closed_at).
	// CompletedBy = actor event status-final (fallback updated_by_username).
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CompletedBy string     `json:"completed_by,omitempty"`

	// Extension (hanya diisi sesuai item_type)
	Task     *TaskDetails     `json:"task,omitempty"`
	Reminder *ReminderDetails `json:"reminder,omitempty"`
	RFS      *RFSDetails      `json:"rfs,omitempty"`
	Ticket   *TicketDetails   `json:"ticket,omitempty"`

	// F20: penangan tambahan (diisi saat detail dimuat).
	Collaborators []Collaborator `json:"collaborators,omitempty"`
	// F20: siklus SLA tiket (diisi saat detail dimuat).
	SLACycles   []SLACycle `json:"sla_cycles,omitempty"`
	ReopenCount int        `json:"reopen_count"`

	// F25: data teknis aktivasi RFS (diisi saat detail dimuat, item_type=rfs).
	Activation *RFSActivationData `json:"activation,omitempty"`

	// SLA turunan (dihitung saat serialisasi, tidak disimpan di kolom khusus).
	// SLAStartAt = first_response_at (status pertama bukan state awal).
	// SLAEndAt   = closed_at (final) atau now (berjalan).
	SLASeconds int64      `json:"sla_seconds"`
	SLARunning bool       `json:"sla_running"`
	SLAStartAt *time.Time `json:"sla_start_at,omitempty"`
	SLAEndAt   *time.Time `json:"sla_end_at,omitempty"`
}

// IsTicket melaporkan apakah item ini tipe tiket (incident/request/change).
func (wi *WorkItem) IsTicket() bool {
	if wi == nil {
		return false
	}
	switch wi.ItemType {
	case ItemIncident, ItemRequest, ItemChange:
		return true
	default:
		return false
	}
}

// ComputeSLA mengisi SLA turunan untuk tiket: durasi dari first_response_at
// sampai closed_at (final) atau sekarang (berjalan). Untuk item non-tiket,
// SLA dibiarkan nol.
func (wi *WorkItem) ComputeSLA(now time.Time) {
	if wi == nil || !wi.IsTicket() {
		return
	}
	start := wi.FirstResponseAt
	if start == nil {
		return
	}
	wi.SLAStartAt = start

	if wi.ClosedAt != nil {
		wi.SLAEndAt = wi.ClosedAt
		wi.SLASeconds = int64(wi.ClosedAt.Sub(*start).Seconds())
		wi.SLARunning = false
	} else {
		wi.SLAEndAt = nil
		wi.SLASeconds = int64(now.Sub(*start).Seconds())
		wi.SLARunning = true
	}
	if wi.SLASeconds < 0 {
		wi.SLASeconds = 0
	}
}

// TaskDetails adalah extension untuk item_type = task.
type TaskDetails struct {
	WorkItemID      uuid.UUID       `json:"work_item_id"`
	Checklist       []ChecklistItem `json:"checklist"`
	ProgressPct     int             `json:"progress_pct"`
	EstimateMinutes *int            `json:"estimate_minutes,omitempty"`
	// CompletionNote diisi operator saat menandai Daily Task "Selesai".
	CompletionNote string `json:"completion_note,omitempty"`
	// F24: DailyTaskType adalah kode jenis daily task (master data
	// kind `daily_task_type`). Kosong untuk item_type='task' biasa.
	DailyTaskType string `json:"daily_task_type,omitempty"`
	// F24: ResultStatus adalah hasil penyelesaian daily task:
	// "" (belum ditandai), "normal", atau "bermasalah" (memicu tiket).
	ResultStatus string `json:"result_status,omitempty"`
}

// F24 — Hasil penyelesaian Daily Task.
const (
	DailyResultNormal      = "normal"
	DailyResultBermasalah  = "bermasalah"
	DailyTicketLinkKindStr = "daily_ticket"
)

// ChecklistItem adalah satu butir checklist pada task.
type ChecklistItem struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// ReminderDetails adalah extension untuk item_type = reminder.
type ReminderDetails struct {
	WorkItemID         uuid.UUID  `json:"work_item_id"`
	Category           string     `json:"category"`
	SubjectName        string     `json:"subject_name"`
	SubjectType        string     `json:"subject_type"`
	RecurrenceRule     *string    `json:"recurrence_rule,omitempty"`
	EscalationPolicyID *uuid.UUID `json:"escalation_policy_id,omitempty"`
	ActivatedAt        *time.Time `json:"activated_at,omitempty"`
	LastOffsetFired    string     `json:"last_offset_fired"`
}

// RFSDetails adalah extension untuk item_type = rfs.
type RFSDetails struct {
	WorkItemID     uuid.UUID `json:"work_item_id"`
	CustomerName   string    `json:"customer_name"`
	ServiceID      string    `json:"service_id"`
	ServicePackage string    `json:"service_package"`
	Bandwidth      string    `json:"bandwidth"`
	PicNOC         string    `json:"pic_noc"`
	PicSales       string    `json:"pic_sales"`
	// F25: PIC berupa tim (menggantikan input bebas pic_noc).
	PicTeamID     *uuid.UUID `json:"pic_team_id,omitempty"`
	SalesUsername string     `json:"sales_username"`
	Site          string     `json:"site"`
	InstallStage  string     `json:"install_stage"`
	// F23: catatan penanganan (modal Troubleshoot pada Aktivasi/EWO).
	IssueFound      string `json:"issue_found"`
	Troubleshooting string `json:"troubleshooting"`
	ActionSolution  string `json:"action_solution"`
}

// RFSActivationData (F25) menyimpan data teknis aktivasi RFS (1:1 work_items).
type RFSActivationData struct {
	WorkItemID    uuid.UUID `json:"work_item_id"`
	IPAddress     string    `json:"ip_address"`
	VLanDetail    string    `json:"vlan_detail"`
	InterfacePort string    `json:"interface_port"`
	BandwidthTest string    `json:"bandwidth_test"`
	PingTest      string    `json:"ping_test"`
	PacketLoss    string    `json:"packet_loss"`
	UpdatedBy     string    `json:"updated_by"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// TicketDetails adalah extension untuk item_type = incident | request | change.
// Dipakai pada fase F10.
type TicketDetails struct {
	WorkItemID      uuid.UUID `json:"work_item_id"`
	Category        string    `json:"category"`
	Subcategory     string    `json:"subcategory"`
	IncidentType    string    `json:"incident_type"`
	Impact          string    `json:"impact"`
	Urgency         string    `json:"urgency"`
	AssignmentGroup string    `json:"assignment_group"`
	EscalationLevel int       `json:"escalation_level"`
	ReopenCount     int       `json:"reopen_count"`
	// Catatan penanganan (F21).
	IssueFound      string `json:"issue_found"`
	Troubleshooting string `json:"troubleshooting"`
	ActionSolution  string `json:"action_solution"`
}

// SLACycle adalah satu siklus SLA tiket (F20).
//
// Siklus 0 = pekerjaan awal sejak tiket dibuat; 1.. = setiap reopen.
// Riwayat siklus tidak pernah dihapus.
type SLACycle struct {
	ID              uuid.UUID  `json:"id"`
	WorkItemID      uuid.UUID  `json:"work_item_id"`
	CycleNo         int        `json:"cycle_no"`
	OpenedAt        time.Time  `json:"opened_at"`
	ReopenedAt      *time.Time `json:"reopened_at,omitempty"`
	ClosedAt        *time.Time `json:"closed_at,omitempty"`
	FirstResponseAt *time.Time `json:"first_response_at,omitempty"`
	HandlerUsername string     `json:"handler_username"`
	ClosedBy        string     `json:"closed_by"`
	CreatedAt       time.Time  `json:"created_at"`
}

// Collaborator adalah NOC yang "ikut menangani" sebuah tiket (F20).
type Collaborator struct {
	ID         uuid.UUID `json:"id"`
	WorkItemID uuid.UUID `json:"work_item_id"`
	Username   string    `json:"username"`
	AddedBy    string    `json:"added_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// WorkItemEvent adalah catatan aktivitas append-only.
type WorkItemEvent struct {
	ID            int64          `json:"id"`
	WorkItemID    uuid.UUID      `json:"work_item_id"`
	EventType     string         `json:"event_type"`
	ActorUsername string         `json:"actor_username"`
	FromValue     string         `json:"from_value"`
	ToValue       string         `json:"to_value"`
	Detail        map[string]any `json:"detail"`
	CreatedAt     time.Time      `json:"created_at"`
}

// Note (F26) adalah catatan mandiri (sticky note) dengan tim pemilik + berbagi.
type Note struct {
	ID                uuid.UUID  `json:"id"`
	Title             string     `json:"title"`
	Body              string     `json:"body"`
	Visibility        string     `json:"visibility"` // internal | eksternal
	OwnerTeamID       *uuid.UUID `json:"owner_team_id,omitempty"`
	OwnerTeamName     string     `json:"owner_team_name,omitempty"`
	Color             string     `json:"color"`
	Pinned            bool       `json:"pinned"`
	CreatedBy         string     `json:"created_by"`
	UpdatedByUsername string     `json:"updated_by_username"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	// SharedTeamIDs adalah tim yang boleh melihat catatan ini (diisi saat dimuat).
	SharedTeamIDs []uuid.UUID `json:"shared_team_ids,omitempty"`
	// SharedTeams menyertakan nama tim (untuk tampilan).
	SharedTeams []NoteSharedTeam `json:"shared_teams,omitempty"`
}

// NoteSharedTeam adalah ringkasan tim yang di-share ke sebuah catatan.
type NoteSharedTeam struct {
	TeamID uuid.UUID `json:"team_id"`
	Name   string    `json:"name"`
}

// Visibilitas catatan (F26).
const (
	NoteVisibilityInternal  = "internal"
	NoteVisibilityEksternal = "eksternal"
)

// Comment adalah komentar/diskusi pada work item.
type Comment struct {
	ID             uuid.UUID `json:"id"`
	WorkItemID     uuid.UUID `json:"work_item_id"`
	AuthorUsername string    `json:"author_username"`
	Body           string    `json:"body"`
	IsInternal     bool      `json:"is_internal"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Attachment adalah lampiran pada work item.
type Attachment struct {
	ID         uuid.UUID `json:"id"`
	WorkItemID uuid.UUID `json:"work_item_id"`
	Filename   string    `json:"filename"`
	StoredPath string    `json:"-"`
	SizeBytes  int64     `json:"size_bytes"`
	Mime       string    `json:"mime"`
	UploadedBy string    `json:"uploaded_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// NotificationOutbox adalah antrian pengiriman notifikasi.
type NotificationOutbox struct {
	ID                int64          `json:"id"`
	WorkItemID        *uuid.UUID     `json:"work_item_id,omitempty"`
	EventKey          string         `json:"event_key"`
	SourceType        string         `json:"source_type"`
	TemplateKey       string         `json:"template_key"`
	OffsetLabel       string         `json:"offset_label"`
	Severity          string         `json:"severity"`
	Channel           string         `json:"channel"`
	Destination       string         `json:"destination"`
	ProviderID        *uuid.UUID     `json:"provider_id,omitempty"`
	Payload           map[string]any `json:"payload"`
	RenderedSubject   string         `json:"rendered_subject"`
	RenderedBody      string         `json:"rendered_body"`
	Status            string         `json:"status"`
	Attempts          int            `json:"attempts"`
	MaxAttempts       int            `json:"max_attempts"`
	NextAttemptAt     time.Time      `json:"next_attempt_at"`
	LastError         string         `json:"last_error"`
	ProviderMessageID string         `json:"provider_message_id"`
	SentAt            *time.Time     `json:"sent_at,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

/* ---------------------------------------------------------------------------
   Google Sheets sync (F18)
   --------------------------------------------------------------------------- */

// Status antrean sinkronisasi spreadsheet.
const (
	SheetSyncPending = "pending"
	SheetSyncSending = "sending"
	SheetSyncSent    = "sent"
	SheetSyncFailed  = "failed"
)

// Operasi sinkronisasi spreadsheet.
const (
	SheetOpAppend = "append" // item baru → tambah baris
	SheetOpUpdate = "update" // item berubah → perbarui baris yang sama (by Ref)
)

// SheetSyncConfig adalah konfigurasi sinkronisasi (satu baris di database).
//
// ServiceAccountEnc tidak pernah dikembalikan utuh ke klien; API hanya
// mengembalikan ServiceAccountSet dan ClientEmail.
type SheetSyncConfig struct {
	ID                uuid.UUID  `json:"id"`
	Enabled           bool       `json:"enabled"`
	SpreadsheetID     string     `json:"spreadsheet_id"`
	SheetName         string     `json:"sheet_name"`
	ServiceAccountEnc string     `json:"-"`
	ServiceAccountSet bool       `json:"service_account_set"`
	ClientEmail       string     `json:"client_email"`
	HeaderWritten     bool       `json:"header_written"`
	LastSyncAt        *time.Time `json:"last_sync_at,omitempty"`
	LastError         string     `json:"last_error"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// SheetSyncQueueRow adalah satu baris antrean sinkronisasi spreadsheet.
type SheetSyncQueueRow struct {
	ID            int64          `json:"id"`
	WorkItemID    uuid.UUID      `json:"work_item_id"`
	EventKey      string         `json:"event_key"`
	RefNo         string         `json:"ref_no"`
	Op            string         `json:"op"`
	Action        string         `json:"action"`
	Payload       map[string]any `json:"payload"`
	Status        string         `json:"status"`
	Attempts      int            `json:"attempts"`
	MaxAttempts   int            `json:"max_attempts"`
	NextAttemptAt time.Time      `json:"next_attempt_at"`
	LastError     string         `json:"last_error"`
	SentAt        *time.Time     `json:"sent_at,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// AuditLog adalah catatan aksi user.
type AuditLog struct {
	ID         int64          `json:"id"`
	Username   string         `json:"username"`
	Action     string         `json:"action"`
	EntityType string         `json:"entity_type"`
	EntityID   string         `json:"entity_id"`
	Detail     map[string]any `json:"detail"`
	IP         string         `json:"ip"`
	Success    bool           `json:"success"`
	CreatedAt  time.Time      `json:"created_at"`
}

// Setting adalah konfigurasi runtime yang dapat diubah dari panel.
type Setting struct {
	Key       string         `json:"key"`
	Value     map[string]any `json:"value"`
	UpdatedBy string         `json:"updated_by"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// MasterData adalah satu entri data referensi (kategori, produk, POP, tag, dst.).
//
// Kind bersifat terbuka sehingga operator dapat membuat kelompok baru langsung
// dari panel tanpa migrasi skema.
type MasterData struct {
	ID          uuid.UUID      `json:"id"`
	Kind        string         `json:"kind"`
	Code        string         `json:"code"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	ParentID    *uuid.UUID     `json:"parent_id,omitempty"`
	SortOrder   int            `json:"sort_order"`
	Meta        map[string]any `json:"meta"`
	IsActive    bool           `json:"is_active"`
	CreatedBy   string         `json:"created_by"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// MasterDataKind adalah registri kelompok master data yang dikenali panel.
type MasterDataKind struct {
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	IsSystem    bool   `json:"is_system"`
	SortOrder   int    `json:"sort_order"`
}

// Kind master data bawaan yang punya perlakuan khusus di panel.
const (
	// KindCustomer menyimpan pelanggan; kode dibuat otomatis CUST-<tahun>-<urut>
	// dan atribut (jenis, PIC, telepon, surel, alamat, kapasitas) ada di meta.
	KindCustomer = "customer"
	// KindDailyTaskType (F24) menyimpan jenis Daily Task; meta `dapat_membuat_tiket`.
	KindDailyTaskType = "daily_task_type"
	// KindTelegramChat (F12) menyimpan allowlist grup bot Telegram; code = chat_id.
	// meta opsional mengatur izin per-command (allow_open, allow_ticket, dst.);
	// bila tidak ada, seluruh command dianggap diizinkan.
	KindTelegramChat = "telegram_chat"
)

// Jenis customer pada meta_json.
const (
	CustomerPersonal  = "personal"
	CustomerCorporate = "corporate"
)

// RolePermission adalah satu sel matriks izin (role × action).
type RolePermission struct {
	Role      string    `json:"role"`
	Action    string    `json:"action"`
	Allowed   bool      `json:"allowed"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BoolPtr mengembalikan pointer ke bool (helper untuk field opsional).
func BoolPtr(b bool) *bool { return &b }
