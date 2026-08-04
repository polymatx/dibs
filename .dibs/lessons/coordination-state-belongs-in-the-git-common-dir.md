---
title: coordination state belongs in the git common dir
tags:
    - architecture
agent: polymatx
created: 2026-08-04T05:21:16.105823+03:00
---

Coordination state must live in the git COMMON dir (git rev-parse --git-common-dir), not .git of the worktree. Worktrees each have their own .git file pointing at the shared common dir - storing state there is what makes leases visible across all worktrees instantly with zero commits.
