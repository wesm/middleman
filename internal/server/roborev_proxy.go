package server

import (
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/wesm/middleman/internal/config"
)

// roborevProxy returns an HTTP handler that reverse-proxies requests
// to the roborev daemon. It strips the /api/roborev prefix before
// forwarding and streams SSE/NDJSON responses without buffering.
func roborevProxy(target string) http.Handler {
	targetURL, err := url.Parse(target)
	if err != nil {
		// Static config value; if it's invalid, fail visibly.
		panic("roborev: invalid target URL: " + err.Error())
	}

	proxy := &httputil.ReverseProxy{
		// Flush immediately for streaming responses (SSE, NDJSON).
		FlushInterval: -1,
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(targetURL)
			// Strip the /api/roborev prefix so the daemon sees
			// its own path namespace.
			r.Out.URL.Path = strings.TrimPrefix(
				r.Out.URL.Path, "/api/roborev",
			)
			if r.Out.URL.Path == "" {
				r.Out.URL.Path = "/"
			}
		},
		ErrorHandler: func(
			w http.ResponseWriter, _ *http.Request, _ error,
		) {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": "roborev daemon is not reachable",
			})
		},
	}
	return proxy
}

type roborevStatusResponse struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Endpoint  string `json:"endpoint"`
}

// handleRoborevStatus probes the roborev daemon and reports whether
// it is reachable and what version it advertises.
func handleRoborevStatus(
	cfg *config.Config,
) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		endpoint := cfg.RoborevEndpoint()
		// Sanitize: strip credentials and path, expose
		// only scheme + host for the UI error message.
		sanitized := "(configured endpoint)"
		if u, err := url.Parse(endpoint); err == nil {
			u.User = nil
			u.Path = ""
			u.RawQuery = ""
			u.Fragment = ""
			sanitized = u.String()
		}
		resp := roborevStatusResponse{Endpoint: sanitized}

		client := &http.Client{Timeout: 2 * time.Second}
		statusURL := strings.TrimRight(endpoint, "/") + "/api/status"

		r, err := client.Get(statusURL)
		if err != nil {
			writeJSON(w, http.StatusOK, resp)
			return
		}
		defer r.Body.Close()

		var body struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusOK, resp)
			return
		}
		resp.Available = true
		resp.Version = body.Version
		writeJSON(w, http.StatusOK, resp)
	}
}
