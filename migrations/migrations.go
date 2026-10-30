// Package migrations holds the ordered SQL migration files applied at startup.
package migrations

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed *.sql
var files embed.FS

// Statement is one migration: a filename and its SQL body.
type Statement struct {
	Name string
	SQL  string
}

// Ordered returns all embedded migrations sorted by filename.
func Ordered() ([]Statement, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([]Statement, 0, len(names))
	for _, n := range names {
		data, err := fs.ReadFile(files, n)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", n, err)
		}
		out = append(out, Statement{Name: n, SQL: string(data)})
	}
	return out, nil
}
