package github

import (
	"os"

	adapterdriven "github.com/dpopsuev/conty/internal/adapter/driven"
	"github.com/dpopsuev/conty/internal/config"
	"github.com/dpopsuev/conty/internal/port/driven"
)

func init() {
	adapterdriven.Register("github", 0, func(name string, backend config.Backend) (driven.CIAdapter, error) {
		token := backend.ResolveToken()
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN")
		}
		owner := backend.User
		if owner == "" {
			owner = os.Getenv("GITHUB_OWNER")
		}
		repo := backend.URL
		if repo == "" {
			repo = os.Getenv("GITHUB_REPO")
		}
		if owner == "" || repo == "" {
			return nil, nil
		}
		return New(name, token, owner, repo)
	})
}
