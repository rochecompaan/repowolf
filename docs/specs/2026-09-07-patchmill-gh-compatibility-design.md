# Patchmill GitHub CLI compatibility design

**Date:** 2026-09-07

**Status:** Approved

## Context

RepoWolf exposes a restricted `gh` compatibility client. The client accepts approved command forms and converts them to typed protobuf requests.

Patchmill 0.20.0 uses additional `gh` command forms. RepoWolf rejects these forms during client parsing and exits with status 2.

This failure occurs before client configuration, policy authorization, or provider execution. Patchmill therefore cannot use RepoWolf as its `gh` shim.

Issue #18 requires the minimum Patchmill command surface without a generic command tunnel. The design keeps the existing fail-closed parser and provider boundary.

## Outcome

RepoWolf accepts the exact `gh` forms that Patchmill 0.20.0 needs. RepoWolf continues to reject arbitrary `gh` commands and arbitrary GitHub API requests.

The client converts each accepted command into a dedicated typed operation. The provider builds fixed GitHub requests from validated fields.

All pagination, file input, provider output, protobuf responses, and command arguments remain bounded.

## Approaches considered

### Dedicated typed operations

Add typed protobuf operations for the missing Patchmill actions. Extend existing request and response records only for required fields.

This approach preserves policy checks, audit names, validation, normalization, and bounded provider execution. This is the selected approach.

### Generic `gh api` passthrough

Forward a client-supplied endpoint, method, GraphQL query, JQ expression, or request body to the provider.

This approach supports more commands with less code. It also creates a broad network and query tunnel through RepoWolf.

The design rejects this approach.

### Patchmill-specific command rewrites outside RepoWolf

Wrap or patch Patchmill so that it uses only the current RepoWolf command set.

This approach avoids RepoWolf changes. It also makes RepoWolf incompatible with the released Patchmill command contract.

The design rejects this approach.

## Supported command surface

RepoWolf adds support for these exact command families:

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

Existing approved command forms remain supported. This change does not widen unrelated command families.

The parser accepts flags in any order after required positional values. It rejects duplicate flags and `--flag=value` forms.

The parser continues to allow at most 64 arguments and 64 KiB of argument data. Every argument must contain valid UTF-8 and no NUL byte.

Unsupported flags, fields, subcommands, and extra positional values fail during parsing. A parse failure returns exit status 2 through the existing generic diagnostic.

## Local command handling

### Version

`gh --version` is a local compatibility command. It does not read client configuration, infer a repository, open a connection, or call the provider.

The command writes `gh version repowolf\n` to standard output and exits with status 0. Any extra argument fails with status 2.

### Authentication status

`gh auth status` uses the typed `current_user` operation. The command therefore proves that configuration, RepoWolf authentication, repository authorization, and GitHub credentials work.

A successful response writes `Logged in to github.com as <login>\n` to standard error. Patchmill uses only the exit status.

### Current user login

Only `gh api user --jq .login` is accepted. The client rejects all other endpoints, methods, headers, fields, and JQ expressions.

The client maps this command to `current_user`. The provider executes fixed `GET /user` and normalizes only the `login` field.

The renderer writes the login and one trailing newline.

## Repository selection

Commands without `--repo` use the existing repository hint from the caller working directory.

`gh label list` and `gh label create` accept one optional `--repo owner/name` selector. Existing repository validation applies.

`gh repo view owner/name` accepts exactly one repository slug. The client rejects a simultaneous positional slug and `--repo` selector.

The server still resolves every remote operation through configured repository policy. Client input never selects an unconfigured provider target.

## Typed protocol changes

The `GitHubRequest` operation union adds these requests:

- `current_user`
- `label_list`
- `label_create`
- `issue_label_change`

`GitHubIssueViewRequest` adds `include_comments`. The client sets this field only when the selected JSON fields include `comments`.

The response union adds results for current-user, label-list, and label-create operations. The label-change operation returns the normalized updated issue.

The protocol adds only fields required for the approved Patchmill output:

- A user record exposes `login`.
- A label record exposes `name`.
- An issue record exposes `body`, `labels`, `author`, creation time, update time, URL, and comments.
- An issue comment exposes `body`, `author`, and creation time.
- A repository record exposes the public URL and SSH URL.

Existing protobuf field numbers do not change. New fields and union members use new field numbers.

Generated Go files remain generated artifacts. The protobuf toolchain regenerates them from `proto/repowolf/v1/github.proto`.

## Parser and rendering behavior

### JSON fields

Each operation has a closed set of JSON field names. The parser rejects unknown and duplicate field names.

The issue-list field set adds `body`. The issue-view field set adds `comments`.

The repository-view field set adds `name`, `url`, and `sshUrl`. The pull-view field set continues to support `body` and `url`.

The label-list field set contains only `name`. The current-user API form does not accept general JSON selection.

The renderer preserves the requested field order. It emits GitHub CLI field names such as `createdAt` and `sshUrl`.

