package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ingatin/backend/internal/crypto"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/notify"
	"ingatin/backend/internal/providers"
	"ingatin/backend/internal/repository"
)

/* ---------------------------------------------------------------------------
   DTO
   --------------------------------------------------------------------------- */

type providerCreateRequest struct {
	Channel   string         `json:"channel"`
	Kind      string         `json:"kind"`
	Label     string         `json:"label"`
	BaseURL   string         `json:"base_url"`
	APIKey    string         `json:"api_key"`
	Extra     map[string]any `json:"extra"`
	IsDefault bool           `json:"is_default"`
	IsActive  *bool          `json:"is_active"`
}

type providerUpdateRequest struct {
	Label     *string        `json:"label"`
	BaseURL   *string        `json:"base_url"`
	APIKey    *string        `json:"api_key"`
	Extra     map[string]any `json:"extra"`
	IsDefault *bool          `json:"is_default"`
	IsActive  *bool          `json:"is_active"`
}

/* ---------------------------------------------------------------------------
   Providers
   --------------------------------------------------------------------------- */

// handleListProviders mengembalikan seluruh provider.
//
// GET /api/providers
func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListProviders(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"providers":        list,
		"total":            len(list),
		"supported_kinds":  providers.SupportedKinds(),
		"kinds_by_channel": kindsByChannel(),
	})
}

func kindsByChannel() map[string][]string {
	channels := []string{
		models.ChannelWhatsApp, models.ChannelTelegram,
	}
	out := map[string][]string{}
	for _, c := range channels {
		out[c] = providers.KindsForChannel(c)
	}
	return out
}

// handleCreateProvider menambahkan provider (admin).
//
// POST /api/providers
func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	var req providerCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.Label = strings.TrimSpace(req.Label)
	req.Kind = strings.TrimSpace(req.Kind)

	if req.Label == "" {
		writeErr(w, http.StatusBadRequest, "label wajib diisi")
		return
	}
	if !providers.IsKnownKind(req.Kind) {
		writeErr(w, http.StatusBadRequest, "jenis provider tidak dikenal")
		return
	}
	channel := strings.TrimSpace(req.Channel)
	if channel == "" {
		channel = channelForKind(req.Kind)
	}
	if !isKnownChannel(channel) {
		writeErr(w, http.StatusBadRequest, "channel tidak dikenal")
		return
	}

	enc, err := crypto.Encrypt(s.cfg.CredentialKey, req.APIKey)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	p, err := s.store.CreateProvider(r.Context(), repository.CreateProviderParams{
		Channel:   channel,
		Kind:      req.Kind,
		Label:     req.Label,
		BaseURL:   strings.TrimSpace(req.BaseURL),
		APIKeyEnc: enc,
		Extra:     req.Extra,
		IsDefault: req.IsDefault,
		IsActive:  isActive,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}

	s.audit(r, "", "provider.create", "provider", p.ID.String(), map[string]any{
		"kind":    p.Kind,
		"channel": p.Channel,
		"label":   p.Label,
	}, true)
	writeJSON(w, http.StatusCreated, p)
}

// handleUpdateProvider memperbarui provider (admin).
//
// PATCH /api/providers/{id}
func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id provider tidak valid")
		return
	}

	var req providerUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	params := repository.UpdateProviderParams{
		Label:     req.Label,
		BaseURL:   req.BaseURL,
		Extra:     req.Extra,
		IsDefault: req.IsDefault,
		IsActive:  req.IsActive,
	}

	// APIKey: nil = jangan ubah; "" = hapus; selain itu = enkripsi baru.
	if req.APIKey != nil {
		enc, err := crypto.Encrypt(s.cfg.CredentialKey, *req.APIKey)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		params.APIKeyEnc = &enc
	}

	p, err := s.store.UpdateProvider(r.Context(), id, params)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "provider tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}

	s.audit(r, "", "provider.update", "provider", id.String(), nil, true)
	writeJSON(w, http.StatusOK, p)
}

