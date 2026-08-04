package gitssh

import (
	"reflect"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func TestParseAcceptsExactGitGeneratedForms(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		operation Operation
		selector  *repowolfv1.RepositorySelector
	}{
		{
			name:      "upload without options",
			args:      []string{"git@github.example", "git-upload-pack 'owner/repo.git'"},
			operation: UploadPack,
			selector:  &repowolfv1.RepositorySelector{Host: "github.example", Owner: "owner", Name: "repo"},
		},
		{
			name:      "receive with protocol option",
			args:      []string{"-o", "SendEnv=GIT_PROTOCOL", "git@GitHub.Example", "git-receive-pack 'owner/repo.git'"},
			operation: ReceivePack,
			selector:  &repowolfv1.RepositorySelector{Host: "github.example", Owner: "owner", Name: "repo"},
		},
		{
			name:      "upload with port",
			args:      []string{"-p", "2222", "git@github.example", "git-upload-pack 'owner/repo.git'"},
			operation: UploadPack,
			selector:  &repowolfv1.RepositorySelector{Host: "github.example", SshPort: 2222, Owner: "owner", Name: "repo"},
		},
		{
			name:      "upload from SSH URL with generated leading slash",
			args:      []string{"-o", "SendEnv=GIT_PROTOCOL", "-p", "2222", "git@github.example", "git-upload-pack '/owner/repo.git'"},
			operation: UploadPack,
			selector:  &repowolfv1.RepositorySelector{Host: "github.example", SshPort: 2222, Owner: "owner", Name: "repo"},
		},
		{
			name:      "receive from SSH URL with generated leading slash",
			args:      []string{"-o", "SendEnv=GIT_PROTOCOL", "-p", "65535", "git@github.example", "git-receive-pack '/owner/repo.git'"},
			operation: ReceivePack,
			selector:  &repowolfv1.RepositorySelector{Host: "github.example", SshPort: 65535, Owner: "owner", Name: "repo"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := Parse(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if request.Operation != test.operation || !reflect.DeepEqual(request.Repository, test.selector) {
				t.Fatalf("Parse(%q) = %#v", test.args, request)
			}
		})
	}
}

func TestParseRejectsEverythingOutsideExactGitShape(t *testing.T) {
	validCommand := "git-upload-pack 'owner/repo.git'"
	tests := map[string][]string{
		"no arguments":               nil,
		"host only":                  {"git@github.example"},
		"missing user":               {"github.example", validCommand},
		"wrong user":                 {"root@github.example", validCommand},
		"empty host":                 {"git@", validCommand},
		"host with user suffix":      {"git@github.example@evil.example", validCommand},
		"host with port":             {"git@github.example:22", validCommand},
		"IP literal syntax":          {"git@[::1]", validCommand},
		"leading dot host":           {"git@.github.example", validCommand},
		"empty host label":           {"git@github..example", validCommand},
		"host label punctuation":     {"git@git_example", validCommand},
		"zero port":                  {"-p", "0", "git@github.example", validCommand},
		"negative port":              {"-p", "-1", "git@github.example", validCommand},
		"large port":                 {"-p", "65536", "git@github.example", validCommand},
		"signed port":                {"-p", "+22", "git@github.example", validCommand},
		"nondecimal port":            {"-p", "0x16", "git@github.example", validCommand},
		"empty port":                 {"-p", "", "git@github.example", validCommand},
		"port after host":            {"git@github.example", "-p", "22", validCommand},
		"non-generated option order": {"-p", "22", "-o", "SendEnv=GIT_PROTOCOL", "git@github.example", validCommand},
		"proxy command":              {"-o", "ProxyCommand=sh", "git@github.example", validCommand},
		"environment option":         {"-o", "SetEnv=TOKEN=secret", "git@github.example", validCommand},
		"combined option":            {"-oSendEnv=GIT_PROTOCOL", "git@github.example", validCommand},
		"shell flag":                 {"-t", "git@github.example", validCommand},
		"config flag":                {"-F", "/tmp/config", "git@github.example", validCommand},
		"option terminator":          {"--", "git@github.example", validCommand},
		"missing git suffix":         {"git@github.example", "git-upload-pack 'owner/repo'"},
		"unquoted slug":              {"git@github.example", "git-upload-pack owner/repo.git"},
		"double quoted slug":         {"git@github.example", `git-upload-pack "owner/repo.git"`},
		"two leading path slashes":   {"git@github.example", "git-upload-pack '//owner/repo.git'"},
		"extra path component":       {"git@github.example", "git-upload-pack 'group/owner/repo.git'"},
		"slash extra path component": {"git@github.example", "git-upload-pack '/group/owner/repo.git'"},
		"empty owner":                {"git@github.example", "git-upload-pack '/repo.git'"},
		"empty repository":           {"git@github.example", "git-upload-pack 'owner/.git'"},
		"dot repository":             {"git@github.example", "git-upload-pack 'owner/...git'"},
		"owner punctuation":          {"git@github.example", "git-upload-pack 'owner_name/repo.git'"},
		"repository whitespace":      {"git@github.example", "git-upload-pack 'owner/bad repo.git'"},
		"shell command":              {"git@github.example", "sh"},
		"archive command":            {"git@github.example", "git archive 'owner/repo.git'"},
		"command injection":          {"git@github.example", "git-upload-pack 'owner/repo.git'; id"},
		"trailing command":           {"git@github.example", "git-upload-pack 'owner/repo.git' trailing"},
		"newline command":            {"git@github.example", "git-upload-pack 'owner/repo.git'\nid"},
		"extra argument":             {"git@github.example", validCommand, "tail"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			if request, err := Parse(args); err == nil {
				t.Fatalf("Parse(%q) accepted %#v", args, request)
			}
		})
	}
}

func TestParseRejectsUnboundedOrInvalidArgumentData(t *testing.T) {
	tooMany := make([]string, 65)
	for index := range tooMany {
		tooMany[index] = "x"
	}
	for name, args := range map[string][]string{
		"too many":      tooMany,
		"too large":     {"git@github.example", "git-upload-pack 'owner/" + string(make([]byte, 64<<10)) + ".git'"},
		"NUL":           {"git@github.example", "git-upload-pack 'owner/repo.git'\x00"},
		"invalid UTF-8": {"git@github.example", string([]byte{0xff})},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(args); err == nil {
				t.Fatalf("Parse accepted %s input", name)
			}
		})
	}
}