Issue authors render as objects with a `login` field. Labels render as objects with a `name` field.

Issue comments render only `author`, `body`, and `createdAt`. These are the fields that Patchmill reads.

### Label CSV values

The client splits each label CSV argument on commas. It trims surrounding ASCII space from each item.

Every item must be nonempty and valid UTF-8. Duplicate items in one flag are invalid.

A label in both `--add-label` and `--remove-label` is invalid. An issue-label change must add or remove at least one label.

The client rejects label-change flags mixed with issue title or body changes. Existing typed issue edits remain separate from `issue_label_change`.

`gh issue create --label <csv>` writes the parsed labels into the existing typed issue-create request. Issue comments retain their existing body-only form.

### Label creation

`gh label create` requires one label name, one `--color`, and one `--description` value. An extra positional value is invalid.

The name must contain 1 through 50 Unicode code points. The description can contain at most 100 Unicode code points.

The color must contain exactly six hexadecimal characters without a leading `#`. Letter case is accepted without semantic change.

Missing values, duplicate flags, and malformed colors fail during parsing. The provider repeats all validation before execution.

## Secure pull-request body files

`gh pr edit <number> --body-file <path>` reads the file in the client process. The path never enters the protobuf request.

A relative path resolves from the caller working directory. An absolute path remains absolute.

The client opens the leaf with no-follow behavior. The client rejects a symbolic-link leaf, directory, device, socket, and named pipe.

The opened file descriptor must identify a regular file. This descriptor check protects the operation from a path replacement after validation.

The client reads at most 65,537 bytes. It rejects a body larger than 65,536 bytes.

The body must contain valid UTF-8. An empty regular file is valid because the operation can clear a pull-request body.

The client puts only the body text into `GitHubPullEditRequest.Body`. Provider and server behavior remains identical to an inline typed body edit.

The parser rejects duplicate `--body-file` flags. It also rejects a command that combines `--body` with `--body-file`.

A missing path, unreadable file, invalid file type, oversized file, or invalid UTF-8 file fails parsing with exit status 2.

## Provider execution

The provider accepts only validated typed requests. It builds fixed `gh api` child commands with fixed methods, endpoints, headers, and query documents.

No client value can supply a provider command, endpoint, method, GraphQL document, JQ expression, template, extension, alias, or shell fragment.

### Current user

The provider executes one fixed `GET /user` request. It accepts only a JSON object with a valid nonempty `login` string.

### Issue list

The provider uses one fixed GraphQL query for repository issues. The query requests only Patchmill fields and orders issues from oldest to newest.

Each full page requests 100 issues. Ten full pages can produce 1,000 issues.

If ten pages are full, one final request asks for one issue. The operation therefore uses at most 11 pages and returns at most 1,001 issues.

The provider checks `hasNextPage` and `endCursor` after each page. A missing cursor with another page, a repeated cursor, or contradictory metadata fails closed.

The final 1,001st issue lets Patchmill detect its safe-selection overflow condition. RepoWolf does not silently truncate this record.

### Issue view and comments

The provider first fetches the issue through the fixed repository issue endpoint. It rejects a response that represents a pull request.

If `include_comments` is false, the provider returns the issue without a comments request.

If `include_comments` is true, the provider requests REST comment pages with 100 records per page. It accepts at most 1,000 comments.

After ten full pages, the provider makes one bounded overflow request. If a 1,001st comment exists, the operation fails closed.

Each comment normalization keeps only body, author login, and creation time. Malformed records fail the operation.

### Label list

The provider requests repository labels in pages of 100. It stops after a short page or ten pages.

The provider reads fixed REST pagination metadata for each page. Missing, malformed, repeated, or contradictory pagination data fails closed.

The result contains at most 1,000 label names. If page ten reports another page, the operation fails on overflow.

### Label creation

The provider executes one fixed repository label endpoint. It sends only the validated name, six-digit color, and bounded description.

The provider normalizes only the created label name. GitHub response details do not cross the typed boundary.

### Issue-label changes

The provider preflights the issue with a fixed issue-view request. It rejects pull requests before any mutation.

The provider normalizes the current label names, applies removals, and then applies additions. The result contains each label once.

The provider performs one typed issue update with the complete computed label set. It does not send separate add and remove commands.

A malformed preflight response stops the operation before mutation. A malformed mutation response fails the complete operation.

## Output and time bounds

A paginated read has an 8 MiB aggregate provider-output budget. This budget includes every page and any overflow request.

A mutation has a 4 MiB aggregate provider-output budget. This budget includes preflight and mutation responses.

Each child call receives only the remaining aggregate budget. Exhaustion returns the existing output-limit error.

The normalized protobuf response retains the existing 1 MiB limit. Client-rendered output therefore remains bounded by typed response data.

The complete operation remains under the RPC context and configured provider timeout. Pagination does not create a new independent timeout for each page.

