package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wesm/middleman/internal/server"
)

func main() {
	var out string
	flag.StringVar(&out, "out", "frontend/openapi/openapi.json", "path to write the generated OpenAPI document")
	flag.Parse()

	spec, err := server.NewOpenAPI().MarshalJSON()
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal openapi: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", filepath.Dir(out), err)
		os.Exit(1)
	}

	if err := os.WriteFile(out, append(spec, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", out, err)
		os.Exit(1)
	}
}
