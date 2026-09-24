package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/repository"
	"ingatin/backend/internal/sheets"
	"ingatin/backend/internal/workitems"
)

// dispatchResult adalah hasil satu command: balasan + jejak audit.
type dispatchResult struct {
	reply   string
	result  string // ringkasan hasil untuk bot_command_log
	workRef string // ref_no item terkait (bila ada)
}

// dispatch merutekan command ke implementasinya.
func (d *Dispatcher) dispatch(ctx context.Context, cmd ParsedCommand, group *models.MasterData, msg *UpdateMessage) dispatchResult {
	// meta allow_* disiapkan; default semua diizinkan (tanpa entri = true).
	if !commandAllowed(group, cmd.Name) {
		return dispatchResult{reply: "⛔ Perintah ini dinonaktifkan untuk grup ini.", result: "denied"}
	}

	switch cmd.Name {
	case "start", "help":
		return dispatchResult{reply: d.helpText(), result: "ok"}
	case "id", "chatid":
		return dispatchResult{
			reply:  fmt.Sprintf("🆔 Chat ID grup ini:\n%s\n\nNama: %s", strconv.FormatInt(msg.Chat.ID, 10), dash(group.Label)),
			result: "ok",
		}
	case "open":
		return d.cmdOpen(ctx, cmd, msg, models.ItemDailyTask)
	case "ticket":
		return d.cmdOpen(ctx, cmd, msg, models.ItemIncident)
	case "list":
		return d.cmdList(ctx, group)
	case "solved", "done":
		return d.cmdStatus(ctx, cmd, msg, true)
	case "hold":
		return d.cmdStatus(ctx, cmd, msg, false)
	case "rekap":
		return d.cmdRekap(ctx, cmd)
	default:
		return dispatchResult{
			reply:  "❓ Perintah tidak dikenal. Kirim /help untuk daftar perintah.",
			result: "unknown",
		}
	}
}

// commandAllowed mengevaluasi meta_json allow_<command>. Bila kunci tidak ada,
// dianggap diizinkan (default semua boleh).
func commandAllowed(group *models.MasterData, command string) bool {
	if group == nil || group.Meta == nil {
		return true
	}
	key := "allow_" + command
	v, ok := group.Meta[key]
	if !ok {
		return true
	}
	b, ok := v.(bool)
	if !ok {
		return true
	}
	return b
}

