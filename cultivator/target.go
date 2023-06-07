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
		logger.DebugMsg(fmt.Sprintf("repo already exists: %s", t.Path))
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

	logger.DebugMsg(fmt.Sprintf("fetching refs: %s", t.Path))
	err = remote.Fetch(&git.FetchOptions{
		RefSpecs: []config.RefSpec{"refs/*:refs/*", "HEAD:refs/heads/HEAD"},
		Force:    true,
		Auth:     t.BasicAuth,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return err
	}

	logger.DebugMsg(fmt.Sprintf("checking out %s", *t.Data.DefaultBranch))
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

	logger.DebugMsg(fmt.Sprintf("cloning %s", t.Path))
	_, err = git.PlainClone(t.Path, false, &git.CloneOptions{
		URL:  *t.Data.CloneURL,
		Auth: t.BasicAuth,
	})
	return err
}

func (t *target) runCheck(c string, dir string) error {
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
		logger.DebugMsg(fmt.Sprintf("no changes for %s on %s", c, t.Path))
		return t.closePR(change)
	}
	logger.DebugMsg(fmt.Sprintf("found changes for %s on %s", c, t.Path))
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
		logger.DebugMsg(fmt.Sprintf("no open PRs for %s on %s", change.Name, t.Path))
		return nil
	} else if len(prs) > 1 {
		return fmt.Errorf("got more than 1 PR for same branch, refusing to proceed")
	}

	logger.DebugMsg(fmt.Sprintf("closing PR for %s on %s", change.Name, t.Path))
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

	logger.DebugMsg(fmt.Sprintf("pushing %s for %s", change.Branch, t.Path))
	err = r.Push(&git.PushOptions{
		RemoteName: "origin",
		Auth:       t.BasicAuth,
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("%s:%s", hash, change.Branch)),
		},
		Force: true,
	})
	if err != nil {
		return err
	}

	logger.DebugMsg(fmt.Sprintf("creating PR for %s on %s", change.Branch, t.Path))
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
