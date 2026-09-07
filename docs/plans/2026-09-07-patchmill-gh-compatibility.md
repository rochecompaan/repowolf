# Patchmill GitHub CLI Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make RepoWolf support the bounded GitHub CLI command surface that Patchmill 0.20.0 uses.

**Architecture:** The client parses each approved command into a typed protobuf operation. The server authorizes and audits that operation before a provider adapter builds fixed GitHub requests. New pagination and file-input modules hide their complexity behind existing parser and adapter interfaces.

**Tech Stack:** Go 1.25, gRPC-Go, Protocol Buffers, Buf, `golang.org/x/sys/unix`, the pinned GitHub CLI, and standard Go tests.

## Global Constraints

- Work only in `/home/roche/projects/repowolf/.worktrees/issue-18-patchmill-gh-compat` on `fix/issue-18-patchmill-gh-compat`.
- Use `docs/specs/2026-09-07-patchmill-gh-compatibility-design.md` as the approved contract.
- Keep generic REST, GraphQL, JQ, template, extension, alias, and shell passthrough rejected.
- Keep `gh repo create`, `gh repo delete`, and `gh repo clone` rejected.
- Keep unrelated Patchmill bootstrap and run-once commands rejected.
- Keep the parser limit at 64 arguments and 64 KiB of argument data.
- Reject unknown fields, unknown flags, duplicate flags, `--flag=value`, malformed CSV, conflicts, and extra positional values with exit status 2.
- Handle exact `gh --version` locally before working-directory, configuration, connection, or provider access.
- Map `current_user` and `repository_view` to `repository:read`.
- Map `issue_list`, `issue_view`, and `label_list` to `issues:read`.
- Map `issue_create`, `issue_comment`, `label_create`, and `issue_label_change` to `issues:write`.
- Keep `pull_view` on `pull_requests:read` and `pull_edit` on `pull_requests:write`.
- Return at most 1,001 issues through at most 11 fixed GraphQL pages.
- Return at most 1,000 issue comments, with an overflow probe after ten full REST pages.
- Return at most 1,000 labels through at most ten fixed REST pages.
- Apply an 8 MiB aggregate provider-output limit to paginated reads.
- Apply a 4 MiB aggregate provider-output limit to mutations, including preflight output.
- Keep the normalized protobuf response and client-rendered response limit at 1 MiB.
- Keep all calls under the request context and configured provider timeout.
- Read pull-request body files through a no-follow regular-file descriptor.
- Read at most 65,537 body-file bytes and reject bodies larger than 65,536 bytes.
- Require valid UTF-8 for body-file content and never send its path over RPC.
- Hide provider details behind existing generic client diagnostics.
- Do not edit files under `gen/repowolf/v1` by hand.
- Apply the Testing Value Gate before adding a test. Use direct checks for documentation and generated-file freshness.

## Required Patchmill command surface

```text
gh --version
gh auth status
gh api user --jq .login
gh issue list --state open --limit 1001 --json number,title,body,state,labels,author,createdAt,updatedAt,url
gh issue view <number> --json number,title,body,state,labels,author,createdAt,updatedAt,url,comments
gh label list [--repo owner/name] --limit 1000 --json name
gh label create <name> [--repo owner/name] --color <color> --description <text>
gh issue edit <number> [--add-label <csv>] [--remove-label <csv>]
gh issue create [existing flags] [--label <csv>]
gh issue comment <number> --body <text>
gh repo view owner/name --json name,url,sshUrl
gh pr view <number> --json body,url
gh pr edit <number> --body-file <path>
```

## Planned module structure

The existing public seams stay unchanged: `github.Parse`, `github.Run`, `GitHubService.Execute`, and `github.Adapter.Execute` remain the entry points.

New client files keep filesystem input and Patchmill-specific grammar out of the central parser:

- `internal/client/github/identity.go` parses `auth status` and the one approved `api user` form.
- `internal/client/github/repository.go` owns repository-view positional selection.
- `internal/client/github/label.go` owns label grammar, CSV parsing, and label-value checks.
- `internal/client/github/body_file.go` owns bounded no-follow file reads.
- `internal/client/github/json_shape.go` converts typed scalar records to GitHub CLI JSON shapes.
- `internal/client/github/patchmill_parse_test.go` covers the added command grammar and JSON rendering.
- `internal/client/github/body_file_test.go` covers file types, byte limits, and body-input conflicts.

New provider files keep multi-call behavior out of the single-call command planner:

- `internal/provider/github/budget.go` tracks aggregate raw output for one operation.
- `internal/provider/github/normalize_patchmill.go` owns the new GitHub response shapes and record conversion.
- `internal/provider/github/validate_patchmill.go` owns validation for the four new request types.
- `internal/provider/github/issue_list.go` owns bounded GraphQL issue pagination.
- `internal/provider/github/issue_comments.go` owns optional REST comment pagination.
- `internal/provider/github/label_list.go` owns bounded REST label pagination and header parsing.
- `internal/provider/github/issue_labels.go` owns issue-label preflight, set computation, and one update.

Tests for each provider module live in matching focused files. Existing large test files receive only assertions that belong to their current contracts.

---

### Task 1: Add the typed protocol surface

**Files:**
- Modify: `proto/repowolf/v1/github.proto:51-259`
- Create: `internal/provider/github/protocol_patchmill_test.go`
- Generate: `gen/repowolf/v1/github.pb.go`
- Generate or verify unchanged: `gen/repowolf/v1/github_grpc.pb.go`

**Interfaces:**
- Consumes: the existing `GitHubRequest` and `GitHubResponse` oneof contracts.
- Produces: generated Go types for `current_user`, `label_list`, `label_create`, `issue_label_change`, issue comments, and repository SSH URLs.

- [ ] **Step 1: Write a descriptor test for the new field names and numbers**

Create a test that compiles against the current generated descriptor and fails because the new schema members are absent:

```go
func TestPatchmillProtocolSurface(t *testing.T) {
	file := repowolfv1.File_repowolf_v1_github_proto
	want := map[protoreflect.Name]map[protoreflect.Name]protoreflect.FieldNumber{
		"GitHubIssueViewRequest":       {"include_comments": 2},
		"GitHubIssueLabelChangeRequest": {"number": 1, "add_labels": 2, "remove_labels": 3},
		"GitHubLabelCreateRequest":     {"name": 1, "color": 2, "description": 3},
		"GitHubLabelListRequest":       {"limit": 1},
		"GitHubUserRecord":             {"login": 1},
		"GitHubLabelRecord":            {"name": 1},
		"GitHubRepositoryRecord":       {"ssh_url": 8},
		"GitHubIssueRecord":            {"comments": 11},
	}
	for messageName, fields := range want {
		message := file.Messages().ByName(messageName)
		if message == nil {
			t.Fatalf("missing message %s", messageName)
		}
		for fieldName, fieldNumber := range fields {
			field := message.Fields().ByName(fieldName)
			if field == nil || field.Number() != fieldNumber {
				t.Errorf("%s.%s: got %v, want field %d", messageName, fieldName, field, fieldNumber)
			}
		}
	}
}
```

Add descriptor assertions for request oneof fields 30 through 33 and matching response fields 30 through 33.

