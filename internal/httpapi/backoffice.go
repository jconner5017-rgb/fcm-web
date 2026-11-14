package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/jconner5017-rgb/fcm-web/internal/auth"
	"github.com/jconner5017-rgb/fcm-web/internal/backoffice"
)

// requireStaff allows owners and the staff they granted a role with this
// permission (auth.Account.Can), and only with two-factor sign-in on.
func (s *Server) requireStaff(perm string, next http.HandlerFunc) http.HandlerFunc {
	return requireAccount(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := currentSession(r)
		if !sess.Account.Can(perm) {
			writeError(w, http.StatusForbidden, "forbidden", "You don't have back office access for this.", nil)
			return
		}
		if st, err := s.mfa.Status(r.Context(), sess.Account.ID); err != nil || st == "setup_required" {
			writeError(w, http.StatusForbidden, "mfa_setup_required", "Turn on two-factor sign-in (Account page) to use the back office.", nil)
			return
		}
		if ok, err := s.ipRules.Allowed(r.Context(), clientIP(r, s.trustProxy)); err != nil || !ok {
			writeError(w, http.StatusForbidden, "ip_not_allowed", "The back office can't be used from this network.", nil)
			return
		}
		next(w, r)
	})
}

func (s *Server) audit(r *http.Request, action, accountID, detail string) {
	sess, _ := currentSession(r)
	s.office.Audit(r.Context(), sess.Account, action, accountID, detail)
}

// canTouch: only owners may act on staff accounts; nobody acts on an owner here.
func (s *Server) canTouch(w http.ResponseWriter, r *http.Request, id string) bool {
	sess, _ := currentSession(r)
	t, err := s.auth.ByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Account not found.", nil)
		return false
	}
	if t.IsAdmin || (t.StaffRole != "" && !sess.Account.IsAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "Staff accounts can only be changed by an owner.", nil)
		return false
	}
	return true
}

func (s *Server) officeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case validationError(w, err):
	case errors.Is(err, backoffice.ErrNotFound), errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Account not found (or not in a state that allows this).", nil)
	default:
		s.internalError(w, r, err)
	}
}

func (s *Server) handleOfficeOverview(w http.ResponseWriter, r *http.Request) {
	o, err := s.office.Overview(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) handleOfficeMember(w http.ResponseWriter, r *http.Request) {
	m, err := s.office.Member(r.Context(), r.PathValue("id"))
	if err != nil {
		s.officeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleOfficeSuspend(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	if in.Reason == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid", "Say why (the member doesn't see it; staff do).", map[string]string{"reason": "Required."})
		return
	}
	if !s.canTouch(w, r, id) {
		return
	}
	if err := s.office.Suspend(r.Context(), id, in.Reason); err != nil {
		s.officeError(w, r, err)
		return
	}
	s.audit(r, "suspend", id, in.Reason)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOfficeUnsuspend(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canTouch(w, r, id) {
		return
	}
	if err := s.office.Unsuspend(r.Context(), id); err != nil {
		s.officeError(w, r, err)
		return
	}
	s.audit(r, "unsuspend", id, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOfficeSignOut(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canTouch(w, r, id) {
		return
	}
	n, err := s.office.SignOut(r.Context(), id)
	if err != nil {
		s.officeError(w, r, err)
		return
	}
	s.audit(r, "sign_out", id, "")
	writeJSON(w, http.StatusOK, map[string]int64{"sessions_ended": n})
}

func (s *Server) handleOfficeNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	sess, _ := currentSession(r)
	id := r.PathValue("id")
	if err := s.office.AddNote(r.Context(), id, sess.Account.Email, in.Body); err != nil {
		s.officeError(w, r, err)
		return
	}
	s.audit(r, "note", id, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOfficeAlerts(w http.ResponseWriter, r *http.Request) {
	a, err := s.office.Alerts(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": a})
}

func (s *Server) handleOfficeDismiss(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key string `json:"key"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	sess, _ := currentSession(r)
	if err := s.office.Dismiss(r.Context(), in.Key, sess.Account.Email); err != nil {
		s.officeError(w, r, err)
		return
	}
	s.audit(r, "dismiss_alert", "", in.Key)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOfficeAudit(w http.ResponseWriter, r *http.Request) {
	list, err := s.office.AuditLog(r.Context(), r.URL.Query().Get("account"), 300)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": list})
}

func (s *Server) handleOfficeStaff(w http.ResponseWriter, r *http.Request) {
	list, err := s.office.StaffList(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"staff": list})
}

func (s *Server) handleOfficeSetStaff(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"` // support, manager, or "" to remove
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	id, err := s.office.SetStaff(r.Context(), in.Email, in.Role)
	if errors.Is(err, backoffice.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "No account with that email (they sign up first; owners are set on the server).", nil)
		return
	}
	if err != nil {
		s.officeError(w, r, err)
		return
	}
	s.audit(r, "staff_role", id, map[bool]string{true: "removed", false: in.Role}[in.Role == ""])
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOfficeAccess(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleOfficeBan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	if in.Reason == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid", "Say why (staff only).", map[string]string{"reason": "Required."})
		return
	}
	if !s.canTouch(w, r, id) {
		return
	}
	if err := s.office.Ban(r.Context(), id, in.Reason); err != nil {
		s.officeError(w, r, err)
		return
	}
	s.audit(r, "ban", id, in.Reason)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOfficeUnban(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canTouch(w, r, id) {
		return
	}
	if err := s.office.Unban(r.Context(), id); err != nil {
		s.officeError(w, r, err)
		return
	}
	s.audit(r, "unban", id, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleIPRules(w http.ResponseWriter, r *http.Request) {
	list, err := s.ipRules.List(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": list, "your_ip": clientIP(r, s.trustProxy), "enforced": !s.ipRules.Disabled})
}

func (s *Server) handleIPRuleAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CIDR  string `json:"cidr"`
		Label string `json:"label"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	sess, _ := currentSession(r)
	rule, err := s.ipRules.Add(r.Context(), in.CIDR, in.Label, sess.Account.Email, clientIP(r, s.trustProxy))
	switch {
	case errors.Is(err, backoffice.ErrLockout):
		writeError(w, http.StatusConflict, "lockout", "That would block the network you're on now. Add your current network first.", nil)
	case err != nil && err.Error() == "bad network":
		writeError(w, http.StatusUnprocessableEntity, "invalid", "Enter an address like 203.0.113.7 or a range like 203.0.113.0/24.", map[string]string{"cidr": "Not a network."})
	case err != nil:
		s.internalError(w, r, err)
	default:
		s.audit(r, "ip_rule_added", "", rule.CIDR+" "+rule.Label)
		writeJSON(w, http.StatusCreated, rule)
	}
}

func (s *Server) handleIPRuleRemove(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	err := s.ipRules.Remove(r.Context(), id, clientIP(r, s.trustProxy))
	if errors.Is(err, backoffice.ErrLockout) {
		writeError(w, http.StatusConflict, "lockout", "Removing that would block the network you're on now.", nil)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, "ip_rule_removed", "", strconv.FormatInt(id, 10))
	w.WriteHeader(http.StatusNoContent)
}
