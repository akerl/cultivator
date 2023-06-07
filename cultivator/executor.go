package cultivator

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"

	"github.com/bradleyfalzon/ghinstallation"
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
	logger.DebugMsg(fmt.Sprintf("found %d targets", len(targets)))
	logger.DebugMsg(fmt.Sprintf("running %d checks", len(e.Config.Checks)))
	for _, c := range e.Config.Checks {
		err := e.runCheck(c, targets)
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) runCheck(c string, targets []target) error {
	dir, err := os.MkdirTemp("", "cultivator-")
	if err != nil {
		return err
	}
	logger.DebugMsg(fmt.Sprintf("created tmpdir for %s at %s", c, dir))
	defer os.RemoveAll(dir)

	for _, t := range targets {
		logger.DebugMsg(fmt.Sprintf("running %s on %s", c, t.Path))
		if err := t.sync(); err != nil {
			return err
		}
		if err := t.runCheck(c, dir); err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) appClient() (*github.Client, error) {
	logger.DebugMsg("creating app client")
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
	logger.DebugMsg(fmt.Sprintf("creating install client for %d", installID))
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

func (e *Executor) basicAuth(installID int64) (string, error) {
	logger.DebugMsg(fmt.Sprintf("creating basic auth token for %d", installID))
	appClient, err := e.appClient()
	if err != nil {
		return "", err
	}

	token, _, err := appClient.Apps.CreateInstallationToken(context.Background(), installID, nil)
	if err != nil {
		return "", err
	}

	return "x-access-token:" + *token.Token, nil
}

func (e *Executor) targets() ([]target, error) {
	var t []target

	appClient, err := e.appClient()
	if err != nil {
		return t, err
	}

	user, _, err := appClient.Apps.Get(context.Background(), "")
	if err != nil {
		return t, err
	}

	installations, _, err := appClient.Apps.ListInstallations(context.Background(), nil)
	logger.DebugMsg(fmt.Sprintf("found %d installations", len(installations)))
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
					Slug:      *user.Slug,
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
