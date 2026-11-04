package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/yuhaiin/purego-sqlite/internal/sqlitesource"
	"github.com/yuhaiin/purego-sqlite/internal/vendoredsqlite"
)

func main() {
	update := flag.Bool("update", false, "import the checksum-verified source pin")
	flag.Parse()
	if err := run(*update); err != nil {
		log.Fatal(err)
	}
}

func run(update bool) error {
	pin, err := sqlitesource.ReadPin("scripts/source.json")
	if err != nil {
		return err
	}
	if update {
		data, err := sqlitesource.Fetch(context.Background(), &http.Client{Timeout: time.Minute}, pin.URL)
		if err != nil {
			return err
		}
		if err := vendoredsqlite.Import(".", pin, data); err != nil {
			return err
		}
	}
	if err := vendoredsqlite.Verify(".", pin); err != nil {
		return err
	}
	fmt.Println("verified official SQLite " + pin.Version)
	return nil
}
