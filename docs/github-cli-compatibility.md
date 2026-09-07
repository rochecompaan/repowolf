# GitHub CLI compatibility

RepoWolf provides a restricted `gh` client for approved command forms. This
reference lists the GitHub CLI commands that RepoWolf accepts.

## Supported command matrix

Use the command forms in this table exactly. Brackets mark optional values.
Commands that use a repository hint get it from the caller working directory.

| Command | Repository source | Capability | Output form |
| --- | --- | --- | --- |
| `gh --version` | None | None | `gh version repowolf` on standard output |
| `gh auth status` | Repository hint | `repository:read` | Login status on standard error |
| `gh api user --jq .login` | Repository hint | `repository:read` | Login and newline on standard output |
| `gh issue list --state open --limit 1001 --json number,title,body,state,labels,author,createdAt,updatedAt,url` | Repository hint | `issues:read` | JSON array and newline |
| `gh issue view <number> --json number,title,body,state,labels,author,createdAt,updatedAt,url,comments` | Repository hint | `issues:read` | JSON object and newline |
| `gh label list [--repo owner/name] --limit 1000 --json name` | Repository hint or `--repo` | `issues:read` | JSON array and newline |
| `gh label create <name> [--repo owner/name] --color <color> --description <text>` | Repository hint or `--repo` | `issues:write` | No standard output |
| `gh issue edit <number> [--add-label <csv>] [--remove-label <csv>]` | Repository hint | `issues:write` | Native issue output |
| `gh issue create [existing flags] [--label <csv>]` | Repository hint | `issues:write` | Native issue output |
| `gh issue comment <number> --body <text>` | Repository hint | `issues:write` | Comment URL and newline |
| `gh repo view owner/name --json name,url,sshUrl` | Positional repository slug | `repository:read` | JSON object and newline |
| `gh pr view <number> --json body,url` | Repository hint | `pull_requests:read` | JSON object and newline |
| `gh pr edit <number> --body-file <path>` | Repository hint | `pull_requests:write` | Native pull-request output |

RepoWolf accepts flags in any order after required positional values. It rejects
duplicate flags, `--flag=value` forms, unsupported flags, fields, subcommands,
and extra positional values. Parse errors exit with status 2.

## Security contract

RepoWolf rejects generic `gh api`, GraphQL, JQ, templates, aliases, extensions,
and shell passthrough. Only `gh api user --jq .login` is accepted from the API
command family.

Remote commands use a configured repository policy. A repository hint, a
`--repo` value, or a positional slug cannot select an unconfigured provider
target.

The `--body-file` command reads the file in the client process. The path does
not enter the request. RepoWolf opens the leaf without following a symbolic
link and accepts only a regular, valid UTF-8 file. An empty regular file is
valid and clears the pull-request body.

## Resource bounds

```text
argv: 64 arguments and 64 KiB
body file: 65,536 bytes, regular file, valid UTF-8, no symlink leaf
issue list: 1,001 records, 11 pages, 8 MiB aggregate provider output
issue comments: 1,000 records plus overflow detection, 8 MiB aggregate provider output
label list: 1,000 records, 10 pages, 8 MiB aggregate provider output
mutations: 4 MiB aggregate provider output
normalized and rendered response: 1 MiB
```

RepoWolf fails closed when an input, limit, response, or pagination record is
invalid. Provider commands use fixed methods, endpoints, headers, and query
documents. Client values cannot provide provider command text or shell
fragments.
