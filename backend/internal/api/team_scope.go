package api

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

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

// canJoinItemTeam melaporkan apakah user boleh ikut menangani item (tiket/RFS).
//
// Aturan: admin/super selalu boleh. Untuk tipe tiket (incident/request/change),
// rfs, dan reminder, user boleh bila timnya SAMA DENGAN TIM PEMBUAT item —
// karena work item tiket/rfs/reminder tidak menyimpan team_id, tim efektif
// diturunkan dari tim pembuat (created_by). Bila pembuat tidak ditemukan atau
// tidak punya tim, fallback ke aturan lama (canManageItem).
//
// Catatan (fix Item 0): reminder masuk aturan tim hanya untuk EDIT isi/jadwal;
// akses hapus tidak memakai canJoinItemTeam tetapi canManageItem, sehingga
// hapus reminder tetap terbatas pada pembuat/admin.
func (s *Server) canJoinItemTeam(r *http.Request, user *models.User, item *models.WorkItem) bool {
	if user == nil || item == nil {
		return false
	}
	if s.isSuper(r, user) {
		return true
	}
	if canManageItem(user, item) {
		return true
	}
	switch item.ItemType {
	case models.ItemIncident, models.ItemRequest, models.ItemChange, models.ItemRFS, models.ItemReminder:
		// Fix Item 6: tiket/RFS tanpa owner boleh di-claim siapa pun yang
		// bukan viewer (self-claim lintas tim, mis. NOC mengambil tiket Sales).
		if canSelfClaimItem(user, item) {
			return true
		}
		// Fix Item 6: penangan tambahan (collaborator) tetap berhak setelah
		// ikut, agar dapat memperbarui item yang ditangani.
		if isCollaboratorOf(user, item) {
			return true
		}
		creator, err := s.store.GetUserByUsername(r.Context(), strings.TrimSpace(item.CreatedBy))
		if err == nil && creator != nil && user.TeamID != nil && creator.TeamID != nil && *user.TeamID == *creator.TeamID {
			return true
		}
	}
	return false
}

// resolveTeamForCreate menentukan team_id saat membuat item ber-team (F31).
//
// Aturan:
//   - non-super : SELALU memakai tim pengguna; input dari klien diabaikan.
//   - super     : memakai team_id pilihan klien bila diisi; bila kosong,
//     jatuh ke tim pengguna sendiri agar item tidak "tanpa tim".
//     (Item task/daily_task tanpa team_id hanya terlihat admin.)
//
// Bila super tidak memilih tim DAN pengguna super tidak punya tim, hasilnya nil
// (perilaku lama dipertahankan — admin melihat semua item).
func resolveTeamForCreate(requested *uuid.UUID, user *models.User, isSuper bool) *uuid.UUID {
	if user == nil {
		return requested
	}
	if !isSuper {
		return user.TeamID
	}
	if requested != nil {
		return requested
	}
	return user.TeamID
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

// canModifyItem adalah penjaga tulis terpadu untuk sebuah work item.
//
// Pemilihan penjaga (F31):
//   - task/daily_task (ber-scoping tim) : canWriteItemTeam — anggota tim yang
//     sama boleh mengubah/menyelesaikan, termasuk item milik rekan setim.
//   - tipe lain (tiket/RFS/reminder)    : canJoinItemTeam — perilaku lama tetap
//     utuh: admin/pembuat/owner + anggota tim pembuat untuk tiket/RFS.
//
// Dipakai pada perubahan status (selesai) dan perubahan isi item agar aturan
// tulis task/daily_task konsisten dengan canWriteItemTeam. Penjaga assign/
// penangan tambahan tetap memakai canJoinItemTeam (UI-nya khusus tiket/RFS).
func (s *Server) canModifyItem(r *http.Request, user *models.User, item *models.WorkItem) bool {
	if item == nil {
		return false
	}
	if isTeamScopedType(item.ItemType) {
		return s.canWriteItemTeam(r, user, item)
	}
	return s.canJoinItemTeam(r, user, item)
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

// isCollaboratorOf melaporkan apakah user tercatat sebagai penangan tambahan
// item. Dipakai fix Item 6 agar penangan yang sudah ikut dapat mengubah item.
func isCollaboratorOf(user *models.User, item *models.WorkItem) bool {
	if user == nil || item == nil {
		return false
	}
	u := strings.ToLower(strings.TrimSpace(user.Username))
	if u == "" {
		return false
	}
	for _, c := range item.Collaborators {
		if strings.ToLower(strings.TrimSpace(c.Username)) == u {
			return true
		}
	}
	return false
}

// canSelfClaimItem melaporkan apakah user boleh mengambil item lintas tim
// (fix Item 6).
//
// Aturan: tiket (incident/request/change) dan RFS boleh diambil siapa pun
// yang bukan viewer — termasuk lintas tim, mis. NOC mengambil tiket buatan
// Sales. Item yang sudah punya owner TIDAK dapat diambil-alih (owner lock
// tetap admin-only); jalur "ikut menangani" memakai collaborator.
func canSelfClaimItem(user *models.User, item *models.WorkItem) bool {
	if user == nil || item == nil {
		return false
	}
	if user.Role == models.RoleViewer || user.Role == models.RoleCustomer {
		return false
	}
	if !item.IsTicket() && item.ItemType != models.ItemRFS {
		return false
	}
	return strings.TrimSpace(item.OwnerUsername) == ""
}

// canJoinHandling melaporkan apakah user boleh ikut menangani item tanpa
// menjadi owner: self-join sebagai collaborator.
//
// Fix Item 6: NOC boleh ikut penanganan walau tiket sudah punya owner
// (keputusan produk). Karena itu aturan ini tidak melihat owner sama sekali.
func canJoinHandling(user *models.User, item *models.WorkItem) bool {
	if user == nil || item == nil {
		return false
	}
	if user.Role == models.RoleViewer || user.Role == models.RoleCustomer {
		return false
	}
	return item.IsTicket() || item.ItemType == models.ItemRFS
}
