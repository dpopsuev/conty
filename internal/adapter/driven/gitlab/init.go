package gitlab

import (
	"os"

	adapterdriven "github.com/DanyPops/conty/internal/adapter/driven"
	"github.com/DanyPops/conty/internal/config"
	"github.com/DanyPops/conty/internal/port/driven"
)

func init() {
	adapterdriven.Register("gitlab", 0, func(name string, backend config.Backend) (driven.CIAdapter, error) {
		token := backend.ResolveToken()
		if token == "" {
			token = os.Getenv("GITLAB_TOKEN")
		}
		projectID := backend.User
		if projectID == "" {
			projectID = os.Getenv("GITLAB_PROJECT")
		}
		baseURL := backend.URL
		if baseURL == "" {
			baseURL = os.Getenv("GITLAB_URL")
		}
		if projectID == "" {
			return nil, nil
		}
		return New(name, token, projectID, baseURL)
	})
}
