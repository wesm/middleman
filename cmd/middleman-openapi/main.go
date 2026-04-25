package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wesm/middleman/internal/server"
)

func main() {
	var out string
	var version string
	flag.StringVar(&out, "out", "frontend/openapi/openapi.json", "path to write the generated OpenAPI document")
	flag.StringVar(&version, "version", "3.1", "OpenAPI version to write: 3.1 or 3.0")
	flag.Parse()

	openAPI := server.NewOpenAPI()
	var (
		spec []byte
		err  error
	)
	switch version {
	case "3.1":
		spec, err = openAPI.MarshalJSON()
	case "3.0":
		spec, err = openAPI.Downgrade()
	default:
		fmt.Fprintf(os.Stderr, "unsupported openapi version %q\n", version)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal openapi %s: %v\n", version, err)
		os.Exit(1)
	}
	spec, err = prettyJSON(spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "format openapi %s: %v\n", version, err)
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

func prettyJSON(spec []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, spec, "", "  "); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
