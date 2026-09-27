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