- [ ] **Step 2: Run the descriptor test and observe the missing schema members**

Run:

```bash
go test ./internal/provider/github -run TestPatchmillProtocolSurface -count=1
```

Expected: FAIL with a missing-message or missing-field report.

- [ ] **Step 3: Extend the protobuf schema with additive fields**

Add these request messages and fields without changing existing numbers:

```proto
message GitHubCurrentUserRequest {}
message GitHubLabelListRequest { uint64 limit = 1; }
message GitHubLabelCreateRequest {
  string name = 1;
  string color = 2;
  string description = 3;
}
message GitHubIssueLabelChangeRequest {
  uint64 number = 1;
  repeated string add_labels = 2;
  repeated string remove_labels = 3;
}

message GitHubIssueViewRequest {
  uint64 number = 1;
  bool include_comments = 2;
}
```

Append these request oneof members:

```proto
GitHubCurrentUserRequest current_user = 30;
GitHubLabelListRequest label_list = 31;
GitHubLabelCreateRequest label_create = 32;
GitHubIssueLabelChangeRequest issue_label_change = 33;
```

Add records and additive record fields:

```proto
message GitHubUserRecord { string login = 1; }
message GitHubLabelRecord { string name = 1; }

// Add to GitHubRepositoryRecord.
string ssh_url = 8;

// Add to GitHubIssueRecord.
repeated GitHubCommentRecord comments = 11;
```

Add result messages and response oneof members:

```proto
message GitHubCurrentUserResult { GitHubUserRecord user = 1; }
message GitHubLabelListResult { repeated GitHubLabelRecord labels = 1; }
message GitHubLabelCreateResult { GitHubLabelRecord label = 1; }
message GitHubIssueLabelChangeResult { GitHubIssueRecord issue = 1; }

GitHubCurrentUserResult current_user = 30;
GitHubLabelListResult label_list = 31;
GitHubLabelCreateResult label_create = 32;
GitHubIssueLabelChangeResult issue_label_change = 33;
```

- [ ] **Step 4: Generate Go code and lint the schema**

Run:

```bash
./scripts/generate.sh
go tool buf lint
```

Expected: both commands exit 0.

- [ ] **Step 5: Run the protocol test and generated-file check**

Run:

```bash
go test ./internal/provider/github -run TestPatchmillProtocolSurface -count=1
./scripts/check-generated.sh
```

Expected: the test passes and the generated-file check exits 0 with no diff.

- [ ] **Step 6: Commit the protocol slice**

```bash
git add proto/repowolf/v1/github.proto gen/repowolf/v1/github.pb.go gen/repowolf/v1/github_grpc.pb.go internal/provider/github/protocol_patchmill_test.go
git commit -m "feat(protocol): add Patchmill GitHub operations"
```

### Task 2: Add local version, identity, and repository command parsing

**Files:**
- Create: `internal/client/github/identity.go`
- Create: `internal/client/github/repository.go`
- Create: `internal/client/github/patchmill_parse_test.go`
- Create: `internal/client/github/json_shape.go`
- Modify: `internal/client/github/parse.go:18-190`
- Modify: `internal/client/github/flags.go:89-120`
- Modify: `internal/client/github/client.go:16-64`
- Modify: `internal/client/github/operation.go:13-168`
- Modify: `internal/client/github/render.go:15-127`
- Modify: `internal/client/github/client_test.go`

**Interfaces:**
- Consumes: generated `GitHubCurrentUserRequest`, `GitHubCurrentUserResult`, and repository records.
- Produces: exact parsing for `auth status`, `api user --jq .login`, and positional repository view.

- [ ] **Step 1: Add failing parser tests for identity and repository commands**

Cover these cases in `patchmill_parse_test.go`:

```go
func newGitHubRepository(t *testing.T) string {
	t.Helper()
	cwd := t.TempDir()
	if output, err := exec.Command("git", "init", "--quiet", cwd).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	config := "[remote \"origin\"]\n\turl = git@github.com:owner/repo.git\n"
	if err := os.WriteFile(filepath.Join(cwd, ".git", "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return cwd
}

func TestParsePatchmillIdentityCommands(t *testing.T) {
	cwd := newGitHubRepository(t)
	for _, test := range []struct {
		name string
		args []string
		kind operationKind
	}{
		{"auth status", []string{"auth", "status"}, operationAuthStatus},
		{"current login", []string{"api", "user", "--jq", ".login"}, operationCurrentUserLogin},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseArgs(test.args, cwd)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.kind != test.kind || parsed.request.GetCurrentUser() == nil {
				t.Fatalf("unexpected parse: %#v", parsed)
			}
		})
	}
}
```

Add a repository case for:

```text
gh repo view owner/name --json name,url,sshUrl
```

Assert that the selector contains `owner` and `name`, and that selected fields preserve the requested order.

Add negative cases for `api repos/x`, another JQ expression, auth flags, extra positional values, and positional slug plus `--repo`.

- [ ] **Step 2: Run the new parser tests and observe rejection**

Run:

```bash
go test ./internal/client/github -run 'TestParsePatchmillIdentityCommands|TestParsePatchmillRepositoryView' -count=1
```

Expected: FAIL because the parser does not recognize `auth`, `api`, or the positional repository slug.

- [ ] **Step 3: Add focused identity and repository parsers**

Implement closed parsers with these interfaces:

```go
func parseAuth(args []string) (any, parsedFlags, operationKind, error)
func parseAPI(args []string) (any, parsedFlags, operationKind, error)
func parseRepository(args []string) (any, parsedFlags, operationKind, error)
```

`parseAuth` accepts only `[]string{"status"}`. `parseAPI` accepts only `[]string{"user", "--jq", ".login"}`.

Move the existing repository parser from `parse.go` into `repository.go`. Accept zero or one positional slug after `view`.

If a positional slug exists, place it in `flags.values["--repo"]` after repository validation. Reject a simultaneous `--repo` flag.

Add `operationAuthStatus` and `operationCurrentUserLogin`. Both kinds assign the same `GitHubRequest_CurrentUser` operation.

- [ ] **Step 4: Add failing local-version tests**

Add tests that clear RepoWolf client environment values and call `Run` directly:

```go
func TestRunVersionIsLocal(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Run(context.Background(), []string{"--version"}, &stdout, &stderr)
	if status != 0 || stdout.String() != "gh version repowolf\n" || stderr.Len() != 0 {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
}

func TestRunRejectsVersionArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Run(context.Background(), []string{"--version", "extra"}, &stdout, &stderr)
	if status != 2 {
		t.Fatalf("status=%d, want 2", status)
	}
}
```

Run:

```bash
go test ./internal/client/github -run 'TestRunVersion' -count=1
```

Expected: FAIL because `Run` enters working-directory and parser handling.

- [ ] **Step 5: Handle exact version before all other client work**

Add this first branch to `Run`, before `os.Getwd()`:

```go
if len(args) == 1 && args[0] == "--version" {
	if err := writeExact(stdout, []byte("gh version repowolf\n")); err != nil {
		writeDiagnostic(stderr, "gh: GitHub operation failed\n")
		return 1
	}
	return 0
}
```

All other `--version` shapes continue into parsing and return status 2.

- [ ] **Step 6: Add failing response and rendering tests**

