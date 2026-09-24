// Package workitems berisi logika domain untuk work_items: penomoran referensi,
// pencatatan event (append-only), dan validasi state machine.
package workitems

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Prefix item type — dipakai pada nomor referensi.
const (
	PrefixTask      = "TSK"
	PrefixReminder  = "REM"
	PrefixRFS       = "RFS"
	PrefixIncident  = "INC"
	PrefixRequest   = "REQ"
	PrefixChange    = "CHG"
	PrefixDailyTask = "DTK"
	PrefixDefault   = "WRK"
)

// PrefixFor mengembalikan prefiks referensi untuk sebuah item_type.
func PrefixFor(itemType string) string {
	switch itemType {
	case "task":
		return PrefixTask
	case "reminder":
		return PrefixReminder
	case "rfs":
		return PrefixRFS
	case "incident":
		return PrefixIncident
	case "request":
		return PrefixRequest
	case "change":
		return PrefixChange
	case "daily_task":
		return PrefixDailyTask
	default:
		return PrefixDefault
	}
}

// FormatRefNo menyusun nomor referensi dari prefiks, tahun, dan nomor urut.
// Contoh: FormatRefNo("INC", 2026, 42) => "INC-2026-0042".
func FormatRefNo(prefix string, year int, seq int64) string {
	return fmt.Sprintf("%s-%d-%04d", prefix, year, seq)
}

// NextRefNo mengalokasikan nomor referensi berikutnya untuk item_type.
// Aman untuk konkurensi: memakai UPSERT + RETURNING pada tabel ref_counters
// sehingga dua pemanggil bersamaan tidak akan memperoleh nomor sama.
func NextRefNo(ctx context.Context, tx pgx.Tx, itemType string, now time.Time) (string, error) {
	prefix := PrefixFor(itemType)
	year := now.UTC().Year()

	var seq int64
	err := tx.QueryRow(ctx, `
		INSERT INTO ref_counters (prefix, year, last_value)
		VALUES ($1, $2, 1)
		ON CONFLICT (prefix, year)
		DO UPDATE SET last_value = ref_counters.last_value + 1
		RETURNING last_value`, prefix, year).Scan(&seq)
	if err != nil {
		return "", fmt.Errorf("alokasi nomor referensi: %w", err)
	}
	return FormatRefNo(prefix, year, seq), nil
}

// Event type yang dikenal (harus sinkron dengan CHECK constraint di 00001).
const (
	EventCreated         = "created"
	EventUpdated         = "updated"
	EventStatusChanged   = "status_changed"
	EventStageChanged    = "stage_changed"
	EventAssigned        = "assigned"
	EventUnassigned      = "unassigned"
	EventEscalated       = "escalated"
	EventReopened        = "reopened"
	EventCommented       = "commented"
	EventAttachmentAdded = "attachment_added"
	EventDueChanged      = "due_changed"
	EventExpireChanged   = "expire_changed"
	EventSLAWarning      = "sla_warning"
	EventSLABreached     = "sla_breached"
	EventSLAPaused       = "sla_paused"
	EventSLAResumed      = "sla_resumed"
	EventResolved        = "resolved"
	EventClosed          = "closed"
	EventCancelled       = "cancelled"
	EventNotified        = "notified"
	EventNotifyFailed    = "notify_failed"
	// EventUnlocked = admin memaksa membuka tiket yang sudah ditutup (F20).
	EventUnlocked = "unlocked"
)

// validEventTypes adalah whitelist agar nilai tidak menyalahi CHECK constraint.
var validEventTypes = map[string]bool{
	EventCreated: true, EventUpdated: true, EventStatusChanged: true,
	EventStageChanged: true, EventAssigned: true, EventUnassigned: true,
	EventEscalated: true, EventReopened: true, EventCommented: true,
	EventAttachmentAdded: true, EventDueChanged: true, EventExpireChanged: true,
	EventSLAWarning: true, EventSLABreached: true, EventSLAPaused: true,
	EventSLAResumed: true, EventResolved: true, EventClosed: true,
	EventCancelled: true, EventNotified: true, EventNotifyFailed: true,
	EventUnlocked: true,
}

