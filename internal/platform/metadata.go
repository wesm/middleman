package platform

import (
	"fmt"
	"regexp"
	"strings"
)

type Metadata struct {
	Kind               Kind
	Label              string
	DefaultHost        string
	AllowNestedOwner   bool
	LowercaseRepoNames bool
}

var validKindRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

var builtInMetadata = map[Kind]Metadata{
	KindGitHub: {
		Kind:               KindGitHub,
		Label:              "GitHub",
		DefaultHost:        DefaultGitHubHost,
		AllowNestedOwner:   false,
		LowercaseRepoNames: true,
	},
	KindGitLab: {
		Kind:             KindGitLab,
		Label:            "GitLab",
		DefaultHost:      DefaultGitLabHost,
		AllowNestedOwner: true,
	},
}

func NormalizeKind(raw string) (Kind, error) {
	kind := Kind(strings.ToLower(strings.TrimSpace(raw)))
	if kind == "" {
		return KindGitHub, nil
	}
	if !validKindRe.MatchString(string(kind)) {
		return "", fmt.Errorf("unsupported platform %q", raw)
	}
	return kind, nil
}

func MetadataFor(kind Kind) (Metadata, bool) {
	kind, err := NormalizeKind(string(kind))
	if err != nil {
		return Metadata{}, false
	}
	meta, ok := builtInMetadata[kind]
	return meta, ok
}

func DefaultHost(kind Kind) (string, bool) {
	meta, ok := MetadataFor(kind)
	if !ok || meta.DefaultHost == "" {
		return "", false
	}
	return meta.DefaultHost, true
}

func HostOrDefault(kind Kind, host string) (string, bool) {
	host = strings.TrimSpace(host)
	if host != "" {
		return host, true
	}
	return DefaultHost(kind)
}

func AllowsNestedOwner(kind Kind) bool {
	meta, ok := MetadataFor(kind)
	if !ok {
		return true
	}
	return meta.AllowNestedOwner
}

func LowercaseRepoNames(kind Kind) bool {
	meta, ok := MetadataFor(kind)
	return ok && meta.LowercaseRepoNames
}