Construct a current-user response with login `octocat`. Assert these exact outputs:

```text
Logged in to github.com as octocat
```

for `auth status`, and:

```text
octocat
```

for `api user --jq .login`.

Construct a repository record with `Repository: "repowolf"`, `Url`, and `SshUrl`. Assert:

```json
{"name":"repowolf","sshUrl":"git@github.com:owner/repowolf.git","url":"https://github.com/owner/repowolf"}
```

- [ ] **Step 7: Implement response selection, aliases, and output routing**

Extend `normalizedResult` and `responseMatches` so both identity kinds require `response.GetCurrentUser()`.

In repository JSON shaping, map the existing `repository` value to the requested `name` key. Preserve `sshUrl` from the new protobuf field.

Add native render cases:

```go
case operationAuthStatus:
	fmt.Fprintf(&output, "Logged in to github.com as %s\n", cell(object["login"]))
case operationCurrentUserLogin:
	fmt.Fprintln(&output, cell(object["login"]))
```

In `Run`, select `stderr` as the success writer only for `operationAuthStatus`. Keep all other successful output on `stdout`.

- [ ] **Step 8: Run the client slice and full client package**

Run:

```bash
gofmt -w internal/client/github
go test ./internal/client/github -run 'TestParsePatchmillIdentityCommands|TestParsePatchmillRepositoryView|TestRunVersion|TestPatchmillIdentityRendering|TestPatchmillRepositoryRendering' -count=1
go test ./internal/client/github -count=1
```

Expected: all commands exit 0 and `gofmt` changes no files after the first pass.

- [ ] **Step 9: Commit the identity and repository slice**

```bash
git add internal/client/github
git commit -m "feat(github): parse Patchmill identity commands"
```

### Task 3: Read pull-request body files safely

**Files:**
- Create: `internal/client/github/body_file.go`
- Create: `internal/client/github/body_file_test.go`
- Modify: `internal/client/github/parse.go:59-96`
- Modify: `internal/client/github/pull.go:11-108`

**Interfaces:**
- Consumes: `parseArgs(args []string, cwd string)` and `GitHubPullEditRequest.Body`.
- Produces: `readBodyFile(cwd, path string) (string, error)` with no-follow, type, size, and UTF-8 checks.

- [ ] **Step 1: Add failing tests for relative and absolute regular files**

Use `Parse` and assert that only file content reaches the request:

```go
func TestParsePullEditBodyFile(t *testing.T) {
	dir := newGitHubRepository(t)
	path := filepath.Join(dir, "body.md")
	if err := os.WriteFile(path, []byte("new body\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := Parse([]string{"pr", "edit", "18", "--body-file", "body.md"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	edit := request.GetPullEdit()
	if edit == nil || edit.Body == nil || *edit.Body != "new body\n" {
		t.Fatalf("unexpected body: %#v", edit)
	}
	if strings.Contains(request.String(), path) {
		t.Fatalf("request leaked path %q", path)
	}
}
```

Repeat with the absolute path and an empty regular file.

- [ ] **Step 2: Add failing tests for unsafe inputs**

Add table cases for:

- 65,537 bytes.
- Invalid UTF-8 bytes `{0xff, 0xfe}`.
- A symbolic link to a regular file.
- A directory.
- A FIFO created with `unix.Mkfifo`.
- A missing file.
- A mode-000 unreadable file when the effective user is not root.
- Duplicate `--body-file`.
- `--body` combined with `--body-file`.
- A missing flag value.

Every case must return a parse error without content in a request.

- [ ] **Step 3: Run the body-file tests and observe parser rejection**

Run:

```bash
go test ./internal/client/github -run 'TestParsePullEditBodyFile|TestParsePullEditBodyFileRejects' -count=1
```

Expected: FAIL because `--body-file` is unsupported.

- [ ] **Step 4: Implement the no-follow file reader**

Use the existing `golang.org/x/sys/unix` dependency:

```go
const maximumBodyFileBytes = 65_536

func readBodyFile(cwd, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("body file path is required")
	}
	resolved := path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(cwd, resolved)
	}
	fd, err := unix.Open(resolved, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("open body file")
	}
	file := os.NewFile(uintptr(fd), resolved)
	if file == nil {
		_ = unix.Close(fd)
		return "", fmt.Errorf("open body file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("body file must be regular")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumBodyFileBytes+1))
	if err != nil || len(raw) > maximumBodyFileBytes || !utf8.Valid(raw) {
		return "", fmt.Errorf("invalid body file")
	}
	return string(raw), nil
}
```

Do not include `resolved` or the provider error text in client diagnostics.

- [ ] **Step 5: Thread the caller working directory into pull editing**

Change the internal parser interfaces to:

```go
func parsePull(args []string, cwd string) (any, parsedFlags, operationKind, error)
func parsePullEdit(args []string, cwd string) (any, parsedFlags, operationKind, error)
```

Allow `--body-file` beside existing `--title`, `--body`, and `--base`. Reject the inline/file conflict before reading.

After validation, set `GitHubPullEditRequest.Body` to the loaded string. Do not add a protobuf path field.

- [ ] **Step 6: Run focused and package tests**

Run:

```bash
gofmt -w internal/client/github/body_file.go internal/client/github/body_file_test.go internal/client/github/parse.go internal/client/github/pull.go
go test ./internal/client/github -run 'TestParsePullEditBodyFile|TestParsePullEditBodyFileRejects' -count=1
go test ./internal/client/github -count=1
```

Expected: all tests pass.

- [ ] **Step 7: Commit the secure file-input slice**

```bash
git add internal/client/github/body_file.go internal/client/github/body_file_test.go internal/client/github/parse.go internal/client/github/pull.go
git commit -m "feat(github): read pull request body files safely"
```

### Task 4: Parse and render Patchmill issue and label commands

**Files:**
- Create: `internal/client/github/label.go`
- Modify: `internal/client/github/parse.go:18-190`
- Modify: `internal/client/github/flags.go:63-120`
- Modify: `internal/client/github/issue.go:10-136`
- Modify: `internal/client/github/operation.go:13-168`
- Modify: `internal/client/github/render.go:15-127`
- Modify: `internal/client/github/json_shape.go`
- Modify: `internal/client/github/patchmill_parse_test.go`
- Modify: `internal/client/github/fuzz_test.go`

**Interfaces:**
- Consumes: the four new protobuf operations and existing issue-create records.
- Produces: closed label grammar, bounded list limits, `include_comments`, label CSV conversion, and Patchmill JSON shapes.

- [ ] **Step 1: Add failing exact-command parser tests**

Add table cases for the complete Patchmill grammar. Use `newGitHubRepository(t)` as the caller working directory:

```go
var patchmillCommands = [][]string{
	{"issue", "list", "--state", "open", "--limit", "1001", "--json", "number,title,body,state,labels,author,createdAt,updatedAt,url"},
	{"issue", "view", "18", "--json", "number,title,body,state,labels,author,createdAt,updatedAt,url,comments"},
	{"label", "list", "--limit", "1000", "--json", "name"},
	{"label", "list", "--repo", "owner/name", "--limit", "1000", "--json", "name"},
	{"label", "create", "patchmill:ready", "--repo", "owner/name", "--color", "1a2B3c", "--description", "Ready for work"},
	{"issue", "edit", "18", "--add-label", "patchmill:ready,help wanted", "--remove-label", "patchmill:queued"},
	{"issue", "create", "--repo", "owner/name", "--title", "Title", "--body", "Body", "--label", "patchmill:ready,bug"},
	{"issue", "comment", "18", "--body", "Comment"},
	{"pr", "view", "18", "--json", "body,url"},
}
```

