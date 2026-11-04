package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/yuhaiin/purego-sqlite/internal/sqlitesource"
)

func main() {
	force := flag.Bool("force", false, "rebuild even if the upstream version is unchanged")
	buildInputs := flag.Bool("build-inputs", false, "validate the current source pin after build input changes")
	flag.Parse()
	if err := run(*force, *buildInputs); err != nil {
		log.Fatal(err)
	}
}

func run(force, buildInputs bool) error {
	current, err := sqlitesource.ReadPin("scripts/source.json")
	if err != nil {
		return err
	}
	if buildInputs {
		return (sqlitesource.Decision{CurrentVersion: current.Version, LatestVersion: "not checked", Version: current.Version, Changed: true, Reason: "build-inputs-changed", Pin: current}).Report()
	}
	d, err := sqlitesource.Check(context.Background(), &http.Client{Timeout: time.Minute}, sqlitesource.DownloadURL, current, force)
	if err != nil {
		return err
	}
	if d.UpdateAvailable {
		if err := sqlitesource.WriteJSON("scripts/source.json", d.Pin); err != nil {
			return err
		}
	}
	return d.Report()
}
