package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/wesm/middleman/internal/db"
)

type activityResponse struct {
	Items   []activityItemResponse `json:"items"`
	HasMore bool                   `json:"has_more"`
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
	}

	if types := q.Get("types"); types != "" {
		opts.Types = strings.Split(types, ",")
	}

	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	opts.Limit = limit + 1

	if cursor := q.Get("before"); cursor != "" {
		t, source, sourceID, err := db.DecodeCursor(cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid before cursor: "+err.Error())
			return
		}
		opts.BeforeTime = &t
		opts.BeforeSource = source
		opts.BeforeSourceID = sourceID
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

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
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

	writeJSON(w, http.StatusOK, activityResponse{Items: out, HasMore: hasMore})
}