Assert operation type, list limit, repository selector, CSV values, selected-field order, and `IncludeComments`.

- [ ] **Step 2: Add failing strict-rejection tests**

Cover these invalid forms:

- Limit 1,002 for issues and 1,001 for labels.
- Unknown and duplicate JSON fields.
- Empty CSV items from `a,,b`, `,a`, and `a,`.
- Duplicate labels after trimming.
- The same label in add and remove lists.
- Label flags mixed with issue `--title` or `--body`.
- Label create without a name, color, or description.
- A color with `#`, five digits, seven digits, or non-hex characters.
- A label name above 50 Unicode code points.
- A description above 100 Unicode code points.
- Extra positional values.
- Deferred `repo create`, `repo delete`, and `repo clone` forms.
- Other `api`, GraphQL, template, JQ, extension, alias, and shell forms.

- [ ] **Step 3: Run the parser tests and observe failures**

Run:

```bash
go test ./internal/client/github -run 'TestParsePatchmillCommands|TestRejectPatchmillNearMisses' -count=1
```

Expected: FAIL on the newly approved commands.

- [ ] **Step 4: Add bounded list and CSV helpers**

Replace the single fixed list validator with:

```go
func listLimit(value string, maximum uint64) (uint64, error) {
	limit, err := positiveDecimal(value)
	if err != nil || limit > maximum {
		return 0, fmt.Errorf("limit must be between 1 and %d", maximum)
	}
	return limit, nil
}
```

Call it with 1,001 for issue lists and 1,000 for label lists. Use 100 for existing pull and run lists.

Implement CSV conversion in `label.go`:

```go
func labelCSV(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	if len(parts) > 100 {
		return nil, fmt.Errorf("too many labels")
	}
	seen := make(map[string]struct{}, len(parts))
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		label := strings.Trim(part, " \t")
		if label == "" || len(label) > 255 || !utf8.ValidString(label) || strings.IndexByte(label, 0) >= 0 {
			return nil, fmt.Errorf("invalid label list")
		}
		if _, duplicate := seen[label]; duplicate {
			return nil, fmt.Errorf("duplicate label")
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	return labels, nil
}
```

Reuse existing 100-label and 255-byte per-label provider bounds for issue labels. Apply the stricter 50-code-point rule only to label creation.

- [ ] **Step 5: Implement label and issue parsing**

Add:

```go
func parseLabel(args []string) (any, parsedFlags, operationKind, error)
func parseLabelList(args []string) (any, parsedFlags, operationKind, error)
func parseLabelCreate(args []string) (any, parsedFlags, operationKind, error)
```

For issue edits, return `GitHubRequest_IssueLabelChange` when either label-change flag exists. Keep title/body edits on `GitHubRequest_IssueEdit`.

For issue creation, parse `--label` into `GitHubIssueCreateRequest.Labels`.

Require explicit `--limit` and exact `--json name` for label lists. Reject a missing flag, another field set, or native label-list output.

After `selectedFields` succeeds, set:

```go
if view := request.GetIssueView(); view != nil {
	view.IncludeComments = slices.Contains(fields, "comments")
}
```

Add `body` to issue-list fields, `comments` to issue-view fields, and `name` to label-list fields.

- [ ] **Step 6: Add failing Patchmill JSON-shape tests**

Build typed issue responses and assert that selected JSON renders authors and labels as GitHub CLI objects:

```json
{"author":{"login":"octocat"},"labels":[{"name":"bug"}]}
```

For issue-view comments, assert:

```json
{"comments":[{"author":{"login":"reviewer"},"body":"done","createdAt":"2026-09-07T00:00:00Z"}]}
```

For label list, assert:

```json
[{"name":"bug"},{"name":"patchmill:ready"}]
```

- [ ] **Step 7: Implement response selection and JSON shaping**

Extend `normalizedResult` and `responseMatches` for label list, label create, and issue-label change.

Render issue-label changes through the existing native issue-edit field view. Return no native output for label creation.

Add a pure JSON-shaping function in `json_shape.go`, called only before selected JSON rendering:

```go
func githubJSONShape(kind operationKind, value any) any
```

For issue records, convert author strings to `{login: value}` and label strings to `{name: value}`.

Rebuild each nested comment with only `author`, `body`, and `createdAt`. Convert the nested author string to `{login: value}`.

Keep native table and field rendering on the existing normalized scalar values. Return no native output for label creation.

- [ ] **Step 8: Add fuzz seeds and run all client tests**

Add the exact issue-list, issue-view, label-list, label-create, and issue-label-change forms to `fuzz_test.go`.

Run:

```bash
gofmt -w internal/client/github
go test ./internal/client/github -run 'TestParsePatchmillCommands|TestRejectPatchmillNearMisses|TestPatchmillJSONRendering' -count=1
go test ./internal/client/github -count=1
```

Expected: all tests pass.

- [ ] **Step 9: Commit the command-grammar slice**

```bash
git add internal/client/github
git commit -m "feat(github): parse Patchmill issue and label commands"
```

### Task 5: Enforce server policy, audit, and provider validation

**Files:**
- Create: `internal/provider/github/validate_patchmill.go`
- Create: `internal/provider/github/validation_patchmill_test.go`
- Modify: `internal/provider/github/validate.go:18-271`
- Modify: `internal/provider/github/operation.go:13-91`
- Modify: `internal/provider/github/request_boundaries_test.go`
- Modify: `internal/provider/github/inert_test.go`
- Create: `internal/server/github_patchmill_test.go`
- Modify: `internal/server/github_test.go`

**Interfaces:**
- Consumes: typed requests from Tasks 1 and 4.
- Produces: side-effect-free validation, one capability per operation, and canonical audit names.

- [ ] **Step 1: Add failing capability and operation-name tests**

Use a table with these exact mappings:

```go
var patchmillOperations = []struct {
	request    *repowolfv1.GitHubRequest
	capability config.Capability
	name       string
}{
	{&repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_CurrentUser{CurrentUser: &repowolfv1.GitHubCurrentUserRequest{}}}, config.RepositoryRead, "github.current_user"},
	{&repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelList{LabelList: &repowolfv1.GitHubLabelListRequest{Limit: 1000}}}, config.IssuesRead, "github.label_list"},
	{&repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: "patchmill:ready", Color: "1a2b3c", Description: "Ready"}}}, config.IssuesWrite, "github.label_create"},
	{&repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: &repowolfv1.GitHubIssueLabelChangeRequest{Number: 18, AddLabels: []string{"patchmill:ready"}}}}, config.IssuesWrite, "github.issue_label_change"},
}
```

Run:

```bash
go test ./internal/provider/github -run TestPatchmillOperationMappings -count=1
```

Expected: FAIL with `ErrInvalidRequest` for each new operation.

