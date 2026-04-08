package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/wesm/middleman/internal/config"
	ghclient "github.com/wesm/middleman/internal/github"
)

type settingsResponse struct {
	Repos    []config.Repo   `json:"repos"`
	Activity config.Activity `json:"activity"`
}

type updateSettingsRequest struct {
	Activity config.Activity `json:"activity"`
}

func (s *Server) handleGetSettings(
	w http.ResponseWriter, r *http.Request,
) {
	if s.cfg == nil {
		writeError(w, http.StatusNotFound,
			"settings not available")
		return
	}

	s.cfgMu.Lock()
	repos := make([]config.Repo, len(s.cfg.Repos))
	copy(repos, s.cfg.Repos)
	activity := s.cfg.Activity
	s.cfgMu.Unlock()

	writeJSON(w, http.StatusOK, settingsResponse{
		Repos:    repos,
		Activity: activity,
	})
}

func (s *Server) handleUpdateSettings(
	w http.ResponseWriter, r *http.Request,
) {
	if s.cfgPath == "" {
		writeError(w, http.StatusNotFound,
			"settings not available")
		return
	}

	var body updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	candidate := body.Activity
	if candidate.ViewMode == "" {
		candidate.ViewMode = "threaded"
	}
	if candidate.TimeRange == "" {
		candidate.TimeRange = "7d"
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	prev := s.cfg.Activity
	s.cfg.Activity = candidate
	if err := s.cfg.Validate(); err != nil {
		s.cfg.Activity = prev
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.Save(s.cfgPath); err != nil {
		s.cfg.Activity = prev
		writeError(w, http.StatusInternalServerError,
			"save config: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, settingsResponse{
		Repos:    s.cfg.Repos,
		Activity: s.cfg.Activity,
	})
}

func (s *Server) handleAddRepo(
	w http.ResponseWriter, r *http.Request,
) {
	if s.cfgPath == "" {
		writeError(w, http.StatusNotFound,
			"settings not available")
		return
	}

	var body struct {
		Owner string `json:"owner"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Owner == "" || body.Name == "" {
		writeError(w, http.StatusBadRequest,
			"owner and name are required")
		return
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	for _, rp := range s.cfg.Repos {
		if rp.Owner == body.Owner && rp.Name == body.Name {
			writeError(w, http.StatusBadRequest,
				body.Owner+"/"+body.Name+" is already configured")
			return
		}
	}

	ghClient, clientErr := s.syncer.ClientForHost("github.com")
	if clientErr != nil {
		writeError(w, http.StatusServiceUnavailable,
			"no GitHub client available")
		return
	}
	if _, err := ghClient.GetRepository(
		r.Context(), body.Owner, body.Name,
	); err != nil {
		writeError(w, http.StatusBadGateway,
			"GitHub API error: "+err.Error())
		return
	}

	s.cfg.Repos = append(s.cfg.Repos,
		config.Repo{Owner: body.Owner, Name: body.Name})

	if err := s.cfg.Save(s.cfgPath); err != nil {
		s.cfg.Repos = s.cfg.Repos[:len(s.cfg.Repos)-1]
		writeError(w, http.StatusInternalServerError,
			"save config: "+err.Error())
		return
	}

	refs := make([]ghclient.RepoRef, len(s.cfg.Repos))
	for i, rp := range s.cfg.Repos {
		refs[i] = ghclient.RepoRef{
			Owner:        rp.Owner,
			Name:         rp.Name,
			PlatformHost: rp.PlatformHostOrDefault(),
		}
	}
	s.syncer.SetRepos(refs)
	s.syncer.TriggerRun(context.WithoutCancel(r.Context()))

	writeJSON(w, http.StatusCreated,
		config.Repo{Owner: body.Owner, Name: body.Name})
}

func (s *Server) handleDeleteRepo(
	w http.ResponseWriter, r *http.Request,
) {
	if s.cfgPath == "" {
		writeError(w, http.StatusNotFound,
			"settings not available")
		return
	}

	owner := r.PathValue("owner")
	name := r.PathValue("name")

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()

	idx := -1
	for i, rp := range s.cfg.Repos {
		if rp.Owner == owner && rp.Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		writeError(w, http.StatusNotFound,
			owner+"/"+name+" is not configured")
		return
	}

	removed := s.cfg.Repos[idx]
	s.cfg.Repos = append(
		s.cfg.Repos[:idx], s.cfg.Repos[idx+1:]...,
	)

	if err := s.cfg.Save(s.cfgPath); err != nil {
		s.cfg.Repos = append(
			s.cfg.Repos[:idx],
			append([]config.Repo{removed}, s.cfg.Repos[idx:]...)...,
		)
		writeError(w, http.StatusInternalServerError,
			"save config: "+err.Error())
		return
	}

	refs := make([]ghclient.RepoRef, len(s.cfg.Repos))
	for i, rp := range s.cfg.Repos {
		refs[i] = ghclient.RepoRef{
			Owner:        rp.Owner,
			Name:         rp.Name,
			PlatformHost: rp.PlatformHostOrDefault(),
		}
	}
	s.syncer.SetRepos(refs)

	w.WriteHeader(http.StatusNoContent)
}