Cancellation and timeout stop further provider calls. Partial results never become successful responses.

## Validation and fail-closed behavior

The client validates untrusted command arguments before configuration or network access. The server and provider validate every typed request again.

The provider rejects nil records, invalid numbers, invalid UTF-8, oversized strings, invalid label data, and unsupported union members.

Pagination fails on malformed JSON, missing required fields, repeated cursors, contradictory page metadata, output exhaustion, cancellation, and timeout.

Provider details remain behind the existing generic client diagnostics. The client does not expose GitHub response bodies or provider command lines.

## Policy and audit mapping

| Typed operation | Capability | Audit operation |
|---|---|---|
| `current_user` | `repository:read` | `github.current_user` |
| `repository_view` | `repository:read` | `github.repository_view` |
| `issue_list` | `issues:read` | `github.issue_list` |
| `issue_view` | `issues:read` | `github.issue_view` |
| `label_list` | `issues:read` | `github.label_list` |
| `label_create` | `issues:write` | `github.label_create` |
| `issue_label_change` | `issues:write` | `github.issue_label_change` |
| `issue_create` | `issues:write` | `github.issue_create` |
| `issue_comment` | `issues:write` | `github.issue_comment` |
| `pull_view` | `pull_requests:read` | `github.pull_view` |
| `pull_edit` | `pull_requests:write` | `github.pull_edit` |

`gh --version` has no remote operation, capability check, or audit event.

## Error behavior

Unsupported or malformed command input returns exit status 2 and the existing invalid-command diagnostic.

Configuration, connection, authorization, provider, normalization, limit, and response errors return exit status 1 through existing generic diagnostics.

Cancellation preserves the existing interrupted status behavior. No new error path exposes credentials, provider output, or repository policy details.

## Test strategy

### Parser tests

Table tests cover every exact Patchmill command. They also cover field order, repository selectors, bounds, and typed request contents.

Negative tests cover unknown fields, unknown flags, duplicate flags, malformed CSV, label conflicts, malformed colors, and unsafe positional forms.

Issue-list tests cover the 1,001 limit. Existing limit behavior remains covered for other list operations.

### Body-file tests

Tests use relative and absolute regular files. They prove that the protobuf request contains body text and not a path.

Negative tests cover missing files, unreadable files, directories, devices or pipes, symbolic links, oversized files, and invalid UTF-8.

Tests also cover duplicate `--body-file` flags and conflicts between `--body` and `--body-file`.

### Provider contract tests

Command-contract tests prove that each typed operation produces only fixed methods, endpoints, headers, GraphQL documents, and request bodies.

Validation tests call the provider with malformed protobuf messages. These tests prove that direct RPC callers cannot bypass client validation.

Normalization tests cover required fields, GitHub naming differences, pull-request rejection, and minimal Patchmill response records.

### Pagination and budget tests

Issue-list tests cover short pages, ten full pages, the 1,001st record, repeated cursors, and inconsistent page metadata.

Comment tests cover omitted comments, short pagination, exactly 1,000 comments, and overflow at 1,001 comments.

Label tests cover short pagination and the 1,000-label cap.

Aggregate-budget tests cover success at the boundary and failure after the 8 MiB or 4 MiB budget is exhausted.

Cancellation tests prove that no later page or mutation executes after context cancellation.

### Server, rendering, and compatibility tests

Server tests cover capability selection, policy denial, audit names, response limits, and generic error mapping for every new operation.

Rendering tests compare Patchmill field names and JSON shapes. Local command tests prove that `gh --version` does not load configuration or use the network.

An integration check runs `patchmill doctor` through the RepoWolf shim when the local environment can supply the required configuration and credentials.

## Documentation updates

User-facing documentation lists the new exact command forms and their required capabilities.

The documentation states that RepoWolf does not provide general `gh api` access. It also lists the pagination and file-input bounds.

## Non-goals

This work does not support these operations:

- `gh repo create`
- `gh repo delete`
- `gh repo clone`
- Arbitrary REST or GraphQL requests
- General JQ expressions or templates
- GitHub CLI extensions or aliases
- Shell command passthrough
- Unrelated Patchmill bootstrap or run-once commands

## Acceptance criteria

- Patchmill 0.20.0 command forms in this design pass RepoWolf parsing.
- Each remote command maps to a typed request, capability, and audit name.
- The provider builds only fixed GitHub requests from validated fields.
- Issue lists return at most 1,001 ordered records through bounded GraphQL pagination.
- Issue comments return at most 1,000 records and fail on overflow.
- Label lists return at most 1,000 records.
- Aggregate provider output and final protobuf output stay within their limits.
- `--body-file` reads only a bounded regular UTF-8 file through a no-follow open.
- Unsupported commands, unsafe files, malformed input, and inconsistent provider data fail closed.
- Existing supported `gh` behavior and tests remain valid.
- Protobuf lint, generation checks, formatting, tests, vetting, and builds pass.

## Open questions

None.