- [ ] **Step 2: Implement capability and audit mappings**

Add the four operations to both switches in `operation.go`. Do not change existing mappings.

- [ ] **Step 3: Add failing direct-request validation tests**

Cover nil messages, zero numbers, issue limit 1,002, label limit 1,001, and empty change sets.

Cover overlapping changes, duplicate changes, more than 100 labels, invalid UTF-8, and NUL bytes.

Cover label-create names of 0 and 51 code points. Reject NUL and control characters in names.

Cover descriptions of 101 code points, invalid UTF-8 or NUL descriptions, and colors outside `[0-9A-Fa-f]{6}`.

- [ ] **Step 4: Implement focused provider validation**

Add these functions in `validate_patchmill.go`:

```go
func validateCurrentUser(*repowolfv1.GitHubCurrentUserRequest) error
func validateLabelList(*repowolfv1.GitHubLabelListRequest) error
func validateLabelCreate(*repowolfv1.GitHubLabelCreateRequest) error
func validateIssueLabelChange(*repowolfv1.GitHubIssueLabelChangeRequest) error
func labelCreateName(string) error
func labelDescription(string) error
func labelColor(string) error
func distinctLabels([]string, map[string]struct{}) error
```

Use `utf8.RuneCountInString` for the 50 and 100 code-point limits. Reject control characters in names and NUL in descriptions.

Reuse `labels` for issue-label list size and byte validation.

Change the shared limit helper to `limit(value, maximum uint64)`. Use 1,001 only for issue lists and 100 for existing pull and run lists.

`ValidateGitHubRequest` delegates new cases to the focused functions.

- [ ] **Step 5: Prove invalid typed requests are inert**

Extend `inert_test.go` so each malformed new request returns an error before `Caller.Call` receives a command.

Run:

```bash
gofmt -w internal/provider/github internal/server
go test ./internal/provider/github -run 'TestPatchmillOperationMappings|TestPatchmillRequestValidation|TestInvalidPatchmillRequestsAreInert' -count=1
```

Expected: all tests pass and the fake caller records zero calls for invalid input.

- [ ] **Step 6: Add failing server authorization and audit tests**

For each new operation, use the server fixture to assert:

- The approved capability reaches `policy.Resolve`.
- A missing capability returns the existing denied error.
- An accepted request writes the canonical audit name before provider execution.
- Provider errors remain generic to the client.
- A response above 1 MiB returns `runner.ErrOutputLimit`.

- [ ] **Step 7: Run server tests after wiring the mappings**

Run:

```bash
go test ./internal/server -run 'TestGitHubPatchmillPolicyAndAudit|TestGitHubPatchmillResponseLimit' -count=1
go test ./internal/server -count=1
```

Expected: all tests pass. The main server flow needs no new command-specific branch.

- [ ] **Step 8: Commit the validation and authorization slice**

```bash
git add internal/provider/github/validate.go internal/provider/github/validate_patchmill.go internal/provider/github/validation_patchmill_test.go internal/provider/github/operation.go internal/provider/github/request_boundaries_test.go internal/provider/github/inert_test.go internal/server
git commit -m "feat(github): authorize Patchmill operations"
```

### Task 6: Execute and normalize fixed single-call operations

**Files:**
- Create: `internal/provider/github/normalize_patchmill.go`
- Create: `internal/provider/github/single_call_patchmill_test.go`
- Modify: `internal/provider/github/command.go:24-172`
- Modify: `internal/provider/github/adapter.go:45-94`
- Modify: `internal/provider/github/normalize.go:12-205`
- Modify: `internal/provider/github/normalize_records.go:5-31`
- Modify: `internal/provider/github/normalize_response.go`
- Modify: `internal/provider/github/command_contract_test.go`
- Modify: `internal/provider/github/normalization_contract_test.go`

**Interfaces:**
- Consumes: validated `current_user`, `label_create`, and `repository_view` requests.
- Produces: fixed provider commands and minimal typed user, label, and SSH URL data.

- [ ] **Step 1: Add failing fixed-command contract tests**

Assert exact child commands for:

```text
GET /user
GET /repos/owner/name
POST /repos/owner/name/labels
```

The label-create stdin must equal:

```json
{"color":"1a2B3c","description":"Ready for work","name":"patchmill:ready"}
```

Assert that no untrusted value enters the method, endpoint, headers, executable path, or argument structure.

- [ ] **Step 2: Run command-contract tests and observe unsupported operations**

Run:

```bash
go test ./internal/provider/github -run TestPatchmillSingleCallCommandContract -count=1
```

Expected: FAIL because current-user and label-create planning return `ErrInvalidRequest`.

- [ ] **Step 3: Add fixed command plans**

Extend `plan` with:

```go
case *repowolfv1.GitHubRequest_CurrentUser:
	method, endpoint, normalizer = "GET", "/user", "current_user"
case *repowolfv1.GitHubRequest_LabelCreate:
	method, endpoint, normalizer = "POST", base+"/labels", "label_create"
	input = map[string]any{
		"name": operation.LabelCreate.Name,
		"color": operation.LabelCreate.Color,
		"description": operation.LabelCreate.Description,
	}
```

Extend `apiRepository` with this field, then require and copy it in `repositoryRecord`:

```go
SSHURL *string `json:"ssh_url"`
```

Set `current_user` provider output to 1 MiB. Set `label_create` provider output to the 4 MiB mutation limit.

- [ ] **Step 4: Add failing normalization tests**

Use these provider payloads:

```json
{"login":"octocat"}
{"name":"patchmill:ready"}
{"name":"repowolf","owner":{"login":"owner"},"full_name":"owner/repowolf","private":false,"html_url":"https://github.com/owner/repowolf","ssh_url":"git@github.com:owner/repowolf.git","default_branch":"main"}
```

Assert exact protobuf record fields. Add malformed cases for missing login, missing label name, missing SSH URL, wrong types, trailing JSON, and oversized final responses.

- [ ] **Step 5: Implement minimal normalizers**

Add these functions in `normalize_patchmill.go`:

```go
func currentUserResponse(apiUser) (*repowolfv1.GitHubResponse, error)
func labelRecord(apiLabel) (*repowolfv1.GitHubLabelRecord, error)
func labelCreateResponse(apiLabel) (*repowolfv1.GitHubResponse, error)
```

Add `current_user` and `label_create` branches to `normalize`. Keep `decode` strict about trailing data.

- [ ] **Step 6: Run focused and full provider tests**

Run:

```bash
gofmt -w internal/provider/github
go test ./internal/provider/github -run 'TestPatchmillSingleCallCommandContract|TestPatchmillSingleCallNormalization' -count=1
go test ./internal/provider/github -count=1
```

Expected: all tests pass.

- [ ] **Step 7: Commit the single-call provider slice**

```bash
git add internal/provider/github
git commit -m "feat(github): execute Patchmill identity and label calls"
```

### Task 7: Add bounded GraphQL issue-list pagination

**Files:**
- Create: `internal/provider/github/budget.go`
- Create: `internal/provider/github/issue_list.go`
- Create: `internal/provider/github/issue_list_test.go`
- Modify: `internal/provider/github/adapter.go:45-94`
- Modify: `internal/provider/github/command.go:24-92`
- Modify: `internal/provider/github/normalize_patchmill.go`
- Modify: `internal/provider/github/aggregate_budget_test.go`
- Modify: `internal/provider/github/command_contract_test.go`

