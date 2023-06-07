package cultivator

// Change defines the metadata for a Pull Request against a repo
type Change struct {
	Name      string `json:"name"`
	Branch    string `json:"branch"`
	Body      string `json:"body"`
	CommitMsg string `json:"commit_msg"`
}
