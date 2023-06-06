package cultivator

import (
	"context"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"

	"github.com/bradleyfalzon/ghinstallation"
	"github.com/ghodss/yaml"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitHttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/v52/github"
)

const defaultConfigFile = "config.yaml"

// Config describes options for changing the behavior of Cultivator
type Config struct {
	CacheDir       string   `json:"cache_dir"`
	IntegrationID  int      `json:"integration_id"`
	PrivateKeyFile string   `json:"private_key_file"`
	Checks         []string `json:"checks"`
}

// Executor defines a cultivator instance
type Executor struct {
	Config Config
}

type target struct {
	Data      *github.Repository
	Path      string
	Client    *github.Client
	InstallID int64
}

func loadConfig(fileArg string) (Config, error) {
	var c Config
	var err error

	file := fileArg
	if file == "" {
		file = defaultConfigFile
	}

	contents, err := ioutil.ReadFile(file)
	if err != nil {
		return c, err
	}

	err = yaml.Unmarshal(contents, &c)
	return c, err
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
		if err := e.sync(t); err != nil {
			return err
		}
		if err := e.runCheckOnTarget(c, t, dir); err != nil {
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

func (e *Executor) gitAuthTransport(installID int64) (transport.AuthMethod, error) {
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
					InstallID: *i.ID,
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

func (e *Executor) sync(t target) error {
	exists, err := repoExists(t.Path)
	if err != nil {
		return err
	}

	if exists {
		return e.cleanRepo(t)
	}
	return e.cloneRepo(t)
}

func repoExists(filePath string) (bool, error) {
	gitPath := path.Join(filePath, "objects")
	_, err := os.Stat(gitPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (e *Executor) cleanRepo(t target) error {
	gitAuth, err := e.gitAuthTransport(t.InstallID)
	if err != nil {
		return err
	}

	r, err := git.PlainOpen(t.Path)
	if err != nil {
		return err
	}
	w, err := r.Worktree()
	if err != nil {
		return err
	}

	remote, err := r.Remote("origin")
	if err != nil {
		return err
	}

	err = remote.Fetch(&git.FetchOptions{
		RefSpecs: []config.RefSpec{"refs/*:refs/*", "HEAD:refs/heads/HEAD"},
		Force:    true,
		Auth:     gitAuth,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return err
	}

	return w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewRemoteReferenceName("origin", *t.Data.DefaultBranch),
		Force:  true,
	})
}

func (e *Executor) cloneRepo(t target) error {
	gitAuth, err := e.gitAuthTransport(t.InstallID)
	if err != nil {
		return err
	}

	err = os.MkdirAll(t.Path, 0750)
	if err != nil {
		return nil
	}

	_, err = git.PlainClone(t.Path, true, &git.CloneOptions{
		URL:  *t.Data.CloneURL,
		Auth: gitAuth,
	})
	if err == transport.ErrEmptyRemoteRepository {
		err = nil
	}
	return err
}

func (e *Executor) runCheckOnTarget(c string, t target, dir string) error {
	cmd := exec.Command(c, dir)
	cmd.Dir = t.Path
	out, err := cmd.Output()
	if err != nil {
		return err
	}

	return nil
}
