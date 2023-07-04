package cultivator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/google/go-github/v52/github"
)

type target struct {
	Data      *github.Repository
	Path      string
	Client    *github.Client
	Slug      string
	BasicAuth string
	Updated   bool
}

func repoExists(filePath string) (bool, error) {
	gitPath := path.Join(filePath, ".git")
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
	logger.DebugMsg(fmt.Sprintf("cleaning repo %s", t.Path))
	_, _, err := t.runCommand("git", "remote", "set-url", "origin", t.cloneURL())
	if err != nil {
		return err
	}

	_, _, err = t.runCommand("git", "clean", "-f")
	if err != nil {
		return err
	}

	_, _, err = t.runCommand("git", "reset", "--hard", "origin/"+*t.Data.DefaultBranch)
	if err != nil {
		return err
	}

	if t.Updated {
		return nil
	}

	_, _, err = t.runCommand("git", "pull", "origin", *t.Data.DefaultBranch)
	if err != nil {
		return err
	}
	t.Updated = true
	return nil
}

func (t *target) cloneRepo() error {
	logger.DebugMsg(fmt.Sprintf("cloning repo %s", t.Path))

	err := os.MkdirAll(t.Path, 0750)
	if err != nil {
		return nil
	}

	_, _, err = t.runCommand("git", "clone", "--recursive", t.cloneURL(), ".")
	return err
}

func (t *target) cloneURL() string {
	return fmt.Sprintf("https://%s@github.com/%s", t.BasicAuth, *t.Data.FullName)
}

func (t *target) runCommand(cmd ...string) (string, string, error) {
	logger.DebugMsg(fmt.Sprintf("executing %v on %s", cmd, t.Path))
	e := exec.Command(cmd[0], cmd[1:]...)
	var outb, errb bytes.Buffer
	e.Stdout = &outb
	e.Stderr = &errb
	e.Dir = t.Path
	err := e.Run()
	if err != nil {
		return "", "", err
	}
	return outb.String(), errb.String(), nil
}

func (t *target) runCheck(c string, dir string) error {
	out, _, err := t.runCommand(c, dir)
	if err != nil {
		return err
	}

	var change Change
	err = json.Unmarshal([]byte(out), &change)
	if err != nil {
		return err
	}

	out, _, err = t.runCommand("git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if len(out) == 0 {
		logger.DebugMsg(fmt.Sprintf("no changes for %s on %s", c, t.Path))
		return t.closePR(change)
	}
	logger.DebugMsg(fmt.Sprintf("found changes for %s on %s", c, t.Path))
	return t.openPR(change)
}

func (t *target) closePR(change Change) error {
	prs, _, err := t.Client.PullRequests.List(
		context.Background(),
		*t.Data.Owner.Login,
		*t.Data.Name,
		&github.PullRequestListOptions{Head: fmt.Sprintf("%s:%s", t.Slug, change.Branch)},
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
	_, _, err := t.runCommand("git", "add", ".")
	if err != nil {
		return err
	}

	_, _, err = t.runCommand("git", "commit", "-m", change.CommitMsg)
	if err != nil {
		return err
	}

	ref := *t.Data.DefaultBranch + ":" + change.Branch
	_, _, err = t.runCommand("git", "push", "--force", "origin", ref)
	if err != nil {
		return err
	}

	prs, _, err := t.Client.PullRequests.List(
		context.Background(),
		*t.Data.Owner.Login,
		*t.Data.Name,
		&github.PullRequestListOptions{Head: fmt.Sprintf("%s:%s", t.Slug, change.Branch)},
	)
	if err != nil {
		return err
	}

	if len(prs) == 0 {
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

	logger.DebugMsg(fmt.Sprintf("updating PR for %s on %s", change.Branch, t.Path))
	_, _, err = t.Client.PullRequests.Edit(
		context.Background(),
		*t.Data.Owner.Login,
		*t.Data.Name,
		int(*prs[0].ID),
		&github.PullRequest{
			Title: &change.Name,
			Body:  &change.Body,
		},
	)

	if err != nil {
		fmt.Printf("%+v\n", prs)
		fmt.Printf("%s:%s", t.Slug, change.Branch)
	}

	return err
}