**Interfaces:**
- Consumes: `GitHubIssueListRequest{State, Limit}` and `Adapter.call`.
- Produces: `executeIssueList(context.Context, policy.ResolvedRepository, *GitHubRequest) (*GitHubResponse, error)`.
- Produces: an internal aggregate-output budget shared by later paginated and mutation modules.

- [ ] **Step 1: Add failing tests for page sizing and oldest-first order**

Use a scripted caller that returns ten 100-node pages and one one-node page. Assert:

- Calls 1 through 10 use `first: 100`.
- Call 11 uses `first: 1`.
- The fixed query includes `orderBy: {field: CREATED_AT, direction: ASC}`.
- The query selects only number, title, body, state, labels, author, timestamps, and URL. It requests at most 100 labels per issue.
- The result preserves the 1,001-node order.

Use a payload shape equivalent to:

```json
{"data":{"repository":{"issues":{"nodes":[{"number":1,"title":"First","body":"Body","state":"OPEN","labels":{"nodes":[{"name":"bug"}]},"author":{"login":"octocat"},"createdAt":"2026-09-01T00:00:00Z","updatedAt":"2026-09-02T00:00:00Z","url":"https://github.com/owner/name/issues/1"}],"pageInfo":{"hasNextPage":false,"endCursor":"cursor-1"}}}}}
```

- [ ] **Step 2: Add failing metadata, cancellation, and budget tests**

Cover missing `data`, repository, issues, nodes, page info, author, label names, and required scalar fields. Reject an issue label connection that reports another page.

Cover `hasNextPage: true` with an empty cursor, repeated cursors, empty nodes with another page, and more nodes than requested.

Return raw page bytes totaling exactly 8 MiB and assert success when the final protobuf stays below 1 MiB. Add one byte and assert `runner.ErrOutputLimit`.

Cancel the context after page one and assert no page-two call.

- [ ] **Step 3: Run the issue-list tests and observe the old REST search plan**

Run:

```bash
go test ./internal/provider/github -run 'TestIssueListGraphQLPagination|TestIssueListRejectsPaginationMetadata|TestIssueListAggregateBudget|TestIssueListStopsOnCancellation' -count=1
```

Expected: FAIL because issue lists still use one REST search request.

- [ ] **Step 4: Implement the aggregate-output budget**

Keep this module private and small:

```go
const maximumPaginatedReadBytes = 8 * miB

type aggregateBudget struct {
	limit int
	used  int
}

func (budget *aggregateBudget) remaining() (int, error) {
	if budget == nil || budget.used >= budget.limit {
		return 0, runner.ErrOutputLimit
	}
	return budget.limit - budget.used, nil
}

func (budget *aggregateBudget) consume(raw []byte) error {
	if budget == nil || len(raw) > budget.limit-budget.used {
		return runner.ErrOutputLimit
	}
	budget.used += len(raw)
	return nil
}
```

Add this internal adapter method:

```go
func (adapter *Adapter) callBudgeted(
	ctx context.Context,
	command runner.Command,
	budget *aggregateBudget,
) (runner.Result, error)
```

It sets `StdoutLimit` to the remaining bytes, calls `adapter.call`, and consumes the returned stdout.

- [ ] **Step 5: Implement the fixed GraphQL query and normalizer**

Define the query as a constant in `issue_list.go`. Supply only typed variables for owner, repository name, state list, cursor, and first count.

Use `states: null` for the existing `all` state. Use `OPEN` or `CLOSED` for the other states.

Add private GraphQL response structs with pointer fields. Reject nonempty GraphQL `errors` and all missing required data.

Add this page-call interface:

```go
func (adapter *Adapter) callIssueGraphQLPage(
	ctx context.Context,
	repository policy.ResolvedRepository,
	state repowolfv1.GitHubIssueState,
	cursor *string,
	first int,
	budget *aggregateBudget,
) ([]byte, error)
```

Normalize GraphQL names into existing protobuf fields. Set assignees to an empty list because Patchmill does not request them.

Query labels as `labels(first: 100) { nodes { name } pageInfo { hasNextPage } }`. Reject `hasNextPage: true` for this nested connection.

- [ ] **Step 6: Implement the bounded page loop**

Use this loop contract:

```go
for len(records) < int(request.Limit) {
	first := min(100, int(request.Limit)-len(records))
	raw, err := adapter.callIssueGraphQLPage(ctx, repository, state, cursor, first, budget)
	// Strictly decode and append at most first nodes.
	// If hasNextPage is false, return the accumulated result.
	// If hasNextPage is true, require a new nonempty endCursor.
}
```

The 1,001 limit naturally produces ten 100-record pages and one one-record page. Do not fetch another page after the requested limit.

Dispatch all `IssueList` requests to `executeIssueList` from `Adapter.Execute`. Remove the old REST search branch from `plan`.

- [ ] **Step 7: Run focused and full provider tests**

Run:

```bash
gofmt -w internal/provider/github/budget.go internal/provider/github/issue_list.go internal/provider/github/issue_list_test.go internal/provider/github/adapter.go internal/provider/github/command.go internal/provider/github/normalize_patchmill.go
go test ./internal/provider/github -run 'TestIssueListGraphQLPagination|TestIssueListRejectsPaginationMetadata|TestIssueListAggregateBudget|TestIssueListStopsOnCancellation|TestPatchmillSingleCallCommandContract' -count=1
go test ./internal/provider/github -count=1
```

Expected: all tests pass and old command-contract tests show no REST issue-search path.

- [ ] **Step 8: Commit the issue-list slice**

```bash
git add internal/provider/github
git commit -m "feat(github): paginate Patchmill issue lists"
```

### Task 8: Fetch issue comments only when requested

**Files:**
- Create: `internal/provider/github/issue_comments.go`
- Create: `internal/provider/github/issue_comments_test.go`
- Modify: `internal/provider/github/adapter.go:45-94`
- Modify: `internal/provider/github/command.go:24-92`
- Modify: `internal/provider/github/normalize_records.go:32-114`
- Modify: `internal/provider/github/normalize_response.go`
- Modify: `internal/provider/github/aggregate_budget_test.go`
- Modify: `internal/provider/github/normalization_contract_test.go`

**Interfaces:**
- Consumes: `GitHubIssueViewRequest{Number, IncludeComments}` and `aggregateBudget`.
- Produces: `executeIssueView(context.Context, policy.ResolvedRepository, *GitHubRequest) (*GitHubResponse, error)`.

- [ ] **Step 1: Add failing no-comments and short-page tests**

For `IncludeComments: false`, assert exactly one fixed issue request and no comment request.

For `IncludeComments: true`, return one issue and one short comment page. Assert that the issue response contains normalized comments.

Assert that a REST comment payload maps only typed fields and later renders `author`, `body`, and `createdAt` for Patchmill.

- [ ] **Step 2: Add failing boundary and error tests**

Cover:

