// Package client parses untrusted local client request hints.
package client

import (
	"fmt"
	"net/url"
	"strings"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

// RepositoryHint resolves untrusted repository identity from an explicit native
// selector or the local origin remote. The service remains the authority.
func RepositoryHint(cwd, explicit string) (*repowolfv1.RepositorySelector, error) {
	if explicit != "" {
		selector, err := parseExplicit(explicit)
		if err != nil {
			return nil, fmt.Errorf("invalid --repo; expected [HOST/]OWNER/REPO")
		}
		return selector, nil
	}
	remote, err := originRemote(cwd)
	if err != nil {
		return nil, fmt.Errorf("repository unavailable; use --repo OWNER/REPO")
	}
	selector, err := parseRemote(remote)
	if err != nil {
		return nil, fmt.Errorf("repository unavailable; use --repo OWNER/REPO")
	}
	return selector, nil
}

func parseExplicit(value string) (*repowolfv1.RepositorySelector, error) {
	parts := strings.Split(value, "/")
	host := "github.com"
	if len(parts) == 3 {
		host, parts = parts[0], parts[1:]
	}
	if len(parts) != 2 || !validHost(host) || !validSlug(parts[0], parts[1]) {
		return nil, fmt.Errorf("invalid repository")
	}
	return &repowolfv1.RepositorySelector{Host: strings.ToLower(host), Owner: parts[0], Name: parts[1]}, nil
}

func parseRemote(remote string) (*repowolfv1.RepositorySelector, error) {
	remote = strings.TrimSpace(remote)
	if strings.ContainsAny(remote, "\r\n\x00") {
		return nil, fmt.Errorf("invalid remote")
	}
	if !strings.Contains(remote, "://") {
		at := strings.LastIndexByte(remote, '@')
		colon := strings.IndexByte(remote, ':')
		if at < 1 || colon <= at+1 {
			return nil, fmt.Errorf("invalid remote")
		}
		return selectorFromPath(remote[at+1:colon], remote[colon+1:])
	}
	parsed, err := url.Parse(remote)
	if err != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "ssh") {
		return nil, fmt.Errorf("invalid remote")
	}
	if parsed.Scheme == "https" && parsed.User != nil || parsed.Port() != "" {
		return nil, fmt.Errorf("invalid remote")
	}
	return selectorFromPath(parsed.Hostname(), parsed.EscapedPath())
}

func selectorFromPath(host, path string) (*repowolfv1.RepositorySelector, error) {
	rawPath := strings.TrimPrefix(path, "/")
	decoded, err := url.PathUnescape(rawPath)
	if err != nil || decoded != rawPath {
		return nil, fmt.Errorf("invalid remote path")
	}
	decoded = strings.TrimSuffix(decoded, ".git")
	parts := strings.Split(decoded, "/")
	if len(parts) != 2 || !validHost(host) || !validSlug(parts[0], parts[1]) {
		return nil, fmt.Errorf("invalid remote")
	}
	return &repowolfv1.RepositorySelector{Host: strings.ToLower(host), Owner: parts[0], Name: parts[1]}, nil
}

func validHost(value string) bool {
	if value == "" || len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	return true
}

func validSlug(owner, name string) bool {
	if owner == "" || len(owner) > 39 || name == "" || len(name) > 100 || name == "." || name == ".." {
		return false
	}
	if owner[0] == '-' || owner[len(owner)-1] == '-' || strings.Contains(owner, "--") {
		return false
	}
	for _, character := range owner {
		if !(asciiAlphaNumeric(character) || character == '-') {
			return false
		}
	}
	for _, character := range name {
		if !(asciiAlphaNumeric(character) || character == '.' || character == '_' || character == '-') {
			return false
		}
	}
	return true
}

func asciiAlphaNumeric(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
}
