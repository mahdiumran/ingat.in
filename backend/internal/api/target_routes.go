package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ingatin/backend/internal/models"
	"ingatin/backend/internal/providers"
	"ingatin/backend/internal/repository"
)

/* ---------------------------------------------------------------------------
   DTO
   --------------------------------------------------------------------------- */

type targetCreateRequest struct {
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	Notes    string  `json:"notes"`
	IsActive *bool   `json:"is_active"`
	OrgID    *string `json:"organization_id"`
}

type targetUpdateRequest struct {
	Name     *string `json:"name"`
	Kind     *string `json:"kind"`
	Notes    *string `json:"notes"`
	IsActive *bool   `json:"is_active"`
}

type bindingCreateRequest struct {
	Channel     string  `json:"channel"`
	Destination string  `json:"destination"`
	ProviderID  *string `json:"provider_id"`
	Label       string  `json:"label"`
	IsPrimary   *bool   `json:"is_primary"`
	IsActive    *bool   `json:"is_active"`
}

type bindingUpdateRequest struct {
	Destination *string `json:"destination"`
	ProviderID  *string `json:"provider_id"`
	Label       *string `json:"label"`
	IsPrimary   *bool   `json:"is_primary"`
	IsActive    *bool   `json:"is_active"`
}

type templateUpdateRequest struct {
	SubjectTpl *string `json:"subject_tpl"`
	BodyTpl    *string `json:"body_tpl"`
	Severity   *string `json:"severity"`
	IsActive   *bool   `json:"is_active"`
}

type policyCreateRequest struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Offsets     []map[string]any `json:"offsets"`
	ItemTypes   []string         `json:"applies_to_item_types"`
	QuietFrom   *string          `json:"quiet_hours_from"`
	QuietTo     *string          `json:"quiet_hours_to"`
	MaxAttempts *int             `json:"max_attempts"`
	IsDefault   *bool            `json:"is_default"`
}

type policyUpdateRequest struct {
	Name        *string          `json:"name"`
	Description *string          `json:"description"`
	Offsets     []map[string]any `json:"offsets"`
	ItemTypes   []string         `json:"applies_to_item_types"`
	QuietFrom   *string          `json:"quiet_hours_from"`
	QuietTo     *string          `json:"quiet_hours_to"`
	MaxAttempts *int             `json:"max_attempts"`
	IsDefault   *bool            `json:"is_default"`
	IsActive    *bool            `json:"is_active"`
}

/* ---------------------------------------------------------------------------
   Targets
   --------------------------------------------------------------------------- */

// handleListTargets mengembalikan target beserta binding.
//
// GET /api/targets
func (s *Server) handleListTargets(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListTargets(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": list, "total": len(list)})
}

// handleCreateTarget membuat target (admin/noc).
//
// POST /api/targets
func (s *Server) handleCreateTarget(w http.ResponseWriter, r *http.Request) {
	var req targetCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "nama target wajib diisi")
		return
	}
	kind := req.Kind
	if kind != "group" && kind != "personal" {
		kind = "group"
	}

	orgID, err := parseOptionalUUID(req.OrgID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "organization_id tidak valid")
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	t, err := s.store.CreateTarget(r.Context(), repository.CreateTargetParams{
		Name:           req.Name,
		Kind:           kind,
		Notes:          req.Notes,
		OrganizationID: orgID,
		IsActive:       isActive,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "target.create", "target", t.ID.String(), map[string]any{"name": t.Name}, true)
	writeJSON(w, http.StatusCreated, t)
}

// handleUpdateTarget memperbarui target (admin/noc).
//
// PATCH /api/targets/{id}
func (s *Server) handleUpdateTarget(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id target tidak valid")
		return
	}
	var req targetUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	t, err := s.store.UpdateTarget(r.Context(), id, repository.UpdateTargetParams{
		Name: req.Name, Kind: req.Kind, Notes: req.Notes, IsActive: req.IsActive,
	})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "target tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "target.update", "target", id.String(), nil, true)
	writeJSON(w, http.StatusOK, t)
}