// helpText adalah panduan command.
func (d *Dispatcher) helpText() string {
	return strings.TrimSpace(`
🤖 PANDUAN BOT NOC (Ingat.in)

• /open [judul] [| PIC]        Buka tugas harian (Daily Task)
• /ticket [judul] [| PIC]      Buat tiket insiden ber-SLA
• /list                        Daftar tugas aktif hari ini
• /solved [REF]                Tandai selesai (mis. /solved DTK-2026-0007)
• /hold [REF]                  Tahan menunggu konfirmasi pelanggan
• /rekap [dd/mm/yyyy]          Rekap kendala (default: hari ini)
• /id                          Lihat chat id grup ini
• /help                        Tampilkan panduan ini

Contoh:
/open Link HSP Banten Down | Erpan
/ticket Internet Pelanggan Down | high | high
/solved DTK-2026-0007
/rekap 23/05/2026`)
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// cmdOpen membuat work item baru.
//
// itemType menentukan: daily_task (default /open) atau incident (/ticket).
// Untuk incident, siklus SLA pertama dimulai (sama seperti API panel).
func (d *Dispatcher) cmdOpen(ctx context.Context, cmd ParsedCommand, msg *UpdateMessage, itemType string) dispatchResult {
	raw := strings.TrimSpace(cmd.Raw)
	if raw == "" {
		return dispatchResult{
			reply:  "⚠️ Judul wajib diisi.\nContoh: /open Link HSP Banten Down | Erpan",
			result: "bad_args",
		}
	}

	parts := splitPipe(raw)
	title := parts[0]
	pic := msg.From.DisplayName()
	if len(parts) > 1 && parts[1] != "" {
		pic = parts[1]
	}
	if title == "" {
		return dispatchResult{reply: "⚠️ Judul wajib diisi.", result: "bad_args"}
	}

	wf := workitems.WorkflowFor(itemType)
	if wf == nil {
		return dispatchResult{reply: "⚠️ Tipe item tidak didukung.", result: "error"}
	}

	// Daily task: tenggat default 23:59 WIB pada hari ini.
	var dueAt *time.Time
	if itemType == models.ItemDailyTask {
		dueAt = defaultDailyTaskDue(time.Now().In(d.loc))
	}

	var orgID *uuid.UUID
	if org, err := d.store.GetDefaultOrganization(ctx); err == nil {
		orgID = &org.ID
	}

	actor := pic
	var created *models.WorkItem
	err := d.store.Tx(ctx, func(tx pgx.Tx) error {
		ref, err := workitems.NextRefNo(ctx, tx, itemType, time.Now())
		if err != nil {
			return err
		}
		wi, err := d.store.CreateWorkItem(ctx, tx, repository.CreateWorkItemParams{
			RefNo:          ref,
			ItemType:       itemType,
			Title:          title,
			Priority:       "normal",
			Status:         wf.InitialState,
			OwnerUsername:  pic,
			OrganizationID: orgID,
			Source:         "telegram",
			DueAt:          dueAt,
			CreatedBy:      actor,
		})
		if err != nil {
			return err
		}
		created = wi

		if err := d.createExtension(ctx, tx, wi.ID, itemType); err != nil {
			return err
		}
		if wi.IsTicket() {
			if err := d.store.CreateInitialSLACycle(ctx, tx, wi.ID, wi.CreatedAt); err != nil {
				return err
			}
		}
		return workitems.AppendEvent(ctx, tx, workitems.EventInput{
			WorkItemID: wi.ID.String(),
			EventType:  workitems.EventCreated,
			Actor:      actor,
			ToValue:    wf.InitialState,
			Detail:     map[string]any{"item_type": itemType, "source": "telegram"},
		})
	})
	if err != nil {
		log.Printf("bot: gagal membuat %s dari telegram: %v", itemType, err)
		return dispatchResult{reply: "⚠️ Gagal menyimpan item. Coba lagi.", result: "error"}
	}

	full, err := d.store.GetWorkItem(ctx, created.ID)
	if err != nil {
		full = created
	}

	// Notifikasi pembuatan (best-effort) + sinkronisasi spreadsheet.
	d.enqueueCreated(ctx, full)
	d.syncSheet(ctx, full, "create")

	label := "Tugas Harian"
	if itemType == models.ItemIncident {
		label = "Tiket Insiden"
	}
	reply := fmt.Sprintf("📢 %s Baru Dicatat\n"+
		"• Ref    : %s\n"+
		"• Judul  : %s\n"+
		"• PIC    : %s\n"+
		"• Status : %s",
		label, full.RefNo, full.Title, dash(pic), full.Status)

	return dispatchResult{reply: reply, result: "created", workRef: full.RefNo}
}

// createExtension membuat baris extension minimal sesuai tipe.
// daily_task/task → task_details; incident → ticket_details (impact/urgency).
func (d *Dispatcher) createExtension(ctx context.Context, tx pgx.Tx, id uuid.UUID, itemType string) error {
	switch itemType {
	case models.ItemTask, models.ItemDailyTask:
		return d.store.CreateTaskDetails(ctx, tx, id, []models.ChecklistItem{}, nil, "")
	case models.ItemIncident:
		return d.store.CreateTicketDetails(ctx, tx, id, repository.CreateTicketDetailsParams{
			Impact:  models.LevelMedium,
			Urgency: models.LevelMedium,
		})
	default:
		return nil
	}
}

// enqueueCreated mengantrikan notifikasi pembuatan (best-effort).
func (d *Dispatcher) enqueueCreated(ctx context.Context, item *models.WorkItem) {
	if item == nil || d.resolver == nil {
		return
	}
	targetID, err := d.store.ResolveItemTargetID(ctx, item)
	if err != nil || targetID == nil {
		return
	}
	tplKey, severity := createdTemplateFor(item.ItemType)
	if _, err := d.resolver.Outbox().Enqueue(ctx, notify.EnqueueParams{
		EventKey:    "created:" + item.ID.String(),
		WorkItemID:  &item.ID,
		SourceType:  "work_item",
		TemplateKey: tplKey,
		Severity:    severity,
		TargetID:    *targetID,
		Payload: notify.Payload{
			RefNo:       item.RefNo,
			Title:       item.Title,
			ItemType:    item.ItemType,
			Priority:    item.Priority,
			Status:      item.Status,
			Owner:       item.OwnerUsername,
			CreatedBy:   item.CreatedBy,
			DueAt:       notify.FormatWIB(item.DueAt),
			CreatedAt:   notify.FormatWIB(&item.CreatedAt),
			Description: item.Description,
			SubjectName: item.Title,
			Category:    item.ItemType,
		},
	}); err != nil {
		log.Printf("bot: gagal mengantrikan notifikasi pembuatan %s: %v", item.RefNo, err)
	}
}

// createdTemplateFor memilih template notifikasi pembuatan per tipe.
func createdTemplateFor(itemType string) (string, string) {
	switch itemType {
	case models.ItemDailyTask:
		return notify.TemplateDailyTaskCreated, models.SeverityInfo
	default:
		return notify.TemplateTodoCreated, models.SeverityInfo
	}
}

// syncSheet mengantrekan sinkronisasi spreadsheet untuk task/daily_task.
func (d *Dispatcher) syncSheet(ctx context.Context, item *models.WorkItem, action string) {
	if d.sheets == nil || item == nil {
		return
	}
	if item.ItemType != models.ItemTask && item.ItemType != models.ItemDailyTask {
		return
	}
	op := models.SheetOpAppend
	if action != "create" {
		op = models.SheetOpUpdate
	}
	payload := map[string]any{
		"item_type":      item.ItemType,
		"ref_no":         item.RefNo,
		"title":          item.Title,
		"description":    item.Description,
		"priority":       item.Priority,
		"status":         item.Status,
		"owner":          item.OwnerUsername,
		"created_by":     item.CreatedBy,
		"updated_by":     item.UpdatedByUsername,
		"due_at_wib":     notify.FormatWIB(item.DueAt),
		"created_at_wib": notify.FormatWIB(&item.CreatedAt),
	}
	if _, err := d.sheets.Enqueue(ctx, sheets.EnqueueParams{
		WorkItemID: item.ID,
		RefNo:      item.RefNo,
		Op:         op,
		Action:     action,
		Payload:    payload,
	}); err != nil {
		log.Printf("bot: gagal mengantrikan sinkronisasi sheet %s: %v", item.RefNo, err)
	}
}

// nonTerminalStates mengembalikan status yang belum final untuk sebuah tipe.
func nonTerminalStates(itemType string) []string {
	wf := workitems.WorkflowFor(itemType)
	if wf == nil {
		return nil
	}
	out := []string{}
	for _, s := range wf.States {
		terminal := false
		for _, t := range wf.TerminalStates {
			if t == s {
				terminal = true
				break
			}
		}
		if !terminal {
			out = append(out, s)
		}
	}
	return out
}

// cmdList menampilkan tugas aktif (non-terminal) hari ini.
func (d *Dispatcher) cmdList(ctx context.Context, group *models.MasterData) dispatchResult {
	loc := d.loc
	now := time.Now().In(loc)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24 * time.Hour)

	// Ambil daily task hari ini (carry-over) + tiket insiden yang belum selesai.
	type row struct {
		ref, title, status, owner string
	}
	rows := []row{}

	dt, _, err := d.store.ListWorkItems(ctx, repository.ListWorkItemsParams{
		ItemType:          models.ItemDailyTask,
		DayFrom:           &dayStart,
		DayTo:             &dayEnd,
		CarryOver:         true,
		NonTerminalStates: nonTerminalStates(models.ItemDailyTask),
		OrderBy:           "created_at",
		Limit:             100,
	})
	if err != nil {
		log.Printf("bot: list daily task: %v", err)
	}
	for _, it := range dt {
		rows = append(rows, row{it.RefNo, it.Title, it.Status, it.OwnerUsername})
	}

	inc, _, err := d.store.ListWorkItems(ctx, repository.ListWorkItemsParams{
		ItemType: models.ItemIncident,
		Search:   "",
		Limit:    100,
	})
	if err != nil {
		log.Printf("bot: list incident: %v", err)
	}
	for _, it := range inc {
		if isTerminalState(models.ItemIncident, it.Status) {
			continue
		}
		rows = append(rows, row{it.RefNo, it.Title, it.Status, it.OwnerUsername})
	}

	if len(rows) == 0 {
		return dispatchResult{reply: "🎉 Tidak ada tugas aktif saat ini.", result: "ok"}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📋 Daftar Tugas Aktif (%d):\n\n", len(rows))
	for i, r := range rows {
		icon := "🔴"
		switch r.status {
		case "done", "resolved", "closed":
			icon = "🟢"
		case "waiting_customer", "pending_customer":
			icon = "⏳"
		}
		fmt.Fprintf(&b, "%d. %s [%s] %s\n   • %s — PIC: %s\n",
			i+1, icon, r.status, r.ref, r.title, dash(r.owner))
	}
	b.WriteString("\nTutup: /solved [REF]  •  Tahan: /hold [REF]")
	return dispatchResult{reply: strings.TrimSpace(b.String()), result: "ok"}
}

// cmdStatus memindahkan status item ke state selesai (solved) atau hold.
//
// solved: daily_task→done, task→closed, incident→resolved (fallback closed),
// request→fulfilled, change→completed.
// hold:   daily_task→waiting_customer, incident/request→pending_customer,
//
//	task→waiting_customer.
func (d *Dispatcher) cmdStatus(ctx context.Context, cmd ParsedCommand, msg *UpdateMessage, solved bool) dispatchResult {
	ref := strings.TrimSpace(cmd.Raw)
	if ref == "" {
		return dispatchResult{
			reply:  "⚠️ Sertakan nomor referensi.\nContoh: /" + cmd.Name + " DTK-2026-0007",
			result: "bad_args",
		}
	}
	ref = strings.ToUpper(ref)

	item, err := d.store.GetWorkItemByRef(ctx, ref)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return dispatchResult{reply: "⚠️ Tidak ada item dengan REF " + ref + ".", result: "not_found"}
		}
		log.Printf("bot: ambil item %s: %v", ref, err)
		return dispatchResult{reply: "⚠️ Gagal membaca item.", result: "error"}
	}

	wf := workitems.WorkflowFor(item.ItemType)
	if wf == nil {
		return dispatchResult{reply: "⚠️ Workflow tipe item tidak ditemukan.", result: "error"}
	}

	target := targetState(item.ItemType, solved)
	if target == "" {
		return dispatchResult{reply: "⚠️ Item tipe ini tidak mendukung perintah tersebut.", result: "unsupported"}
	}

	res := wf.EvaluateTransitions(item.Status, target)
	if !res.Allowed {
		return dispatchResult{
			reply: fmt.Sprintf("⚠️ Tidak bisa memindahkan %s dari %s ke %s.",
				item.RefNo, item.Status, target),
			result: "invalid_transition",
		}
	}

	actor := msg.From.DisplayName()
	now := time.Now().UTC()

	fields := map[string]any{"status": target, "updated_by_username": actor}
	if res.IsClosing && item.ClosedAt == nil {
		fields["closed_at"] = now
		if item.ResolvedAt == nil {
			fields["resolved_at"] = now
		}
	}
	if item.FirstResponseAt == nil && target != wf.InitialState {
		fields["first_response_at"] = now
	}

	eventType := workitems.EventStatusChanged
	switch {
	case res.IsClosing && target == "closed":
		eventType = workitems.EventClosed
	case target == "resolved" || target == "fulfilled":
		eventType = workitems.EventResolved
	}

	err = d.store.Tx(ctx, func(tx pgx.Tx) error {
		if err := d.store.UpdateWorkItemFields(ctx, tx, item.ID, fields); err != nil {
			return err
		}
		return workitems.AppendEvent(ctx, tx, workitems.EventInput{
			WorkItemID: item.ID.String(),
			EventType:  eventType,
			Actor:      actor,
			FromValue:  item.Status,
			ToValue:    target,
			Detail:     map[string]any{"source": "telegram"},
		})
	})
	if err != nil {
		log.Printf("bot: gagal ubah status %s: %v", item.RefNo, err)
		return dispatchResult{reply: "⚠️ Gagal menyimpan perubahan status.", result: "error"}
	}

	updated, err := d.store.GetWorkItem(ctx, item.ID)
	if err != nil {
		updated = item
	}
	d.syncSheet(ctx, updated, "status_changed")

	verb := "DITUTUP"
	if !solved {
		verb = "DIHOLD"
	}
	reply := fmt.Sprintf("✅ %s %s\n• %s\n• Status: %s → %s\n• Oleh: %s",
		updated.RefNo, verb, updated.Title, item.Status, target, dash(actor))
	return dispatchResult{reply: reply, result: "status:" + target, workRef: updated.RefNo}
}

