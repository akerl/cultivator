package cultivator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/google/go-github/v52/github"
)

type target struct {
	Data      *github.Repository
	Path      string
	Client    *github.Client
	BasicAuth transport.AuthMethod
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

func (t *target) sync() error {
	exists, err := repoExists(t.Path)
	if err != nil {
		return err
	}

	if exists {
		return t.cleanRepo()
	}
	return t.cloneRepo()
}

func (t *target) cleanRepo() error {
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
		Auth:     t.BasicAuth,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return err
	}

	return w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewRemoteReferenceName("origin", *t.Data.DefaultBranch),
		Force:  true,
	})
}

func (t *target) cloneRepo() error {
	err := os.MkdirAll(t.Path, 0750)
	if err != nil {
		return nil
	}

	_, err = git.PlainClone(t.Path, true, &git.CloneOptions{
		URL:  *t.Data.CloneURL,
		Auth: t.BasicAuth,
	})
	if err == transport.ErrEmptyRemoteRepository {
		err = nil
	}
	return err
}

// RunCheck executes a check against a single repo
func (t *target) RunCheck(c string, dir string) error {
	cmd := exec.Command(c, dir)
	cmd.Dir = t.Path
	out, err := cmd.Output()
	if err != nil {
		return err
	}

	var change Change
	err = json.Unmarshal(out, &change)
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

	s, err := w.Status()
	if err != nil {
		return err
	}

	if s.IsClean() {
		return t.closePR(change)
	}
	return t.openPR(change)
}

func (t *target) closePR(change Change) error {
	user, _, err := t.Client.Users.Get(context.Background(), "")
	if err != nil {
		return err
	}

	prs, _, err := t.Client.PullRequests.List(
		context.Background(),
		*t.Data.Owner.Login,
		*t.Data.Name,
		&github.PullRequestListOptions{Head: fmt.Sprintf("%s:%s", *user.Login, change.Branch)},
	)

	if len(prs) == 0 {
		return nil
	} else if len(prs) > 1 {
		return fmt.Errorf("got more than 1 PR for same branch, refusing to proceed")
	}

	state := "closed"

	_, _, err = t.Client.PullRequests.Edit(
		context.Background(),
		*t.Data.Owner.Login,
		*t.Data.Name,
		int(*prs[0].ID),
		&github.PullRequest{State: &state},
	)
	return err
}

func (t *target) openPR(change Change) error {
	r, err := git.PlainOpen(t.Path)
	if err != nil {
		return err
	}
	w, err := r.Worktree()
	if err != nil {
		return err
	}

	err = w.AddGlob(".")
	if err != nil {
		return err
	}

	hash, err := w.Commit(change.CommitMsg, nil)
	if err != nil {
		return err
	}

	err = r.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth:       t.BasicAuth,
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("%s:%s", hash, *t.Data.DefaultBranch)),
		},
		Force: true,
	})
	if err != nil {
		return err
	}

	_, _, err = t.Client.PullRequests.Create(
		context.Background(),
		*t.Data.Owner.Login,
		*t.Data.Name,
		&github.NewPullRequest{
			Title: &change.Name,
			Body:  &change.Body,
			Base:  t.Data.DefaultBranch,
			Head:  &change.Branch,
		},
	)
	return err
}
