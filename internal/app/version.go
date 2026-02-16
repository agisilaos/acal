package app

import "fmt"

var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

func SetBuildInfo(version, commit, date string) {
	if version != "" {
		buildVersion = version
	}
	if commit != "" {
		buildCommit = commit
	}
	if date != "" {
		buildDate = date
	}
}

func BuildVersionString() string {
	return fmt.Sprintf("%s (%s) %s", buildVersion, buildCommit, buildDate)
}
