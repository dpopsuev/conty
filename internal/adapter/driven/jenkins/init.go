package jenkins

import (
	"context"
	"os"
	"time"

	adapterdriven "github.com/dpopsuev/conty/internal/adapter/driven"
	"github.com/dpopsuev/conty/internal/config"
	"github.com/dpopsuev/conty/internal/port/driven"
)

func init() {
	adapterdriven.Register("jenkins", 0, func(name string, backend config.Backend) (driven.CIAdapter, error) {
		token := backend.ResolveToken()
		if token == "" {
			token = os.Getenv("JENKINS_API_KEY")
		}
		url := backend.URL
		if url == "" {
			url = os.Getenv("JENKINS_URL")
		}
		user := backend.ResolveUser()
		if user == "" {
			user = os.Getenv("JENKINS_USER")
		}
		if url == "" || token == "" || user == "" {
			return nil, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return New(ctx, name, url, user, token)
	})
}