// EventInput adalah data satu event yang akan dicatat.
type EventInput struct {
	WorkItemID string
	EventType  string
	Actor      string
	FromValue  string
	ToValue    string
	Detail     map[string]any
}

// AppendEvent mencatat event ke work_item_events (append-only).
//
// Tabel ini adalah sumber tunggal untuk timeline UI, audit, dan perhitungan SLA.
// Karena itu setiap perubahan status/assignment WAJIB melalui fungsi ini.
func AppendEvent(ctx context.Context, tx pgx.Tx, in EventInput) error {
	if !validEventTypes[in.EventType] {
		return fmt.Errorf("event_type %q tidak dikenal", in.EventType)
	}
	if in.Detail == nil {
		in.Detail = map[string]any{}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO work_item_events
			(work_item_id, event_type, actor_username, from_value, to_value, detail_json)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		in.WorkItemID, in.EventType, in.Actor, in.FromValue, in.ToValue, in.Detail)
	if err != nil {
		return fmt.Errorf("catat event: %w", err)
	}
	return nil
}

// Workflow menggambarkan state machine satu item_type.
type Workflow struct {
	ItemType       string
	Name           string
	States         []string
	Transitions    map[string][]string
	InitialState   string
	TerminalStates []string
}

// TransitionResult menjelaskan hasil evaluasi transisi.
type TransitionResult struct {
	Allowed   bool
	IsClosing bool
	IsReopen  bool
}

// CanTransition memeriksa apakah perpindahan from -> to diizinkan.
func (w *Workflow) CanTransition(from, to string) bool {
	if from == to {
		return true
	}
	targets, ok := w.Transitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}

// EvaluateTransitions mengembalikan hasil evaluasi transisi.
func (w *Workflow) EvaluateTransitions(from, to string) TransitionResult {
	res := TransitionResult{Allowed: w.CanTransition(from, to)}
	if !res.Allowed {
		return res
	}
	for _, s := range w.TerminalStates {
		if s == to {
			res.IsClosing = true
		}
		if s == from && s != to {
			res.IsReopen = true
		}
	}
	return res
}

// IsValidState melaporkan apakah state termasuk daftar state workflow.
func (w *Workflow) IsValidState(state string) bool {
	for _, s := range w.States {
		if s == state {
			return true
		}
	}
	return false
}

