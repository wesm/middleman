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
		PlatformHost string `json:"platform_host,omitempty"`
		Owner        string `json:"owner"`
		Name         string `json:"name"`
	}
}

type repoConfigInput struct {
	Owner        string `path:"owner"`
	Name         string `path:"name"`
	Provider     string `query:"provider"`
	PlatformHost string `query:"platform_host"`
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
		Path:        "/repos/{owner}/{name}/refresh",
	}, s.refreshConfiguredRepo)
	huma.Register(api, huma.Operation{
		OperationID:   "delete-repo",
		Method:        http.MethodDelete,
		Path:          "/repos/{owner}/{name}",
		DefaultStatus: http.StatusNoContent,
	}, s.deleteConfiguredRepo)
}
