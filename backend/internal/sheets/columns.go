package sheets

import "time"

// HeaderRow adalah baris judul kolom yang ditulis sekali ke spreadsheet.
//
// Urutan kolom WAJIB konsisten dengan RowFromPayload:
//
//	A Ref | B Tipe | C Judul | D Deskripsi | E Prioritas | F Status | G Owner |
//	H Dibuat Oleh | I Diperbarui Oleh | J Device | K Tags | L Due (WIB) |
//	M Dibuat (WIB) | N Diperbarui (WIB) | O Selesai (WIB) | P Diselesaikan Oleh |
//	Q Keterangan
var HeaderRow = []any{
	"Ref", "Tipe", "Judul", "Deskripsi", "Prioritas", "Status", "Owner",
	"Dibuat Oleh", "Diperbarui Oleh", "Device", "Tags", "Due (WIB)",
	"Dibuat (WIB)", "Diperbarui (WIB)", "Selesai (WIB)", "Diselesaikan Oleh",
	"Keterangan",
}

// LastColumn adalah huruf kolom terakhir (Q) untuk penentuan range.
const LastColumn = "Q"

// RefColumnIndex adalah indeks kolom Ref di dalam HeaderRow (0-based).
const RefColumnIndex = 0

// wib adalah zona waktu tampilan (Asia/Jakarta, tanpa DST).
var wib = time.FixedZone("WIB", 7*3600)

// formatWIB memformat waktu ke WIB; string kosong bila nil.
func formatWIB(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(wib).Format("02 Jan 2006 15:04")
}

// statusLabel menerjemahkan status internal menjadi label yang enak dibaca.
func statusLabel(status string) string {
	switch status {
	case "accepted":
		return "Accepted"
	case "on_progress":
		return "On Progress"
	case "expired":
		return "Expired"
	case "canceled":
		return "Canceled"
	case "closed":
		return "Closed"
	// daily task
	case "pending":
		return "Belum Selesai"
	case "done":
		return "Selesai"
	case "":
		return ""
	default:
		return status
	}
}

// itemTypeLabel menerjemahkan item_type menjadi label yang enak dibaca.
func itemTypeLabel(itemType string) string {
	switch itemType {
	case "task":
		return "Todo"
	case "daily_task":
		return "Daily Task"
	case "":
		return ""
	default:
		return itemType
	}
}

// RowFromPayload mengubah payload JSONB menjadi nilai kolom spreadsheet.
//
// Nilai yang hilang menjadi string kosong agar jumlah kolom selalu tepat.
func RowFromPayload(p map[string]any) []any {
	status := statusLabel(str(p["status"]))
	// Item yang dihapus tetap tercatat sebagai riwayat, ditandai jelas.
	if b, ok := p["deleted"].(bool); ok && b {
		status = "Dihapus"
	}
	return []any{
		str(p["ref_no"]),
		itemTypeLabel(str(p["item_type"])),
		str(p["title"]),
		str(p["description"]),
		str(p["priority"]),
		status,
		str(p["owner"]),
		str(p["created_by"]),
		str(p["updated_by"]),
		str(p["device_ref"]),
		str(p["tags"]),
		str(p["due_at_wib"]),
		str(p["created_at_wib"]),
		time.Now().In(wib).Format("02 Jan 2006 15:04"),
		str(p["completed_at_wib"]),
		str(p["completed_by"]),
		str(p["completion_note"]),
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}
