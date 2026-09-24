package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ingatin/backend/internal/models"
)

// ---------------------------------------------------------------------------
// F25 — Data aktivasi RFS + resolusi target notifikasi.
// ---------------------------------------------------------------------------

// GetRFSActivation mengambil data teknis aktivasi sebuah RFS.
// Mengembalikan ErrNotFound bila belum ada (baris dibuat saat pertama disimpan).
func (s *Store) GetRFSActivation(ctx context.Context, workItemID uuid.UUID) (*models.RFSActivationData, error) {
	d := &models.RFSActivationData{WorkItemID: workItemID}
	err := s.pool.QueryRow(ctx, `
		SELECT ip_address, vlan_detail, interface_port,
		       bandwidth_test, ping_test, packet_loss, updated_by, updated_at
		FROM rfs_activation_data WHERE work_item_id=$1`, workItemID,
	).Scan(&d.IPAddress, &d.VLanDetail, &d.InterfacePort,
		&d.BandwidthTest, &d.PingTest, &d.PacketLoss, &d.UpdatedBy, &d.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return d, nil
}

// RFSActivationInput adalah nilai-nilai data aktivasi yang dapat diubah.
type RFSActivationInput struct {
	IPAddress     string
	VLanDetail    string
	InterfacePort string
	BandwidthTest string
	PingTest      string
	PacketLoss    string
}

// UpsertRFSActivation membuat/memperbarui data aktivasi RFS.
func (s *Store) UpsertRFSActivation(ctx context.Context, tx pgx.Tx, workItemID uuid.UUID, in RFSActivationInput, actor string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO rfs_activation_data
			(work_item_id, ip_address, vlan_detail, interface_port,
			 bandwidth_test, ping_test, packet_loss, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8, now())
		ON CONFLICT (work_item_id) DO UPDATE SET
			ip_address     = EXCLUDED.ip_address,
			vlan_detail    = EXCLUDED.vlan_detail,
			interface_port = EXCLUDED.interface_port,
			bandwidth_test = EXCLUDED.bandwidth_test,
			ping_test      = EXCLUDED.ping_test,
			packet_loss    = EXCLUDED.packet_loss,
			updated_by     = EXCLUDED.updated_by,
			updated_at     = now()`,
		workItemID, in.IPAddress, in.VLanDetail, in.InterfacePort,
		in.BandwidthTest, in.PingTest, in.PacketLoss, actor)
	return err
}

// ResolveItemTargetID menentukan target notifikasi efektif sebuah item:
//  1. target_id item bila ada;
//  2. target_id tim (mis. via RFS PIC team / team_id item) bila ada;
//  3. target default.
//
// Mengembalikan nil bila tidak ada satupun yang tersedia.
func (s *Store) ResolveItemTargetID(ctx context.Context, item *models.WorkItem) (*uuid.UUID, error) {
	if item == nil {
		return s.DefaultTargetID(ctx)
	}
	if item.TargetID != nil {
		return item.TargetID, nil
	}

	// Tim item, atau tim PIC (khusus RFS) sebagai cadangan.
	teamID := item.TeamID
	if teamID == nil && item.ItemType == models.ItemRFS {
		var picTeam *uuid.UUID
		err := s.pool.QueryRow(ctx,
			`SELECT pic_team_id FROM rfs_details WHERE work_item_id=$1`, item.ID).Scan(&picTeam)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		teamID = picTeam
	}
	if teamID != nil {
		var targetID *uuid.UUID
		err := s.pool.QueryRow(ctx,
			`SELECT target_id FROM teams WHERE id=$1`, *teamID).Scan(&targetID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if targetID != nil {
			return targetID, nil
		}
	}

	return s.DefaultTargetID(ctx)
}
