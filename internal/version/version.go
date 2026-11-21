package version

import "fmt"

var (
	Version = "0.2.0"
	Commit  = "dev"
)

func String() string {
	return fmt.Sprintf("peaceclaude %s (%s)", Version, Commit)
}
