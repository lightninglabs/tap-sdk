package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
)

type apiServer struct {
	cfg         config
	coordinator *coordinator
}

func (s *apiServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch {
	case r.URL.Path == "/api/health" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

	case r.URL.Path == "/api/config" && r.Method == http.MethodGet:
		s.writeConfig(w)

	case r.URL.Path == "/api/sessions" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, s.coordinator.listSessions())

	case r.URL.Path == "/api/sessions" && r.Method == http.MethodPost:
		s.startSession(w, r)

	case strings.HasPrefix(r.URL.Path, "/api/sessions/"):
		s.sessionByID(w, r)

	default:
		writeError(w, http.StatusNotFound, errors.New("not found"))
	}
}

func (s *apiServer) writeConfig(w http.ResponseWriter) {
	target := s.cfg.tapdHost
	if strings.EqualFold(s.cfg.transport, "rest") {
		target = s.cfg.tapdBaseURL
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"network":     string(s.cfg.network),
		"transport":   s.cfg.transport,
		"tapd":        target,
		"auto_mine":   s.cfg.miningEnabled(),
		"mine_blocks": s.cfg.regtestMineBlocks,
	})
}

func (s *apiServer) startSession(w http.ResponseWriter, r *http.Request) {
	var req startSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	session, err := s.coordinator.startSession(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	writeJSON(w, http.StatusAccepted, session)
}

func (s *apiServer) sessionByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, errors.New("not found"))
		return
	}

	id := parts[0]
	if len(parts) == 1 && r.Method == http.MethodGet {
		session, ok := s.coordinator.getSession(id)
		if !ok {
			writeError(w, http.StatusNotFound,
				errors.New("session not found"))
			return
		}

		writeJSON(w, http.StatusOK, session)
		return
	}

	if len(parts) == 2 && parts[1] == "signature" &&
		r.Method == http.MethodPost {

		s.submitSignature(w, r, id)
		return
	}

	writeError(w, http.StatusNotFound, errors.New("not found"))
}

func (s *apiServer) submitSignature(w http.ResponseWriter, r *http.Request,
	id string) {

	var req submitSignatureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	session, err := s.coordinator.submitSignature(
		id, req.SignedVirtualPSBT,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	writeJSON(w, http.StatusOK, session)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set(
			"Access-Control-Allow-Headers",
			"Content-Type, Authorization",
		)
		w.Header().Set(
			"Access-Control-Allow-Methods",
			"GET, POST, OPTIONS",
		)
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
