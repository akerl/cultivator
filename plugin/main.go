package plugin

import (
	"bufio"
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

// Plugin defines a standard cultivator check
type Plugin struct {
	Name      string
	Branch    string
	Body      string
	CommitMsg string
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

	c := cultivator.Change{
		Name:      p.Name,
		Branch:    p.Branch,
		Body:      p.Body,
		CommitMsg: p.CommitMsg,
	}

	output, err := json.Marshal(c)
	if err != nil {
		panic(err.Error())
	}
	fmt.Print(output)
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
	fh, err := os.Open(file)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(fh)
	var lines []string

	for scanner.Scan() {
		line := scanner.Text()
		if pattern.MatchString(line) {
			line = fn(pattern.FindStringSubmatch(line))
		}
		lines = append(lines, line)
	}

	fh.Close()

	newFile := strings.Join(lines, "\n")
	return os.WriteFile(file, []byte(newFile), 0644)
}