- Ten full pages with exactly 1,000 comments and an empty one-record overflow probe.
- A nonempty overflow probe, which returns `runner.ErrOutputLimit`.
- A short page before page ten, which stops without a probe.
- A pull-request marker in the issue preflight.
- Malformed issue or comment JSON.
- Missing comment author, body, ID, URL, creation time, or update time.
- Aggregate raw output above 8 MiB across issue and comment calls.
- Cancellation before a later comment page.

- [ ] **Step 3: Run the comment tests and observe missing pagination**

Run:

```bash
go test ./internal/provider/github -run 'TestIssueViewComments|TestIssueViewCommentOverflow|TestIssueViewCommentBudget|TestIssueViewCommentsStopOnCancellation' -count=1
```

Expected: FAIL because issue view does not request or attach comments.

- [ ] **Step 4: Implement one issue-view execution path**

Move issue-view execution out of the generic planner and into:

```go
func (adapter *Adapter) executeIssueView(
	ctx context.Context,
	repository policy.ResolvedRepository,
	request *repowolfv1.GitHubRequest,
) (*repowolfv1.GitHubResponse, error)
```

Use an 8 MiB budget when comments are requested. Fetch the issue, reject pull requests, normalize it, and return immediately when `IncludeComments` is false.

- [ ] **Step 5: Implement bounded comment pagination**

Request pages 1 through 10 with `per_page=100`. Stop after a page with fewer than 100 comments.

After ten full pages, request page 1001 with `per_page=1` (REST offset `(page-1)*per_page = 1000`). This is the eleventh comment call, in addition to the issue preflight. Return `runner.ErrOutputLimit` when that probe contains a comment.

Attach at most 1,000 `GitHubCommentRecord` values to `GitHubIssueRecord.Comments`. Keep the operation under the original context.

- [ ] **Step 6: Run focused and full provider tests**

Run:

```bash
gofmt -w internal/provider/github/issue_comments.go internal/provider/github/issue_comments_test.go internal/provider/github/adapter.go internal/provider/github/command.go internal/provider/github/normalize_records.go internal/provider/github/normalize_response.go
go test ./internal/provider/github -run 'TestIssueViewComments|TestIssueViewCommentOverflow|TestIssueViewCommentBudget|TestIssueViewCommentsStopOnCancellation' -count=1
go test ./internal/provider/github -count=1
```

Expected: all tests pass.

- [ ] **Step 7: Commit the comment-pagination slice**

```bash
git add internal/provider/github
git commit -m "feat(github): fetch bounded issue comments"
```

### Task 9: List labels through bounded REST pagination

**Files:**
- Create: `internal/provider/github/label_list.go`
- Create: `internal/provider/github/label_list_test.go`
- Modify: `internal/provider/github/adapter.go:45-94`
- Modify: `internal/provider/github/command_contract_test.go`
- Modify: `internal/provider/github/aggregate_budget_test.go`

**Interfaces:**
- Consumes: `GitHubLabelListRequest{Limit}` and `aggregateBudget`.
- Produces: `executeLabelList(context.Context, policy.ResolvedRepository, *GitHubRequest) (*GitHubResponse, error)`.

- [ ] **Step 1: Add failing page and output tests**

Script ten pages of 100 labels. Assert exact fixed endpoints with `page=N&per_page=100` and an 8 MiB aggregate budget.

Assert that a short page without a next relation stops. Assert that page ten without a next relation returns exactly 1,000 labels.

Add a selected-JSON client assertion for the final typed label list.

- [ ] **Step 2: Add failing pagination-metadata tests**

Provider commands for label pages must include response headers in stdout through fixed `gh api --include` use.

Cover malformed HTTP status lines, malformed headers, conflicting `Link` headers, and invalid link relations.

Cover a short body with `rel="next"`. Also cover page ten with `rel="next"`.

Page ten with a next relation must return `runner.ErrOutputLimit` without an eleventh provider call.

- [ ] **Step 3: Run the label-list tests and observe unsupported execution**

Run:

```bash
go test ./internal/provider/github -run 'TestLabelListPagination|TestLabelListRejectsPaginationMetadata|TestLabelListOverflow|TestLabelListAggregateBudget' -count=1
```

Expected: FAIL because `LabelList` is not dispatched.

- [ ] **Step 4: Implement strict included-response parsing**

Keep header parsing private to `label_list.go`:

```go
type includedPage struct {
	body    []byte
	hasNext bool
}

func decodeIncludedPage(raw []byte, expectedPage int) (includedPage, error)
```

Accept one successful GitHub HTTP response, case-insensitive header names, and zero or one valid `Link` header. Treat an absent next relation as terminal.

Do not follow the URL from the header. Validate its page relation, then build the next fixed repository endpoint locally.

- [ ] **Step 5: Implement the ten-page loop**

Fetch at most `min(100, remainingRecords)` labels per page. Normalize only nonempty label names.

Reject more records than requested. Reject `hasNext` after the requested limit or page ten.

Dispatch `LabelList` to `executeLabelList` from `Adapter.Execute`.

- [ ] **Step 6: Run focused and full provider tests**

Run:

```bash
gofmt -w internal/provider/github/label_list.go internal/provider/github/label_list_test.go internal/provider/github/adapter.go
go test ./internal/provider/github -run 'TestLabelListPagination|TestLabelListRejectsPaginationMetadata|TestLabelListOverflow|TestLabelListAggregateBudget' -count=1
go test ./internal/provider/github -count=1
```

Expected: all tests pass.

- [ ] **Step 7: Commit the label-list slice**

```bash
git add internal/provider/github
git commit -m "feat(github): paginate repository labels"
```

### Task 10: Apply issue-label changes through one typed update

**Files:**
- Create: `internal/provider/github/issue_labels.go`
- Create: `internal/provider/github/issue_labels_test.go`
- Modify: `internal/provider/github/adapter.go:45-94`
- Modify: `internal/provider/github/adapter_special.go:15-73`
- Modify: `internal/provider/github/normalize_response.go`
- Modify: `internal/provider/github/aggregate_budget_test.go`
- Modify: `internal/provider/github/command_contract_test.go`

**Interfaces:**
- Consumes: validated `GitHubIssueLabelChangeRequest` and a 4 MiB `aggregateBudget`.
- Produces: `executeIssueLabelChange(context.Context, policy.ResolvedRepository, *GitHubRequest) (*GitHubResponse, error)`.

- [ ] **Step 1: Add failing preflight and mutation contract tests**

Given current labels `bug` and `patchmill:queued`, plus this request:

```go
&repowolfv1.GitHubIssueLabelChangeRequest{
	Number:       18,
	AddLabels:    []string{"patchmill:ready", "bug"},
	RemoveLabels: []string{"patchmill:queued"},
}
```

Assert exactly two fixed calls:

```text
GET /repos/owner/name/issues/18
PATCH /repos/owner/name/issues/18
```

Assert that the PATCH body is:

```json
{"labels":["bug","patchmill:ready"]}
```

Assert that no add/remove values become endpoint or command fragments.

- [ ] **Step 2: Add failing safety, budget, and cancellation tests**

Cover a pull-request marker in preflight, duplicate provider labels, malformed labels, and more than 100 computed labels.

Cover malformed update output. Make sure that no mutation follows a failed preflight.

Use preflight and mutation output totaling exactly 4 MiB for a boundary case. Add one byte and assert `runner.ErrOutputLimit`.

