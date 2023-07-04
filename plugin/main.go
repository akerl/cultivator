package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/akerl/cultivator/cultivator"
)

// Condition checks whether a plugin is applicable to the repo
type Condition func(string) bool

// Executor defines how to update the repo
type Executor func(string) error

// Change aliases the cultivator type for convenience
type Change cultivator.Change

// Commit defines the metadata of a git commit and PR
type Commit func(string) Change

// Plugin defines a standard cultivator check
type Plugin struct {
	Commit    Commit
	Condition Condition
	Executor  Executor
}

// Run executes the plugin
func (p *Plugin) Run() {
	if len(os.Args) != 2 {
		panic("unexpected number of args provided")
	}
	tmpdir := os.Args[1]

	ok := p.Condition(tmpdir)
	if ok {
		err := p.Executor(tmpdir)
		if err != nil {
			panic(err.Error())
		}
	}

	c := p.Commit(tmpdir)

	output, err := json.Marshal(c)
	if err != nil {
		panic(err.Error())
	}
	fmt.Print(string(output))
}

// FileExistsCondition helps check if a set of files exists
func FileExistsCondition(paths ...string) Condition {
	return func(_ string) bool {
		for _, path := range paths {
			_, err := os.Stat(path)
			if err == nil {
				return true
			}
		}
		return false
	}
}

// AnyCondition combines a set of checks and runs the plugin if any check passes
func AnyCondition(conditions ...Condition) Condition {
	return func(tmpdir string) bool {
		for _, c := range conditions {
			if c(tmpdir) {
				return true
			}
		}
		return false
	}
}

// AllCondition combines a set of checks and runs the plugin if all checks pass
func AllCondition(conditions ...Condition) Condition {
	return func(tmpdir string) bool {
		for _, c := range conditions {
			if !c(tmpdir) {
				return false
			}
		}
		return true
	}
}

// FindReplaceFunc defines an updater for FindReplace matches
type FindReplaceFunc func([]string) string

// FindReplace checks a file and runs an update function on matching lines
func FindReplace(file string, pattern *regexp.Regexp, fn FindReplaceFunc) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	lines := strings.Split(string(raw), "\n")

	for index, line := range lines {
		if pattern.MatchString(line) {
			lines[index] = fn(pattern.FindStringSubmatch(line))
		}
	}

	newFile := strings.Join(lines, "\n")
	return os.WriteFile(file, []byte(newFile), 0644)
}

// SimpleCommit returns a static set of values for the change
func SimpleCommit(name, branch, body, commitmsg string) func(string) Change {
	return func(_ string) Change {
		return Change{
			Name:      name,
			Branch:    branch,
			Body:      body,
			CommitMsg: commitmsg,
		}
	}
}
