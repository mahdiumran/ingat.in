package bot

import (
	"context"
	"log"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/providers"
)

// reply mengirim pesan teks ke chat Telegram memakai provider aktif untuk
// kanal telegram.
//
// Catatan format: providers.Telegram memakai parse_mode=HTML dan meng-escape
// isi Body sebelum dikirim, sehingga balasan di sini cukup berupa teks biasa
// (boleh multi-baris + emoji). Jangan menyisipkan tag HTML — akan tampil
// literal. Data dari pengguna otomatis aman karena ikut di-escape provider.
//
// Kesalahan hanya dicatat ke log: balasan bersifat best-effort dan tidak boleh
// menggagalkan operasi utama (data sudah tersimpan sebelum reply dipanggil).
func (d *Dispatcher) reply(ctx context.Context, chatID string, text string) {
	if d.resolver == nil {
		log.Printf("bot: resolver tidak tersedia, balasan tidak dikirim")
		return
	}
	provider, _, err := d.resolver.ProviderForChannel(ctx, models.ChannelTelegram, nil)
	if err != nil {
		log.Printf("bot: provider telegram tidak tersedia: %v", err)
		return
	}
	if _, err := provider.Send(ctx, providers.Message{
		Destination: chatID,
		Body:        text,
		Severity:    models.SeverityInfo,
	}); err != nil {
		log.Printf("bot: gagal mengirim balasan ke %s: %v", chatID, err)
	}
}
