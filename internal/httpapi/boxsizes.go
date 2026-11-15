package httpapi

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/jconner5017-rgb/fcm-web/internal/inventory"
)

// Box sizes (2026-11-15): the sizes for new boxes of each type, and one
// box's size and Close box / Reopen box - the same as FireCardManager.

func (s *Server) handleBoxSizesGet(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	ed, err := s.invEdition(r, sess.Account.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	sizes, err := s.inv.BoxSizes(r.Context(), sess.Account.ID, ed)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"edition": ed, "sizes": sizes})
}

func (s *Server) handleBoxSizesSave(w http.ResponseWriter, r *http.Request) {
	if !s.invWriteAllowed(w, r) {
		return
	}
	var in struct {
		Edition string `json:"edition"`
		inventory.BoxSizes
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	sess, _ := currentSession(r)
	ed, err := s.invEdition(r, sess.Account.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if in.Edition == "sports" || in.Edition == "tcg" {
		ed = in.Edition
	}
	if err := s.inv.SaveBoxSizes(r.Context(), sess.Account.ID, ed, in.BoxSizes); err != nil {
		s.boxError(w, r, err)
		return
	}
	sizes, _ := s.inv.BoxSizes(r.Context(), sess.Account.ID, ed)
	writeJSON(w, http.StatusOK, map[string]any{"edition": ed, "sizes": sizes})
}

func (s *Server) handleBoxGet(w http.ResponseWriter, r *http.Request) {
	sess, _ := currentSession(r)
	info, err := s.inv.BoxInfo(r.Context(), sess.Account.ID, r.PathValue("id"))
	if err != nil {
		s.boxError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleBoxEdit(w http.ResponseWriter, r *http.Request) {
	if !s.invWriteAllowed(w, r) {
		return
	}
	var in struct {
		Capacity *int  `json:"capacity"`
		Closed   *bool `json:"closed"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	sess, _ := currentSession(r)
	info, err := s.inv.SetBox(r.Context(), sess.Account.ID, r.PathValue("id"), in.Capacity, in.Closed)
	if err != nil {
		s.boxError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) boxError(w http.ResponseWriter, r *http.Request, err error) {
	var fe inventory.FieldErrors
	switch {
	case errors.As(err, &fe):
		msgs := make([]string, 0, len(fe))
		for _, m := range fe {
			msgs = append(msgs, m)
		}
		sort.Strings(msgs)
		writeError(w, http.StatusUnprocessableEntity, "invalid", strings.Join(msgs, " "), fe)
	case errors.Is(err, inventory.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "That box isn't in your online inventory.", nil)
	default:
		s.internalError(w, r, err)
	}
}
