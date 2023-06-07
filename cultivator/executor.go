package cultivator

import (
	"context"
	"net/http"
	"os"
	"path"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitHttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/v52/github"
)

// Executor defines a cultivator instance
type Executor struct {
	Config Config
}

// NewFromFile creates a new Executor from a config file
func NewFromFile(fileArg string) (Executor, error) {
	c, err := loadConfig(fileArg)
	if err != nil {
		return Executor{}, err
	}
	return Executor{Config: c}, err
}

// Execute checks on all visible repos
func (e *Executor) Execute() error {
	targets, err := e.targets()
	if err != nil {
		return err
	}

	for _, c := range e.Config.Checks {
		e.runCheck(c, targets)
	}
	return nil
}

func (e *Executor) runCheck(c string, targets []target) error {
	dir, err := os.MkdirTemp("", "cultivator-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	for _, t := range targets {
		if err := t.sync(); err != nil {
			return err
		}
		if err := t.RunCheck(c, dir); err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) appClient() (*github.Client, error) {
	itr, err := ghinstallation.NewAppsTransportKeyFromFile(
		http.DefaultTransport,
		int64(e.Config.IntegrationID),
		e.Config.PrivateKeyFile,
	)
	if err != nil {
		return nil, err
	}

	return github.NewClient(&http.Client{Transport: itr}), nil
}

func (e *Executor) installClient(installID int64) (*github.Client, error) {
	itr, err := ghinstallation.NewKeyFromFile(
		http.DefaultTransport,
		int64(e.Config.IntegrationID),
		installID,
		e.Config.PrivateKeyFile,
	)
	if err != nil {
		return nil, err
	}

	return github.NewClient(&http.Client{Transport: itr}), nil
}

func (e *Executor) basicAuth(installID int64) (transport.AuthMethod, error) {
	appClient, err := e.appClient()
	if err != nil {
		return nil, err
	}

	token, _, err := appClient.Apps.CreateInstallationToken(context.Background(), installID, nil)
	if err != nil {
		return nil, err
	}
	return &gitHttp.BasicAuth{Username: "x-access-token", Password: *token.Token}, nil
}

func (e *Executor) targets() ([]target, error) {
	var t []target

	appClient, err := e.appClient()
	if err != nil {
		return t, err
	}

	installations, _, err := appClient.Apps.ListInstallations(context.Background(), nil)
	for _, i := range installations {
		installClient, err := e.installClient(*i.ID)
		if err != nil {
			return t, err
		}

		ba, err := e.basicAuth(*i.ID)
		if err != nil {
			return t, err
		}

		opt := &github.ListOptions{PerPage: 100}
		for {
			repos, resp, err := installClient.Apps.ListRepos(context.Background(), opt)
			if err != nil {
				return t, err
			}
			for _, r := range repos.Repositories {
				t = append(t, target{
					Data:      r,
					Path:      path.Join(e.Config.CacheDir, *r.FullName),
					Client:    installClient,
					BasicAuth: ba,
				})
			}
			if resp.NextPage == 0 {
				break
			}
			opt.Page = resp.NextPage
		}
	}

	return t, nil
}