// targetState memetakan command ke status tujuan sesuai tipe item.
func targetState(itemType string, solved bool) string {
	if solved {
		switch itemType {
		case models.ItemDailyTask:
			return "done"
		case models.ItemTask:
			return "closed"
		case models.ItemIncident:
			return "resolved"
		case models.ItemRequest:
			return "fulfilled"
		case models.ItemChange:
			return "completed"
		default:
			return ""
		}
	}
	switch itemType {
	case models.ItemDailyTask, models.ItemTask:
		return "waiting_customer"
	case models.ItemIncident, models.ItemRequest:
		return "pending_customer"
	default:
		return ""
	}
}

// isTerminalState melaporkan apakah state termasuk terminal pada workflow tipe.
func isTerminalState(itemType, state string) bool {
	wf := workitems.WorkflowFor(itemType)
	if wf == nil {
		return false
	}
	for _, s := range wf.TerminalStates {
		if s == state {
			return true
		}
	}
	return false
}

// cmdRekap menampilkan rekap item untuk satu tanggal (default hari ini, WIB).
func (d *Dispatcher) cmdRekap(ctx context.Context, cmd ParsedCommand) dispatchResult {
	arg := strings.TrimSpace(cmd.Raw)
	var day time.Time
	if arg == "" {
		n := time.Now().In(d.loc)
		day = time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, d.loc)
	} else {
		t, ok := parseDay(d.loc, arg)
		if !ok {
			return dispatchResult{reply: "⚠️ Format tanggal salah. Gunakan dd/mm/yyyy.", result: "bad_args"}
		}
		day = t
	}
	dayEnd := day.Add(24 * time.Hour)
	label := day.Format("02/01/2006")

	items, _, err := d.store.ListWorkItems(ctx, repository.ListWorkItemsParams{
		DayFrom: &day,
		DayTo:   &dayEnd,
		OrderBy: "created_at",
		Limit:   200,
	})
	if err != nil {
		log.Printf("bot: rekap: %v", err)
		return dispatchResult{reply: "⚠️ Gagal menyusun rekap.", result: "error"}
	}

	if len(items) == 0 {
		return dispatchResult{
			reply:  fmt.Sprintf("ℹ️ Rekap %s\nTidak ada log pada tanggal tersebut.", label),
			result: "ok",
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📊 REKAP HARIAN — %s\n", label)
	b.WriteString("━━━━━━━━━━━━━━━━\n")
	var solved, progress, hold int
	for _, it := range items {
		switch {
		case isTerminalState(it.ItemType, it.Status):
			solved++
		case it.Status == "waiting_customer" || it.Status == "pending_customer" || it.Status == "hold":
			hold++
		default:
			progress++
		}
	}
	fmt.Fprintf(&b, "• Total        : %d\n• Selesai      : %d 🟢\n• Berjalan     : %d 🔴\n• Ditahan      : %d ⏳\n",
		len(items), solved, progress, hold)
	b.WriteString("━━━━━━━━━━━━━━━━\n\n")
	for i, it := range items {
		badge := "🔴"
		if isTerminalState(it.ItemType, it.Status) {
			badge = "🟢"
		} else if it.Status == "waiting_customer" || it.Status == "pending_customer" {
			badge = "⏳"
		}
		fmt.Fprintf(&b, "%d. %s %s\n   • %s — %s (PIC: %s)\n",
			i+1, badge, it.RefNo, it.Title, it.Status, dash(it.OwnerUsername))
	}
	return dispatchResult{reply: strings.TrimSpace(b.String()), result: "ok"}
}

// defaultDailyTaskDue mengembalikan tenggat default Daily Task 23:59 pada
// tanggal `base` (sudah dalam zona waktu lokal aplikasi).
func defaultDailyTaskDue(base time.Time) *time.Time {
	d := time.Date(base.Year(), base.Month(), base.Day(), 23, 59, 0, 0, base.Location())
	return &d
}
