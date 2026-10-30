-- +goose Up

-- ============================================================================
-- Customer ID (CID) pada master data customer.
--
-- CID adalah identitas pelanggan dari sistem billing (mis. "CID-000123"),
-- berbeda dari `code` yang merupakan nomor pelanggan internal panel
-- (CUST-<tahun>-<urut>). CID disimpan pada meta_json agar tidak mengubah
-- struktur tabel master_data dan tetap fleksibel untuk entri non-customer.
--
-- Keunikan CID dijaga di level basis data: satu CID hanya boleh dipakai satu
-- pelanggan aktif. Indeks parsial dipakai agar entri non-customer dan CID
-- kosong tidak saling bentrok.
-- ============================================================================

CREATE UNIQUE INDEX IF NOT EXISTS ux_master_data_customer_cid
    ON master_data (lower(btrim(meta_json ->> 'cid')))
    WHERE kind = 'customer'
      AND coalesce(btrim(meta_json ->> 'cid'), '') <> '';

-- +goose Down

DROP INDEX IF EXISTS ux_master_data_customer_cid;
