package server

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type getSettingsOutput = bodyOutput[settingsResponse]

type updateSettingsInput struct {
	Body updateSettingsRequest
}

type addRepoInput struct {
	Body struct {
		Provider     string `json:"provider,omitempty"`
		Host         string `json:"host,omitempty"`
		PlatformHost string `json:"platform_host,omitempty"`
		Owner        string `json:"owner"`
		Name         string `json:"name"`
	}
}

type repoConfigInput struct {
	Provider     string `path:"provider"`
	PlatformHost string
	Owner        string `path:"owner"`
	Name         string `path:"name"`
}

type repoConfigHostInput struct {
	Provider     string `path:"provider"`
	PlatformHost string `path:"platform_host"`
	Owner        string `path:"owner"`
	Name         string `path:"name"`
}

type settingsOutput = bodyOutput[settingsResponse]

func (s *Server) registerSettingsAPI(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-settings",
		Method:      http.MethodGet,
		Path:        "/settings",
	}, s.getSettings)
	huma.Register(api, huma.Operation{
		OperationID: "update-settings",
		Method:      http.MethodPut,
		Path:        "/settings",
	}, s.updateSettings)
	huma.Register(api, huma.Operation{
		OperationID:   "add-repo",
		Method:        http.MethodPost,
		Path:          "/repos",
		DefaultStatus: http.StatusCreated,
	}, s.addConfiguredRepo)
	huma.Register(api, huma.Operation{
		OperationID: "refresh-repo",
		Method:      http.MethodPost,
		Path:        "/repo/{provider}/{owner}/{name}/refresh",
	}, s.refreshConfiguredRepo)
	huma.Register(api, huma.Operation{
		OperationID: "refresh-repo-on-host",
		Method:      http.MethodPost,
		Path:        "/host/{platform_host}/repo/{provider}/{owner}/{name}/refresh",
	}, s.refreshConfiguredRepoOnHost)
	huma.Register(api, huma.Operation{
		OperationID:   "delete-repo",
		Method:        http.MethodDelete,
		Path:          "/repo/{provider}/{owner}/{name}",
		DefaultStatus: http.StatusNoContent,
	}, s.deleteConfiguredRepo)
	huma.Register(api, huma.Operation{
		OperationID:   "delete-repo-on-host",
		Method:        http.MethodDelete,
		Path:          "/host/{platform_host}/repo/{provider}/{owner}/{name}",
		DefaultStatus: http.StatusNoContent,
	}, s.deleteConfiguredRepoOnHost)
}
