package repository

import (
	"context"

	"ingatin/backend/internal/models"
)

// MarkBotUpdate memproses idempotensi update Telegram.
//
// Mengembalikan true bila update_id ini BARU (belum pernah diproses) dan sudah
// dicatat; false bila sudah ada (duplikat kiriman ulang webhook). Pemanggil
// harus melewati pemrosesan ketika hasilnya false.
func (s *Store) MarkBotUpdate(ctx context.Context, updateID int64, chatID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO bot_update_log (update_id, chat_id)
		VALUES ($1, $2)
		ON CONFLICT (update_id) DO NOTHING`, updateID, chatID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// LogBotCommandParams adalah parameter pencatatan command bot.
type LogBotCommandParams struct {
	ChatID      int64
	GroupLabel  string
	Username    string
	Command     string
	Args        string
	WorkItemRef string
	Result      string
}

// LogBotCommand mencatat satu command bot untuk audit ringan.
func (s *Store) LogBotCommand(ctx context.Context, p LogBotCommandParams) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO bot_command_log
			(chat_id, group_label, username, command, args, work_item_ref, result)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		p.ChatID, p.GroupLabel, p.Username, p.Command, p.Args, p.WorkItemRef, p.Result)
	return err
}

// GetTelegramChat memetakan chat_id grup ke entri master data allowlist.
//
// Mengembalikan ErrNotFound bila chat_id tidak terdaftar atau entri tidak
// aktif. Kode entri (master_data.code) = chat_id Telegram (bisa negatif untuk
// grup), sehingga perbandingan dilakukan sebagai string.
func (s *Store) GetTelegramChat(ctx context.Context, chatID string) (*models.MasterData, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+masterDataColumns+` FROM master_data
		 WHERE kind=$1 AND code=$2 AND is_active`, models.KindTelegramChat, chatID)
	return scanMasterData(row)
}
