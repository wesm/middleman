package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wesm/middleman/internal/db"
)

const activitySafetyCap = 5000

type activityResponse struct {
	Items  []activityItemResponse `json:"items"`
	Capped bool                   `json:"capped"`
}

type activityItemResponse struct {
	ID           string `json:"id"`
	Cursor       string `json:"cursor"`
	ActivityType string `json:"activity_type"`
	RepoOwner    string `json:"repo_owner"`
	RepoName     string `json:"repo_name"`
	ItemType     string `json:"item_type"`
	ItemNumber   int    `json:"item_number"`
	ItemTitle    string `json:"item_title"`
	ItemURL      string `json:"item_url"`
	ItemState    string `json:"item_state"`
	Author       string `json:"author"`
	CreatedAt    string `json:"created_at"`
	BodyPreview  string `json:"body_preview"`
}

func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	opts := db.ListActivityOpts{
		Repo:   q.Get("repo"),
		Search: q.Get("search"),
		Limit:  activitySafetyCap + 1,
	}

	if types := q.Get("types"); types != "" {
		opts.Types = strings.Split(types, ",")
	}

	if since := q.Get("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid since: "+err.Error())
			return
		}
		opts.Since = &t
	} else {
		defaultSince := time.Now().UTC().AddDate(0, 0, -7)
		opts.Since = &defaultSince
	}

	if cursor := q.Get("after"); cursor != "" {
		t, source, sourceID, err := db.DecodeCursor(cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid after cursor: "+err.Error())
			return
		}
		opts.AfterTime = &t
		opts.AfterSource = source
		opts.AfterSourceID = sourceID
	}

	items, err := s.db.ListActivity(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list activity: "+err.Error())
		return
	}

	capped := len(items) > activitySafetyCap
	if capped {
		items = items[:activitySafetyCap]
	}

	out := make([]activityItemResponse, len(items))
	for i, it := range items {
		out[i] = activityItemResponse{
			ID:           it.Source + ":" + strconv.FormatInt(it.SourceID, 10),
			Cursor:       db.EncodeCursor(it.CreatedAt, it.Source, it.SourceID),
			ActivityType: it.ActivityType,
			RepoOwner:    it.RepoOwner,
			RepoName:     it.RepoName,
			ItemType:     it.ItemType,
			ItemNumber:   it.ItemNumber,
			ItemTitle:    it.ItemTitle,
			ItemURL:      it.ItemURL,
			ItemState:    it.ItemState,
			Author:       it.Author,
			CreatedAt:    it.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			BodyPreview:  it.BodyPreview,
		}
	}

	writeJSON(w, http.StatusOK, activityResponse{Items: out, Capped: capped})
}