Cancel after preflight and assert that the PATCH does not execute.

- [ ] **Step 3: Run the issue-label tests and observe unsupported execution**

Run:

```bash
go test ./internal/provider/github -run 'TestIssueLabelChange|TestIssueLabelChangeRejectsPullRequest|TestIssueLabelChangeAggregateBudget|TestIssueLabelChangeStopsOnCancellation' -count=1
```

Expected: FAIL because the adapter does not dispatch `IssueLabelChange`.

- [ ] **Step 4: Implement deterministic label-set computation**

Use a pure helper:

```go
func changedLabels(current, add, remove []string) ([]string, error)
```

Preserve current-label order. Remove requested labels, then append new labels in request order.

Keep each label once. Reject duplicate current labels and a final set above 100 labels.

- [ ] **Step 5: Implement preflight and one mutation**

Create a 4 MiB budget. Fetch the issue through the fixed issue endpoint and reject pull requests.

Decode and validate the complete current label set. Compute the replacement set.

Before mutation, honor `ctx.Err()`. Then execute one PATCH with `{"labels": computed}`.

Normalize the PATCH response as `GitHubIssueLabelChangeResult`. Do not route this operation through the generic issue-edit preflight.

- [ ] **Step 6: Run focused and full provider tests**

Run:

```bash
gofmt -w internal/provider/github/issue_labels.go internal/provider/github/issue_labels_test.go internal/provider/github/adapter.go internal/provider/github/adapter_special.go internal/provider/github/normalize_response.go
go test ./internal/provider/github -run 'TestIssueLabelChange|TestIssueLabelChangeRejectsPullRequest|TestIssueLabelChangeAggregateBudget|TestIssueLabelChangeStopsOnCancellation' -count=1
go test ./internal/provider/github -count=1
```

Expected: all tests pass.

- [ ] **Step 7: Commit the issue-label mutation slice**

```bash
git add internal/provider/github
git commit -m "feat(github): apply typed issue label changes"
```

### Task 11: Document the exact compatibility and security contract

**Files:**
- Create: `docs/github-cli-compatibility.md`
- Modify: `README.md:131-136`
- Modify: `docs/configuration.md`
- Modify: `docs/specs/2026-09-07-patchmill-gh-compatibility-design.md`

**Interfaces:**
- Consumes: the implemented command, capability, and limit behavior.
- Produces: one user-facing command matrix and security reference linked from the README.

- [ ] **Step 1: Write the compatibility reference**

Document each accepted Patchmill form exactly as listed in the approved spec. Add a table with command, repository source, capability, and output form.

Document these bounds:

```text
argv: 64 arguments and 64 KiB
body file: 65,536 bytes, regular file, valid UTF-8, no symlink leaf
issue list: 1,001 records, 11 pages, 8 MiB aggregate provider output
issue comments: 1,000 records plus overflow detection, 8 MiB aggregate provider output
label list: 1,000 records, 10 pages, 8 MiB aggregate provider output
mutations: 4 MiB aggregate provider output
normalized and rendered response: 1 MiB
```

State that RepoWolf rejects generic `gh api`, GraphQL, JQ, templates, aliases, extensions, and shell passthrough.

- [ ] **Step 2: Link the reference and capability guidance**

Add `GitHub CLI compatibility` under README `Learn more`.

In `docs/configuration.md`, link each GitHub capability to the command families it permits. Do not change configuration behavior.

Set the approved spec status to `Implemented` only after all focused behavior tests pass. Until then, keep its current `Approved` status.

- [ ] **Step 3: Apply direct documentation checks**

The Testing Value Gate excludes tests that assert static Markdown content. Use direct checks instead:

```bash
git diff --check
rg -n "gh --version|gh api user --jq \.login|gh label list|gh pr edit.*--body-file" docs/github-cli-compatibility.md
rg -n "GitHub CLI compatibility" README.md
```

Expected: no whitespace errors and every required reference appears.

- [ ] **Step 4: Commit the documentation slice**

```bash
git add README.md docs/configuration.md docs/github-cli-compatibility.md docs/specs/2026-09-07-patchmill-gh-compatibility-design.md
git commit -m "docs(github): document restricted Patchmill commands"
```

### Task 12: Run adversarial review and complete verification

**Files:**
- Modify only files required to resolve verified review findings.
- Review: all changes from base `143be90` through `HEAD`.

**Interfaces:**
- Consumes: the complete issue #18 implementation and approved design.
- Produces: reviewed code, fresh verification evidence, and a clean issue branch except for `.analysis/`.

- [ ] **Step 1: Run the full local verification set before review**

Run each command separately and retain its exit status:

```bash
go tool buf lint
./scripts/check-generated.sh
test -z "$(gofmt -l .)"
go test ./internal/client/github ./internal/provider/github ./internal/server -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
git diff --check 143be90..HEAD
```

Expected: every command exits 0. `gofmt -l` and `git diff --check` produce no output.

- [ ] **Step 2: Request adversarial code review**

Dispatch the canonical Pi `reviewer` with fresh context. Include:

- Issue #18 and the approved spec path.
- Base SHA `143be90` and current head SHA.
- The full diff.
- The exact supported and deferred command surface.
- File-input, pagination, aggregate-budget, capability, audit, and diagnostic requirements.
- A request for concrete findings only, ordered by severity, with file and line references.

Do not dispatch the disabled `code-reviewer` shim.

- [ ] **Step 3: Evaluate every review finding before editing**

For each finding:

1. Reproduce or inspect the claimed behavior.
2. Compare it with the approved spec.
3. Reject findings that conflict with the contract or lack evidence.
4. For a valid behavior defect, write a failing regression test first.
5. Apply the smallest fix.
6. Run the focused test and its package tests.

Commit valid fixes with a concise Conventional Commits subject. Do not combine unrelated findings in one commit.

- [ ] **Step 4: Attempt Patchmill doctor through the RepoWolf shim**

If a runnable RepoWolf broker, repository policy, and credentials exist, run:

```bash
tmp="$(mktemp -d)"
go build -o "$tmp/gh" ./cmd/repowolf-client
PATH="$tmp:$PATH" patchmill doctor
rm -rf "$tmp"
```

Expected: `gh --version` and `gh auth status` pass through the shim. Record any later failure with its exact deferred command.

If required broker configuration or credentials do not exist, record the missing prerequisite. Do not claim an integration pass.

- [ ] **Step 5: Re-run the complete verification set after review fixes**

Run fresh commands:

```bash
go tool buf lint
./scripts/check-generated.sh
test -z "$(gofmt -l .)"
go test ./internal/client/github ./internal/provider/github ./internal/server -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
git diff --check 143be90..HEAD
git status --short --branch
```

Expected: all verification commands exit 0. The status shows only the intentionally untracked `.analysis/` directory.

- [ ] **Step 6: Prepare the completion report**

Report:

- Branch and worktree.
- Base and head SHAs.
- Typed operations and accepted commands.
- Security and resource bounds.
- Review findings and resolutions.
- Exact verification commands and results.
- Patchmill doctor result or the precise missing prerequisite.
- Deferred command surface.
- The untracked `.analysis/` state.

Do not merge, push, or remove the worktree without user instruction.