// DefaultWorkflows adalah definisi fallback bila tabel workflow_definitions
// belum memuat item_type tertentu. Nilainya harus konsisten dengan seed 00002.
func DefaultWorkflows() map[string]*Workflow {
	return map[string]*Workflow{
		"task": {
			ItemType:     "task",
			Name:         "Alur Task Standar",
			States:       []string{"accepted", "on_progress", "waiting_customer", "expired", "canceled", "closed"},
			InitialState: "accepted",
			Transitions: map[string][]string{
				"accepted":         {"on_progress", "waiting_customer", "expired", "canceled", "closed"},
				"on_progress":      {"waiting_customer", "expired", "canceled", "closed"},
				"waiting_customer": {"on_progress", "expired", "canceled", "closed"},
				"expired":          {"accepted", "canceled", "closed"},
				"canceled":         {"accepted"},
				"closed":           {"accepted"},
			},
			TerminalStates: []string{"closed", "canceled"},
		},
		"reminder": {
			ItemType:     "reminder",
			Name:         "Alur Reminder Standar",
			States:       []string{"scheduled", "active", "expiring", "expired", "cancelled"},
			InitialState: "scheduled",
			Transitions: map[string][]string{
				"scheduled": {"active", "cancelled"},
				"active":    {"expiring", "expired", "cancelled"},
				"expiring":  {"expired", "active", "cancelled"},
				"expired":   {"active"},
				"cancelled": {},
			},
			TerminalStates: []string{"expired", "cancelled"},
		},
		"rfs": {
			ItemType:     "rfs",
			Name:         "Alur RFS Standar",
			States:       []string{"planned", "in_progress", "in_progress_field", "pending_troubleshoot", "activated", "postponed", "cancelled"},
			InitialState: "planned",
			Transitions: map[string][]string{
				"planned":              {"in_progress", "postponed", "cancelled"},
				"in_progress":          {"in_progress_field", "pending_troubleshoot", "activated", "postponed", "cancelled"},
				"in_progress_field":    {"pending_troubleshoot", "activated", "postponed", "cancelled"},
				"pending_troubleshoot": {"in_progress", "in_progress_field", "activated", "postponed", "cancelled"},
				"activated":            {},
				"postponed":            {"planned", "in_progress", "cancelled"},
				"cancelled":            {},
			},
			TerminalStates: []string{"activated", "cancelled"},
		},
		"incident": {
			ItemType:     "incident",
			Name:         "Alur Insiden NOC",
			States:       []string{"new", "assigned", "in_progress", "pending_customer", "resolved", "closed"},
			InitialState: "new",
			Transitions: map[string][]string{
				"new":              {"assigned", "in_progress", "closed"},
				"assigned":         {"in_progress", "pending_customer", "resolved", "closed"},
				"in_progress":      {"pending_customer", "resolved", "closed"},
				"pending_customer": {"in_progress", "resolved", "closed"},
				"resolved":         {"closed", "in_progress"},
				"closed":           {"in_progress"},
			},
			TerminalStates: []string{"closed"},
		},
		"request": {
			ItemType:     "request",
			Name:         "Alur Permintaan Layanan",
			States:       []string{"new", "assigned", "in_progress", "pending_customer", "fulfilled", "closed"},
			InitialState: "new",
			Transitions: map[string][]string{
				"new":              {"assigned", "in_progress", "closed"},
				"assigned":         {"in_progress", "pending_customer", "fulfilled", "closed"},
				"in_progress":      {"pending_customer", "fulfilled", "closed"},
				"pending_customer": {"in_progress", "fulfilled", "closed"},
				"fulfilled":        {"closed"},
				"closed":           {"in_progress"},
			},
			TerminalStates: []string{"closed"},
		},
		"change": {
			ItemType:     "change",
			Name:         "Alur Change Request",
			States:       []string{"draft", "review", "approved", "scheduled", "implementing", "completed", "rolled_back", "cancelled"},
			InitialState: "draft",
			Transitions: map[string][]string{
				"draft":        {"review", "cancelled"},
				"review":       {"approved", "draft", "cancelled"},
				"approved":     {"scheduled", "cancelled"},
				"scheduled":    {"implementing", "cancelled"},
				"implementing": {"completed", "rolled_back"},
				"completed":    {},
				"rolled_back":  {},
				"cancelled":    {},
			},
			TerminalStates: []string{"completed", "rolled_back", "cancelled"},
		},
		"daily_task": {
			ItemType:     "daily_task",
			Name:         "Alur Daily Task",
			States:       []string{"pending", "in_progress", "waiting_customer", "done", "canceled"},
			InitialState: "pending",
			Transitions: map[string][]string{
				"pending":          {"in_progress", "done", "canceled"},
				"in_progress":      {"waiting_customer", "done", "pending", "canceled"},
				"waiting_customer": {"in_progress", "done", "canceled"},
				"done":             {"pending"},
				"canceled":         {"pending"},
			},
			TerminalStates: []string{"done", "canceled"},
		},
	}
}

// WorkflowFor mengembalikan workflow untuk item_type (fallback ke default).
func WorkflowFor(itemType string) *Workflow {
	if wf, ok := DefaultWorkflows()[itemType]; ok {
		return wf
	}
	return nil
}

// NormalizeTags membersihkan daftar tag: trim, buang kosong, buang duplikat.
func NormalizeTags(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range in {
		v := strings.TrimSpace(t)
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	return out
}
