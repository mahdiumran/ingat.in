-- +goose Up

-- ============================================================================
-- Bersihkan penangan yang terdaftar OTOMATIS oleh ekspansi tim.
--
-- Versi sebelumnya mencatat seluruh anggota tim penekan tombol "Ikuti"/"Ikut
-- RFS"/"Kerjakan" sebagai penangan tambahan. Akibatnya satu klik (mis. noc3)
-- membuat noc4..noc12 ikut terdaftar walau tidak pernah ikut menangani.
--
-- Aturan produk yang benar: penangan tambahan HANYA user yang benar-benar
-- menekan ikut/kerjakan (plus owner). Migrasi ini menghapus baris hasil
-- ekspansi tersebut, dengan pengaman:
--   - hanya baris yang cocok dengan audit `item.collaborator_team_add`
--     (username + added_by + work item yang sama), dan
--   - tidak menghapus bila user tersebut BELAKANGAN ikut sendiri secara
--     eksplisit (audit `item.collaborator_add` setelahnya).
-- ============================================================================

DELETE FROM ticket_collaborators tc
USING audit_logs a
WHERE a.action = 'item.collaborator_team_add'
  AND a.entity_id = tc.work_item_id::text
  AND a.username = tc.added_by
  AND tc.username = ANY (
        SELECT jsonb_array_elements_text(a.detail -> 'usernames')
      )
  AND NOT EXISTS (
        SELECT 1
        FROM audit_logs b
        WHERE b.action = 'item.collaborator_add'
          AND b.entity_id = a.entity_id
          AND lower(b.detail ->> 'username') = lower(tc.username)
          AND b.created_at >= a.created_at
      );

-- +goose Down

-- Pemulihan tidak mungkin: baris yang dihapus adalah data yang salah.
-- Down sengaja tidak melakukan apa pun.
SELECT 1;