// handleDeleteTarget menghapus target (admin/noc).
//
// DELETE /api/targets/{id}
func (s *Server) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id target tidak valid")
		return
	}
	if err := s.store.DeleteTarget(r.Context(), id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "target tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "target.delete", "target", id.String(), nil, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

/* ---------------------------------------------------------------------------
   Bindings
   --------------------------------------------------------------------------- */

// handleCreateBinding menambahkan binding kanal pada target (admin/noc).
//
// POST /api/targets/{id}/bindings
func (s *Server) handleCreateBinding(w http.ResponseWriter, r *http.Request) {
	targetID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id target tidak valid")
		return
	}
	var req bindingCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if !isKnownChannel(req.Channel) {
		writeErr(w, http.StatusBadRequest, "channel tidak dikenal")
		return
	}
	dest := strings.TrimSpace(req.Destination)
	if dest == "" {
		writeErr(w, http.StatusBadRequest, "destination wajib diisi")
		return
	}

	providerID, err := parseOptionalUUID(req.ProviderID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "provider_id tidak valid")
		return
	}

	isPrimary := true
	if req.IsPrimary != nil {
		isPrimary = *req.IsPrimary
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	b, err := s.store.CreateBinding(r.Context(), repository.CreateBindingParams{
		TargetID:    targetID,
		Channel:     req.Channel,
		Destination: dest,
		ProviderID:  providerID,
		Label:       req.Label,
		IsPrimary:   isPrimary,
		IsActive:    isActive,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "binding.create", "target", targetID.String(), map[string]any{
		"channel": req.Channel,
	}, true)
	writeJSON(w, http.StatusCreated, b)
}

// handleUpdateBinding memperbarui binding (admin/noc).
//
// PATCH /api/bindings/{id}
func (s *Server) handleUpdateBinding(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id binding tidak valid")
		return
	}
	var req bindingUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	params := repository.UpdateBindingParams{
		Destination: req.Destination,
		Label:       req.Label,
		IsPrimary:   req.IsPrimary,
		IsActive:    req.IsActive,
	}
	if req.ProviderID != nil {
		pid, err := parseOptionalUUID(req.ProviderID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "provider_id tidak valid")
			return
		}
		params.ProviderID = pid
	}

	b, err := s.store.UpdateBinding(r.Context(), id, params)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "binding tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "binding.update", "binding", id.String(), nil, true)
	writeJSON(w, http.StatusOK, b)
}

// handleDeleteBinding menghapus binding (admin/noc).
//
// DELETE /api/bindings/{id}
func (s *Server) handleDeleteBinding(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id binding tidak valid")
		return
	}
	if err := s.store.DeleteBinding(r.Context(), id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "binding tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "binding.delete", "binding", id.String(), nil, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTestBinding mengirim pesan uji ke sebuah binding, lalu menandainya
// terverifikasi bila berhasil.
//
// POST /api/bindings/{id}/test
func (s *Server) handleTestBinding(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id binding tidak valid")
		return
	}

	binding, err := s.store.GetBinding(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "binding tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}

	provider, rec, err := s.notifier.ProviderForChannel(r.Context(), binding.Channel, binding.ProviderID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	now := time.Now().UTC()
	body := "✅ Ingat.in — pesan uji\n\nKanal notifikasi berfungsi.\nChannel: " + binding.Channel +
		"\nWaktu (UTC): " + now.Format(time.RFC3339)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res, sendErr := provider.Send(ctx, providers.Message{
		Destination: binding.Destination,
		Body:        body,
		Severity:    models.SeverityInfo,
		Metadata:    map[string]string{"via": "binding_test"},
	})

	if sendErr != nil {
		s.audit(r, "", "binding.test", "binding", id.String(), map[string]any{
			"channel": binding.Channel, "ok": false, "error": sendErr.Error(),
		}, false)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        false,
			"error":     sendErr.Error(),
			"retryable": res.Retryable,
			"provider":  rec.Label,
		})
		return
	}

	if err := s.store.MarkBindingVerified(r.Context(), id, now); err != nil {
		writeInternalError(w, err)
		return
	}

	s.audit(r, "", "binding.test", "binding", id.String(), map[string]any{
		"channel": binding.Channel, "ok": true, "provider": rec.Label,
	}, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"provider":   rec.Label,
		"message_id": res.ProviderMessageID,
	})
}

/* ---------------------------------------------------------------------------
   Templates
   --------------------------------------------------------------------------- */

// handleListTemplates mengembalikan template notifikasi.
//
// GET /api/templates
func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListTemplates(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": list, "total": len(list)})
}

// handleUpdateTemplate memperbarui template (admin/noc).
//
// PATCH /api/templates/{id}
func (s *Server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id template tidak valid")
		return
	}
	var req templateUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Severity != nil {
		switch *req.Severity {
		case models.SeverityInfo, models.SeverityWarning, models.SeverityCritical:
		default:
			writeErr(w, http.StatusBadRequest, "severity harus info/warning/critical")
			return
		}
	}

	tpl, err := s.store.UpdateTemplate(r.Context(), id, repository.UpdateTemplateParams{
		SubjectTpl: req.SubjectTpl,
		BodyTpl:    req.BodyTpl,
		Severity:   req.Severity,
		IsActive:   req.IsActive,
		UpdatedBy:  currentUsername(r),
	})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "template tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "template.update", "template", id.String(), map[string]any{"key": tpl.Key}, true)
	writeJSON(w, http.StatusOK, tpl)
}

