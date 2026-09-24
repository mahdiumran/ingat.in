package api

import (
	"net/http"
	"strings"

	"ingatin/backend/internal/models"
)

/* ---------------------------------------------------------------------------
   F31 — Pembedaan data per tim (task & daily_task).

   Aturan:
     - Admin / super user  : melihat & mengubah semua item.
     - Non-admin bertim     : hanya item dengan team_id = tim pengguna.
     - Non-admin tanpa tim  : hanya item miliknya sendiri (created_by/owner).
     - Item legacy (task/daily_task tanpa team_id) : hanya admin.

   Tipe lain (rfs/incident/request/change/reminder) TIDAK dibatasi oleh aturan
   ini — perilakunya tidak berubah.
   --------------------------------------------------------------------------- */

// isTeamScopedType melaporkan apakah item_type dibatasi per tim (F31).
func isTeamScopedType(itemType string) bool {
	return itemType == models.ItemTask || itemType == models.ItemDailyTask
}

// canViewItemTeam melaporkan apakah user boleh MELIHAT item (F31).
//
// Non-scoped type selalu boleh (true). Untuk task/daily_task berlaku aturan tim.
func (s *Server) canViewItemTeam(r *http.Request, user *models.User, item *models.WorkItem) bool {
	if item == nil {
		return false
	}
	if !isTeamScopedType(item.ItemType) {
		return true
	}
	if user == nil {
		return false
	}
	if s.isSuper(r, user) {
		return true
	}
	// Non-admin bertim: hanya item tim yang sama.
	if user.TeamID != nil && item.TeamID != nil && *user.TeamID == *item.TeamID {
		return true
	}
	// Tanpa tim (atau item legacy tanpa tim): hanya milik sendiri.
	return itemOwnedBy(user, item)
}

// canWriteItemTeam melaporkan apakah user boleh MENGUBAH item (F31).
//
// Non-scoped type: admin, pembuat, atau owner (perilaku lama tidak berubah).
// Scoped type: tambahan — anggota tim yang sama boleh mengubah.
func (s *Server) canWriteItemTeam(r *http.Request, user *models.User, item *models.WorkItem) bool {
	if user == nil || item == nil {
		return false
	}
	if s.isSuper(r, user) {
		return true
	}
	if itemOwnedBy(user, item) {
		return true
	}
	if isTeamScopedType(item.ItemType) && user.TeamID != nil && item.TeamID != nil && *user.TeamID == *item.TeamID {
		return true
	}
	return false
}

// itemOwnedBy melaporkan apakah user adalah pembuat atau owner item.
func itemOwnedBy(user *models.User, item *models.WorkItem) bool {
	if user == nil || item == nil {
		return false
	}
	u := strings.ToLower(strings.TrimSpace(user.Username))
	if u == "" {
		return false
	}
	if strings.ToLower(strings.TrimSpace(item.CreatedBy)) == u {
		return true
	}
	if strings.ToLower(strings.TrimSpace(item.OwnerUsername)) == u {
		return true
	}
	return false
}
