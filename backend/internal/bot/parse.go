// Package bot mengimplementasikan Telegram command bot (F12, inbound).
//
// Alur: Telegram mengirim update ke webhook publik
//
//	POST /api/hooks/telegram/{secret}
//
// yang diteruskan handler API ke Dispatcher.HandleUpdate. Dispatcher memvalidasi
// idempotensi (update_id), allowlist grup (master data "telegram_chat"), lalu
// menjalankan command. Semua perubahan data memakai layanan repository/workitems
// yang sama dengan panel (workflow, audit, outbox) sehingga bot tidak pernah
// mem-bypass aturan.
package bot

import (
	"strings"
	"time"
)

// ParsedCommand adalah hasil penguraian teks pesan menjadi command + argumen.
type ParsedCommand struct {
	Name string // tanpa garis miring, huruf kecil, mis. "open"
	Raw  string // argumen mentah setelah nama command
	Text string // teks asli yang sudah dibersihkan
}

// parseCommand mengurai teks pesan Telegram menjadi command.
//
// Aturan:
//   - hanya pesan yang diawali "/" dianggap command;
//   - tag bot ("/open@NamaBot") dibuang;
//   - nama command di-lowercase; argumen dipertahankan apa adanya.
func parseCommand(text string) (ParsedCommand, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return ParsedCommand{}, false
	}

	// Pisahkan token pertama (command) dari sisanya (argumen).
	fields := strings.SplitN(text, " ", 2)
	head := fields[0]
	raw := ""
	if len(fields) == 2 {
		raw = strings.TrimSpace(fields[1])
	}

	// Buang tag @botname bila ada.
	if at := strings.IndexByte(head, '@'); at >= 0 {
		head = head[:at]
	}
	name := strings.ToLower(strings.TrimPrefix(head, "/"))
	if name == "" {
		return ParsedCommand{}, false
	}
	return ParsedCommand{Name: name, Raw: raw, Text: text}, true
}

// splitPipe memecah argumen bergaya "a | b | c" menjadi komponen yang sudah
// dipangkas. Selalu mengembalikan minimal satu elemen (mungkin kosong).
func splitPipe(raw string) []string {
	parts := strings.Split(raw, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// parseDay mengurai tanggal "dd/mm/yyyy" (atau "dd-mm-yyyy") menurut zona waktu
// lokasi yang diberikan, mengembalikan awal hari (00:00) pada tanggal tersebut.
func parseDay(loc *time.Location, raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	layouts := []string{"02/01/2006", "2/1/2006", "02-01-2006", "2-1-2006", "2006-01-02"}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, raw, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
