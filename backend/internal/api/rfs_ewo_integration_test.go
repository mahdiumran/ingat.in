//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ingatin/backend/internal/config"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
)

func TestRFSSummarySettingsHandlers(t *testing.T) {
	store, ctx := setupTestStore(t)
	s := &Server{cfg: &config.Config{Timezone: "Asia/Jakarta"}, store: store}
	admin := &models.User{Username: "rfs-summary-settings-test", Role: models.RoleAdmin}

	for _, key := range []string{settingRFSSummaryMode, settingRFSSummaryInterval} {
		key := key
		var old json.RawMessage
		err := store.Pool().QueryRow(ctx, `SELECT value FROM settings WHERE key=$1`, key).Scan(&old)
		hadOld := err == nil
		t.Cleanup(func() {
			if hadOld {
				_, _ = store.Pool().Exec(ctx, `
					INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
					ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`, key, old)
			} else {
				_, _ = store.Pool().Exec(ctx, `DELETE FROM settings WHERE key=$1`, key)
			}
		})
	}

	setReq := httptest.NewRequest(http.MethodPost, "/api/settings/rfs-summary",
		strings.NewReader(`{"mode":"interval","interval_minutes":125}`))
	setReq = setReq.WithContext(context.WithValue(setReq.Context(), ctxUserKey, admin))
	setRes := httptest.NewRecorder()
	s.handleSetRFSSummarySettings(setRes, setReq)
	if setRes.Code != http.StatusOK {
		t.Fatalf("set status = %d, want 200; body=%s", setRes.Code, setRes.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/settings/rfs-summary", nil)
	getRes := httptest.NewRecorder()
	s.handleGetRFSSummarySettings(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body=%s", getRes.Code, getRes.Body.String())
	}
	var got struct {
		Mode            string   `json:"mode"`
		IntervalMinutes int      `json:"interval_minutes"`
		Modes           []string `json:"modes"`
	}
	if err := json.Unmarshal(getRes.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Mode != "interval" || got.IntervalMinutes != 125 {
		t.Errorf("GET settings = %+v, want interval/125", got)
	}
	wantModes := []string{"on_change", "interval", "off"}
	if len(got.Modes) != len(wantModes) {
		t.Fatalf("modes = %#v, want %#v", got.Modes, wantModes)
	}
	for i := range wantModes {
		if got.Modes[i] != wantModes[i] {
			t.Errorf("modes[%d] = %q, want %q", i, got.Modes[i], wantModes[i])
		}
	}

	modeCfg, err := store.GetSetting(ctx, settingRFSSummaryMode)
	if err != nil {
		t.Fatalf("load persisted mode: %v", err)
	}
	intervalCfg, err := store.GetSetting(ctx, settingRFSSummaryInterval)
	if err != nil {
		t.Fatalf("load persisted interval: %v", err)
	}
	if modeCfg["value"] != "interval" || intervalCfg["value"] != float64(125) {
		t.Errorf("persisted settings = mode:%#v interval:%#v", modeCfg, intervalCfg)
	}
}

func TestRFSCreateUpdatePersistenceAndValidation(t *testing.T) {
	store, ctx := setupTestStore(t)
	cfg := &config.Config{Timezone: "Asia/Jakarta", OutboxMaxAttempts: 3}
	s := &Server{cfg: cfg, store: store, notifier: notify.NewResolver(cfg, store)}
	user := &models.User{Username: "rfs-persistence-test", Role: models.RoleNOC}

	t.Cleanup(func() {
		_, _ = store.Pool().Exec(ctx, `DELETE FROM work_items WHERE created_by=$1`, user.Username)
	})

	create := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/items", strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), ctxUserKey, user))
		w := httptest.NewRecorder()
		s.handleCreateWorkItem(w, r)
		return w
	}

	t.Run("rejects unknown active-master tag and category", func(t *testing.T) {
		w := create(`{"item_type":"rfs","title":"Bad tag","tags":["definitely_unknown_ewo_tag"],"rfs":{"customer_name":"PT Bad"}}`)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "tag") {
			t.Fatalf("unknown tag: status=%d body=%s", w.Code, w.Body.String())
		}

		w = create(`{"item_type":"rfs","title":"Bad category","rfs":{"customer_name":"PT Bad","category_code":"DEFINITELY_UNKNOWN"}}`)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "kategori EWO") {
			t.Fatalf("unknown category: status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("persists independent fields and canonical category snapshot", func(t *testing.T) {
		w := create(`{
			"item_type":"rfs",
			"title":"Circuit activation title",
			"description":"Initial technical description",
			"owner_username":"noc-owner",
			"expire_at":"2099-01-02T03:04:05Z",
			"tags":["urgent","New Install"],
			"rfs":{"customer_name":"PT Independent Customer","category_code":"activation"}
		}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
		}
		var created models.WorkItem
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode created item: %v", err)
		}
		if created.Title != "Circuit activation title" || created.Description != "Initial technical description" {
			t.Errorf("independent work item fields lost: title=%q description=%q", created.Title, created.Description)
		}
		if created.OwnerUsername != "noc-owner" || created.ExpireAt == nil {
			t.Errorf("owner/schedule not persisted: owner=%q expire=%v", created.OwnerUsername, created.ExpireAt)
		}
		if created.RFS == nil || created.RFS.CustomerName != "PT Independent Customer" ||
			created.RFS.CategoryCode != "ACTIVATION" || created.RFS.CategoryLabel != "Aktivasi" {
			t.Errorf("RFS snapshot not canonical/persisted: %+v", created.RFS)
		}

		patchReq := httptest.NewRequest(http.MethodPatch, "/api/items/"+created.ID.String(), strings.NewReader(`{
			"title":"Updated title",
			"description":"Updated description",
			"owner_username":"noc-owner-2",
			"expire_at":"2099-02-03T04:05:06Z",
			"tags":["urgent","New Install"],
			"rfs":{"customer_name":"PT Updated Customer","category_code":"DOWNGRADE"}
		}`))
		if code := callHandler(t, patchReq, user, created.ID, s.handleUpdateWorkItem); code != http.StatusOK {
			t.Fatalf("update status=%d, want 200", code)
		}

		got, err := store.GetWorkItem(ctx, created.ID)
		if err != nil {
			t.Fatalf("load updated item: %v", err)
		}
		if got.Title != "Updated title" || got.Description != "Updated description" || got.OwnerUsername != "noc-owner-2" {
			t.Errorf("updated work item fields = title:%q description:%q owner:%q", got.Title, got.Description, got.OwnerUsername)
		}
		if got.ExpireAt == nil || got.ExpireAt.UTC().Format("2006-01-02T15:04:05Z") != "2099-02-03T04:05:06Z" {
			t.Errorf("updated expire_at = %v", got.ExpireAt)
		}
		if got.RFS == nil || got.RFS.CustomerName != "PT Updated Customer" || got.RFS.CategoryCode != "DOWNGRADE" || got.RFS.CategoryLabel != "Downgrade" {
			t.Errorf("updated RFS snapshot = %+v", got.RFS)
		}

		badPatch := httptest.NewRequest(http.MethodPatch, "/api/items/"+created.ID.String(), strings.NewReader(`{"tags":["unknown_new_tag"]}`))
		if code := callHandler(t, badPatch, user, created.ID, s.handleUpdateWorkItem); code != http.StatusBadRequest {
			t.Fatalf("unknown update tag status=%d, want 400", code)
		}
	})
}

func TestResolveRFSCategoryAndValidateTags(t *testing.T) {
	store, ctx := setupTestStore(t)
	s := &Server{store: store}

	code, label, err := s.resolveRFSCategory(ctx, "aktivasi", "ignored")
	if err != nil || code != "ACTIVATION" || label != "Aktivasi" {
		t.Fatalf("resolve by label = %q/%q err=%v", code, label, err)
	}
	code, label, err = s.resolveRFSCategory(ctx, "", "")
	if err != nil || code != "" || label != "" {
		t.Fatalf("empty category = %q/%q err=%v", code, label, err)
	}

	if err := s.validateRFSTags(ctx, []string{"Urgent", "new_install"}, nil); err != nil {
		t.Errorf("active code/label tags rejected: %v", err)
	}
	if err := s.validateRFSTags(ctx, []string{"legacy-only"}, []string{"LEGACY-ONLY"}); err != nil {
		t.Errorf("legacy tag rejected: %v", err)
	}
	if err := s.validateRFSTags(ctx, []string{"not-an-active-tag"}, nil); err == nil {
		t.Error("unknown tag accepted")
	}
}
