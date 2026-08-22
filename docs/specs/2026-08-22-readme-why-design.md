# README “Why RepoWolf” Design

**Date:** 2026-08-22  
**Status:** Approved

## Goal

Explain why RepoWolf is safer and easier to operate than common alternatives. The section must make three points:

1. Most developers give a coding agent the same SSH key that they use for Git. This access creates a large blast radius.
2. Fine-grained access tokens reduce access, but they are difficult to create and manage for many repositories.
3. RepoWolf keeps credentials outside the sandbox and applies a local policy for each sandbox.

## Placement

Add `## Why RepoWolf` after the README introduction and before `## How it works`. This position explains the need before the README explains the architecture.

## Copy

Use three short paragraphs in this order:

> Most developers give a coding agent the same SSH key that they use for Git. That key can reach every repository the developer can access. If the agent or sandbox is compromised, every reachable repository is at risk.
>
> Fine-grained access tokens reduce this blast radius, but they are a pain to create and manage. Each repository needs a token, scopes, and an expiration date. Teams must distribute, renew, and revoke these tokens as access changes.
>
> RepoWolf keeps the developer’s SSH key and provider credentials outside the agent sandbox. A local YAML policy grants each sandbox access to only the repositories and actions it needs. The agent gets scoped access without receiving the underlying credentials.

## Diagram

Copy the supplied PNG to `docs/assets/why-repowolf.png`. Apply lossless PNG optimization, but do not crop, resize, or change its content.

Place the diagram after the three paragraphs. Use this alt text:

> Comparison of SSH access, fine-grained tokens, and RepoWolf policy enforcement

The prose contains the important information from the diagram. Readers do not need the image to understand the section.

## Scope

Change only these product files:

- `README.md`
- `docs/assets/why-repowolf.png`

This change does not alter application behavior, configuration, workflows, or release behavior.

## Verification

Do not add an automated test for this static documentation change. Use direct verification:

- Make sure that the PNG is valid and retains its `1536 × 1024` dimensions.
- Make sure that every local README image and link resolves.
- Make sure that Markdown fences are balanced.
- Make sure that `Why RepoWolf` appears before `How it works`.
- Run `git diff --check`.
- Review the final diff and repository status.