// handleDeleteProvider menghapus provider (admin).
//
// DELETE /api/providers/{id}
func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id provider tidak valid")
		return
	}
	if err := s.store.DeleteProvider(r.Context(), id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "provider tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "provider.delete", "provider", id.String(), nil, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTestProvider menguji koneksi provider tanpa mengirim pesan.
//
// POST /api/providers/{id}/test
func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id provider tidak valid")
		return
	}

	rec, enc, err := s.store.ProviderExtraWithSecret(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "provider tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}

	provider, buildErr := s.notifier.BuildFromRecord(rec, enc)
	if buildErr != nil {
		_ = s.store.RecordProviderTest(r.Context(), id, false, buildErr.Error())
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": buildErr.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	testErr := provider.TestConnection(ctx)
	if testErr != nil {
		_ = s.store.RecordProviderTest(r.Context(), id, false, testErr.Error())
		s.audit(r, "", "provider.test", "provider", id.String(), map[string]any{
			"ok": false, "error": testErr.Error(),
		}, false)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": testErr.Error()})
		return
	}

	_ = s.store.RecordProviderTest(r.Context(), id, true, "")
	s.audit(r, "", "provider.test", "provider", id.String(), map[string]any{"ok": true}, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleProviderSessionInfo menampilkan status sesi WAHA dan URL QR.
//
// GET /api/providers/{id}/session
func (s *Server) handleProviderSessionInfo(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id provider tidak valid")
		return
	}

	rec, enc, err := s.store.ProviderExtraWithSecret(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "provider tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	if rec.Kind != providers.KindWAHA {
		writeErr(w, http.StatusBadRequest, "informasi sesi hanya tersedia untuk provider WAHA")
		return
	}

	built, buildErr := s.notifier.BuildFromRecord(rec, enc)
	if buildErr != nil {
		writeErr(w, http.StatusBadRequest, buildErr.Error())
		return
	}
	waha, ok := built.(*providers.WAHA)
	if !ok {
		writeInternalError(w, errors.New("provider bukan WAHA"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	sessions, err := waha.SessionsStatus(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"reachable": false,
			"error":     err.Error(),
			"qr_url":    s.wahaQRURL(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"reachable": true,
		"sessions":  sessions,
		"qr_url":    s.wahaQRURL(),
	})
}

// handleProviderSessionAction menjalankan aksi pada sesi WAHA.
//
// POST /api/providers/{id}/session/{action}  dengan action: start|stop|logout
func (s *Server) handleProviderSessionAction(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id provider tidak valid")
		return
	}
	action := chi.URLParam(r, "action")

	rec, enc, err := s.store.ProviderExtraWithSecret(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "provider tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	if rec.Kind != providers.KindWAHA {
		writeErr(w, http.StatusBadRequest, "aksi sesi hanya untuk provider WAHA")
		return
	}

	built, buildErr := s.notifier.BuildFromRecord(rec, enc)
	if buildErr != nil {
		writeErr(w, http.StatusBadRequest, buildErr.Error())
		return
	}
	waha, ok := built.(*providers.WAHA)
	if !ok {
		writeInternalError(w, errors.New("provider bukan WAHA"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	switch action {
	case "start":
		err = waha.StartSession(ctx)
	case "stop":
		err = waha.StopSession(ctx)
	case "logout":
		err = waha.LogoutSession(ctx)
	default:
		writeErr(w, http.StatusBadRequest, "aksi tidak dikenal (start|stop|logout)")
		return
	}

	if err != nil {
		s.audit(r, "", "provider.session_"+action, "provider", id.String(), map[string]any{
			"ok": false, "error": err.Error(),
		}, false)
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}

	s.audit(r, "", "provider.session_"+action, "provider", id.String(), map[string]any{"ok": true}, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": action})
}

// handleSendTestMessage mengirim pesan uji melalui provider tertentu.
//
// POST /api/providers/{id}/send-test   {destination, message?}
func (s *Server) handleSendTestMessage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id provider tidak valid")
		return
	}

	var req struct {
		Destination string `json:"destination"`
		Message     string `json:"message"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Destination) == "" {
		writeErr(w, http.StatusBadRequest, "destination wajib diisi")
		return
	}

	rec, enc, err := s.store.ProviderExtraWithSecret(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "provider tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}

	provider, buildErr := s.notifier.BuildFromRecord(rec, enc)
	if buildErr != nil {
		writeErr(w, http.StatusBadRequest, buildErr.Error())
		return
	}

	body := strings.TrimSpace(req.Message)
	if body == "" {
		body = "✅ Ingat.in — pesan uji\n\nKanal notifikasi berfungsi.\nWaktu: " +
			time.Now().UTC().Format(time.RFC3339)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res, sendErr := provider.Send(ctx, providers.Message{
		Destination: req.Destination,
		Body:        body,
		Severity:    models.SeverityInfo,
		Metadata:    map[string]string{"via": "panel_test"},
	})

	ok := sendErr == nil
	s.audit(r, "", "provider.send_test", "provider", id.String(), map[string]any{
		"destination": req.Destination,
		"ok":          ok,
		"message_id":  res.ProviderMessageID,
	}, ok)

	if sendErr != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        false,
			"error":     sendErr.Error(),
			"retryable": res.Retryable,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"message_id": res.ProviderMessageID,
	})
}

// wahaQRURL menyusun URL dashboard QR WAHA untuk ditampilkan di panel.
func (s *Server) wahaQRURL() string {
	// nginx site :8010 mem-proxy ke WAHA dan menambahkan basic auth.
	if s.cfg.WAHAQRURL != "" {
		return s.cfg.WAHAQRURL
	}
	return s.cfg.WahaBaseURL
}

/* ---------------------------------------------------------------------------
   Helper
   --------------------------------------------------------------------------- */

// channelForKind menebak kanal default dari jenis provider.
func channelForKind(kind string) string {
	switch kind {
	case providers.KindTelegramBot:
		return models.ChannelTelegram
	case providers.KindWAHA, providers.KindFonnte, providers.KindWablas,
		providers.KindStarsender, providers.KindCustomHTTP:
		return models.ChannelWhatsApp
	case providers.KindSMTP:
		return models.ChannelEmail
	case providers.KindSlackWebhook:
		return models.ChannelSlack
	case providers.KindDiscordWebhook:
		return models.ChannelDiscord
	default:
		return models.ChannelWebhook
	}
}

func isKnownChannel(ch string) bool {
	switch ch {
	case models.ChannelWhatsApp, models.ChannelTelegram, models.ChannelEmail,
		models.ChannelSMS, models.ChannelSlack, models.ChannelDiscord, models.ChannelWebhook:
		return true
	default:
		return false
	}
}

// pastikan notifier dipakai (menghindari import tak terpakai bila berubah).
var _ = notify.ErrNoProvider
