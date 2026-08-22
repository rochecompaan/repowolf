# README “Why RepoWolf” Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a clear README rationale that explains SSH-key blast radius, fine-grained-token toil, and RepoWolf’s scoped policy model.

**Architecture:** Add one descriptive section near the start of `README.md`, followed by one optimized PNG. Keep the important content in text so that the rationale remains accessible when the image is unavailable.

**Tech Stack:** Markdown, PNG, Python 3, Nix, oxipng, ImageMagick, Git

## Global Constraints

- Place `## Why RepoWolf` after the introduction and before `## How it works`.
- State that most developers give a coding agent the same SSH key that they use for Git.
- Explain the blast radius and the management burden of fine-grained access tokens.
- Explain that RepoWolf keeps credentials outside the sandbox and applies a local YAML policy.
- Store the diagram at `docs/assets/why-repowolf.png`.
- Optimize the PNG without changing its pixels or its `1536 × 1024` dimensions.
- Use descriptive alt text, and keep all important diagram information in the prose.
- Do not add an automated test for this static documentation change. Use direct verification.

---

### Task 1: Add the README rationale and diagram

**Files:**
- Modify: `README.md:9-18`
- Create: `docs/assets/why-repowolf.png`
- Reference: `docs/specs/2026-08-22-readme-why-design.md`

**Interfaces:**
- Consumes: the supplied PNG at `/tmp/pi-clipboard-c575ce67-72ca-4411-a72f-6fbba801be5a.png` and the approved copy in the design spec
- Produces: a self-contained `Why RepoWolf` section and its local image asset

- [ ] **Step 1: Make sure that the source image and baseline are valid**

Run:

```bash
test -f /tmp/pi-clipboard-c575ce67-72ca-4411-a72f-6fbba801be5a.png
file /tmp/pi-clipboard-c575ce67-72ca-4411-a72f-6fbba801be5a.png
git status --short
```

Expected: `file` reports a `1536 x 1024` RGBA PNG. The repository has no uncommitted changes.

- [ ] **Step 2: Copy and losslessly optimize the diagram**

Run:

```bash
install -m 0644 \
  /tmp/pi-clipboard-c575ce67-72ca-4411-a72f-6fbba801be5a.png \
  docs/assets/why-repowolf.png

before_bytes=$(stat -c %s /tmp/pi-clipboard-c575ce67-72ca-4411-a72f-6fbba801be5a.png)
nix shell nixpkgs#oxipng -c oxipng -o 4 --strip safe docs/assets/why-repowolf.png
after_bytes=$(stat -c %s docs/assets/why-repowolf.png)
test "$after_bytes" -le "$before_bytes"

magick compare -metric AE \
  /tmp/pi-clipboard-c575ce67-72ca-4411-a72f-6fbba801be5a.png \
  docs/assets/why-repowolf.png \
  null: 2>/tmp/repowolf-why-pixel-diff

test "$(awk '{print $1}' /tmp/repowolf-why-pixel-diff)" = "0"
rm -f /tmp/repowolf-why-pixel-diff
```

Expected: oxipng completes successfully, the output is not larger than the source, and the first ImageMagick result token is `0`.

- [ ] **Step 3: Add the approved README section**

Insert this content after the introductory sentence `RepoWolf does not create, inspect, register, or attest sandboxes.` and before `## How it works`:

```markdown
## Why RepoWolf

Most developers give a coding agent the same SSH key that they use for Git.
That key can reach every repository the developer can access. If the agent or
sandbox is compromised, every reachable repository is at risk.

Fine-grained access tokens reduce this blast radius, but they are a pain to
create and manage. Each repository needs a token, scopes, and an expiration
date. Teams must distribute, renew, and revoke these tokens as access changes.

RepoWolf keeps the developer’s SSH key and provider credentials outside the
agent sandbox. A local YAML policy grants each sandbox access to only the
repositories and actions it needs. The agent gets scoped access without
receiving the underlying credentials.

![Comparison of SSH access, fine-grained tokens, and RepoWolf policy enforcement](docs/assets/why-repowolf.png)
```

- [ ] **Step 4: Run direct documentation and image verification**

Run:

```bash
python3 - <<'PY'
from pathlib import Path
import re
import struct

readme = Path('README.md')
text = readme.read_text()
image = Path('docs/assets/why-repowolf.png')
data = image.read_bytes()

assert data[:8] == b'\x89PNG\r\n\x1a\n'
assert struct.unpack('>II', data[16:24]) == (1536, 1024)
assert text.count('```') % 2 == 0
assert text.index('## Why RepoWolf') < text.index('## How it works')
assert 'Most developers give a coding agent the same SSH key that they use for Git.' in text
assert 'Fine-grained access tokens reduce this blast radius' in text
assert 'A local YAML policy grants each sandbox access' in text
assert '![Comparison of SSH access, fine-grained tokens, and RepoWolf policy enforcement](docs/assets/why-repowolf.png)' in text

targets = re.findall(r'\[[^]]*\]\(([^)]+)\)', text)
targets += re.findall(r'<img[^>]+src="([^"]+)"', text)
for target in targets:
    local = target.split('#', 1)[0]
    if not local or '://' in local or local.startswith('mailto:'):
        continue
    resolved = (readme.parent / local).resolve()
    assert resolved.exists(), f'missing README target: {target}'

print('README rationale, image, links, fences, and section order are valid')
PY
```

Expected: `README rationale, image, links, fences, and section order are valid`.

- [ ] **Step 5: Review the change and whitespace**

Run:

```bash
git diff --check
git diff --stat
git status --short
```

Expected: no whitespace errors. Only `README.md` and `docs/assets/why-repowolf.png` are uncommitted.

- [ ] **Step 6: Commit the implementation**

Run:

```bash
git add README.md docs/assets/why-repowolf.png
git commit -m "docs(readme): explain RepoWolf rationale"
```

Expected: one commit containing the README section and optimized diagram.

- [ ] **Step 7: Repeat the direct verification on the committed tree**

Repeat the Python command from Step 4. Then run:

```bash
git diff --check HEAD^ HEAD
git status --short
git log -1 --format='%h %s'
```

Expected: direct verification passes, the worktree is clean, and the latest commit is `docs(readme): explain RepoWolf rationale`.

## Delivery

After Task 1 passes review, push the branch and verify PR #4:

```bash
git push origin docs/readability-ux
/home/roche/.nix-profile/bin/gh pr view 4 \
  --repo rochecompaan/repowolf \
  --json state,url,headRefName,headRefOid \
  --jq '"STATE=\(.state) HEAD=\(.headRefName) OID=\(.headRefOid) URL=\(.url)"'
```

Expected: PR #4 remains open and its head OID matches the local branch HEAD.
