---
title: gofmt -l always exits 0 - gate CI chains with test -z
tags:
    - ci
    - go
agent: polymatx
created: 2026-08-04T15:23:44.865681+03:00
---

gofmt -l exits 0 whether or not it lists unformatted files, so `gofmt -l . && next-step` does NOT gate on formatting. Gate with: test -z "$(gofmt -l .)". This broke CI twice in this repo before the lesson stuck.
