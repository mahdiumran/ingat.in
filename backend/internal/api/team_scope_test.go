package api

import (
	"testing"

	"github.com/google/uuid"

	"ingatin/backend/internal/models"
)

// TestResolveTeamForCreate mencakup aturan F31 saat membuat task/daily_task.
func TestResolveTeamForCreate(t *testing.T) {
	noc := uuid.New()
	sales := uuid.New()

	adminWithNOC := &models.User{Username: "admin", Role: models.RoleAdmin, TeamID: &noc}
	adminNoTeam := &models.User{Username: "admin2", Role: models.RoleAdmin}
	agent := &models.User{Username: "budi", Role: models.RoleNOC, TeamID: &sales}
	agentNoTeam := &models.User{Username: "citra", Role: models.RoleNOC}

	cases := []struct {
		name      string
		requested *uuid.UUID
		user      *models.User
		isSuper   bool
		want      *uuid.UUID
	}{
		// Bug yang dilaporkan: admin/super buat task tanpa memilih tim →
		// sebelumnya item "tanpa tim"; sekarang jatuh ke tim admin (NOC).
		{"super tanpa pilih tim jatuh ke tim sendiri", nil, adminWithNOC, true, &noc},
		{"super pilih tim lain dihormati", &sales, adminWithNOC, true, &sales},
		{"super tanpa tim & tanpa pilihan tetap nil", nil, adminNoTeam, true, nil},

		// Non-admin selalu dipaksa ke timnya sendiri.
		{"non-admin diabaikan inputnya", &noc, agent, false, &sales},
		{"non-admin tanpa tim tetap nil", nil, agentNoTeam, false, nil},

		// Tanpa user: kembalikan apa adanya (perilaku lama).
		{"user nil mengembalikan input", &noc, nil, false, &noc},
	}

	for _, tc := range cases {
		got := resolveTeamForCreate(tc.requested, tc.user, tc.isSuper)
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("%s: got %v, ingin nil", tc.name, *got)
		case tc.want != nil && got == nil:
			t.Errorf("%s: got nil, ingin %v", tc.name, *tc.want)
		case tc.want != nil && got != nil && *got != *tc.want:
			t.Errorf("%s: got %v, ingin %v", tc.name, *got, *tc.want)
		}
	}
}

// TestCanSelfClaimItem mencakup fix Item 6: claim tiket/RFS tanpa owner
// boleh lintas tim; owner lock tetap admin-only.
func TestCanSelfClaimItem(t *testing.T) {
	ticket := func() *models.WorkItem {
		return &models.WorkItem{
			ItemType:      models.ItemIncident,
			OwnerUsername: "",
		}
	}

	cases := []struct {
		name string
		user *models.User
		item *models.WorkItem
		want bool
	}{
		{"NOC ambil tiket tanpa owner", &models.User{Username: "noc1", Role: models.RoleNOC}, ticket(), true},
		{"sales ambil tiket tanpa owner", &models.User{Username: "s1", Role: models.RoleSales}, ticket(), true},
		{"custom role ambil tiket tanpa owner", &models.User{Username: "an", Role: "admin_noc"}, ticket(), true},
		{"viewer tidak boleh", &models.User{Username: "v", Role: models.RoleViewer}, ticket(), false},
		{
			"tiket sudah berowner tidak bisa diklaim ulang",
			&models.User{Username: "noc1", Role: models.RoleNOC},
			&models.WorkItem{ItemType: models.ItemIncident, OwnerUsername: "s1"},
			false,
		},
		{"reminder bukan self-claim", &models.User{Username: "noc1", Role: models.RoleNOC}, &models.WorkItem{ItemType: models.ItemReminder}, false},
		{"RFS tanpa owner boleh", &models.User{Username: "noc1", Role: models.RoleNOC}, &models.WorkItem{ItemType: models.ItemRFS}, true},
	}

	for _, tc := range cases {
		if got := canSelfClaimItem(tc.user, tc.item); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestIsCollaboratorOf memastikan penangan tambahan terdeteksi (fix Item 6).
func TestIsCollaboratorOf(t *testing.T) {
	item := &models.WorkItem{
		ItemType: models.ItemIncident,
		Collaborators: []models.Collaborator{
			{Username: "NOC2"},
		},
	}
	if !isCollaboratorOf(&models.User{Username: "noc2", Role: models.RoleNOC}, item) {
		t.Error("noc2 tercatat sebagai penangan (case-insensitive) tapi tidak terdeteksi")
	}
	if isCollaboratorOf(&models.User{Username: "noc9", Role: models.RoleNOC}, item) {
		t.Error("noc9 bukan penangan tapi terdeteksi")
	}
	if isCollaboratorOf(nil, item) || isCollaboratorOf(&models.User{Username: "x"}, nil) {
		t.Error("argumen nil harus false")
	}

	// Regresi: penangan RFS (Aktivasi/EWO) dulu tidak terdeteksi karena
	// loadExtensions hanya memuat collaborator untuk IsTicket().
	rfs := &models.WorkItem{
		ItemType: models.ItemRFS,
		Collaborators: []models.Collaborator{
			{Username: "field1"},
		},
	}
	if !isCollaboratorOf(&models.User{Username: "FIELD1", Role: models.RoleNOC}, rfs) {
		t.Error("penangan RFS tidak terdeteksi; penangan tidak akan bisa mengubah/menutup RFS")
	}
	if isCollaboratorOf(&models.User{Username: "field2", Role: models.RoleNOC}, rfs) {
		t.Error("field2 bukan penangan RFS tapi terdeteksi")
	}
}

// TestCanJoinHandling: self-join collaborator tiket/RFS boleh walau owner
// sudah terisi (keputusan produk Item 6). Reminder/task tidak termasuk.
func TestCanJoinHandling(t *testing.T) {
	noc := &models.User{Username: "noc1", Role: models.RoleNOC}
	assigned := &models.WorkItem{ItemType: models.ItemRequest, OwnerUsername: "s1"}
	if !canJoinHandling(noc, assigned) {
		t.Error("NOC harus boleh ikut menangani tiket yang sudah berowner")
	}
	if canJoinHandling(&models.User{Username: "v", Role: models.RoleViewer}, assigned) {
		t.Error("viewer tidak boleh ikut menangani")
	}
	if canJoinHandling(noc, &models.WorkItem{ItemType: models.ItemReminder}) {
		t.Error("reminder bukan jalur collaborator")
	}
}