/* ---------------------------------------------------------------------------
   Escalation policies
   --------------------------------------------------------------------------- */

// handleListPolicies mengembalikan policy eskalasi.
//
// GET /api/policies
func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListPolicies(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policies": list, "total": len(list)})
}

// handleCreatePolicy membuat policy eskalasi (admin/noc).
//
// POST /api/policies
func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	var req policyCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "nama policy wajib diisi")
		return
	}
	if len(req.Offsets) == 0 {
		writeErr(w, http.StatusBadRequest, "policy harus memiliki minimal satu offset")
		return
	}
	if err := validateOffsets(req.Offsets); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	maxAttempts := 5
	if req.MaxAttempts != nil {
		maxAttempts = *req.MaxAttempts
	}
	isDefault := false
	if req.IsDefault != nil {
		isDefault = *req.IsDefault
	}

	p, err := s.store.CreatePolicy(r.Context(), repository.CreatePolicyParams{
		Name:        req.Name,
		Description: req.Description,
		Offsets:     req.Offsets,
		ItemTypes:   req.ItemTypes,
		QuietFrom:   req.QuietFrom,
		QuietTo:     req.QuietTo,
		MaxAttempts: maxAttempts,
		IsDefault:   isDefault,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "policy.create", "policy", p.ID.String(), map[string]any{"name": p.Name}, true)
	writeJSON(w, http.StatusCreated, p)
}

// handleUpdatePolicy memperbarui policy (admin/noc).
//
// PATCH /api/policies/{id}
func (s *Server) handleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id policy tidak valid")
		return
	}
	var req policyUpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Offsets != nil {
		if len(req.Offsets) == 0 {
			writeErr(w, http.StatusBadRequest, "policy harus memiliki minimal satu offset")
			return
		}
		if err := validateOffsets(req.Offsets); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	p, err := s.store.UpdatePolicy(r.Context(), id, repository.UpdatePolicyParams{
		Name:        req.Name,
		Description: req.Description,
		Offsets:     req.Offsets,
		ItemTypes:   req.ItemTypes,
		QuietFrom:   req.QuietFrom,
		QuietTo:     req.QuietTo,
		MaxAttempts: req.MaxAttempts,
		IsDefault:   req.IsDefault,
		IsActive:    req.IsActive,
	})
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "policy tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "policy.update", "policy", id.String(), nil, true)
	writeJSON(w, http.StatusOK, p)
}

// handleDeletePolicy menghapus policy (admin/noc).
//
// DELETE /api/policies/{id}
func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id policy tidak valid")
		return
	}
	if err := s.store.DeletePolicy(r.Context(), id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "policy tidak ditemukan")
			return
		}
		writeInternalError(w, err)
		return
	}
	s.audit(r, "", "policy.delete", "policy", id.String(), nil, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

/* ---------------------------------------------------------------------------
   Validasi offset
   --------------------------------------------------------------------------- */

// validateOffsets memastikan setiap offset memiliki label dan tepat satu dari
// hours_before/hours_after, serta severity yang dikenal.
func validateOffsets(offsets []map[string]any) error {
	seen := map[string]bool{}
	for i, o := range offsets {
		label, _ := o["label"].(string)
		label = strings.TrimSpace(label)
		if label == "" {
			return errors.New("setiap offset harus memiliki label")
		}
		if seen[label] {
			return errors.New("label offset tidak boleh duplikat: " + label)
		}
		seen[label] = true

		_, hasBefore := o["hours_before"]
		_, hasAfter := o["hours_after"]
		if hasBefore && hasAfter {
			return errors.New("offset " + label + " hanya boleh memiliki salah satu hours_before/hours_after, bukan keduanya")
		}
		if !hasBefore && !hasAfter {
			return errors.New("offset " + label + " harus memiliki hours_before atau hours_after")
		}

		if sev, ok := o["severity"].(string); ok {
			switch sev {
			case models.SeverityInfo, models.SeverityWarning, models.SeverityCritical:
			default:
				return errors.New("severity offset " + label + " harus info/warning/critical")
			}
		}
		_ = i
	}
	return nil
}

func currentUsername(r *http.Request) string {
	if u := userFrom(r); u != nil {
		return u.Username
	}
	return ""
}
