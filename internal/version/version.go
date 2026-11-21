package version

import "fmt"

var (
	Version = "0.1.0"
	Commit  = "dev"
)

func String() string {
	return fmt.Sprintf("peaceclaude %s (%s)", Version, Commit)
}
