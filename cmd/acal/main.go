package main

import (
	"os"

	"github.com/agis/acal/internal/app"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	app.SetBuildInfo(version, commit, date)
	os.Exit(app.Execute())
}
