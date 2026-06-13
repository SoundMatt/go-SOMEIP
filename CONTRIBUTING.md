# Contributing to go-SOMEIP

Thank you for your interest in contributing.

## Developer Certificate of Origin (DCO)

All contributions must be signed off under the
[Developer Certificate of Origin v1.1](https://developercertificate.org).
The DCO is a lightweight way to certify that you wrote or have the right to
submit the code you are contributing.

Add a `Signed-off-by` trailer to every commit:

```
git commit -s -m "feat: add awesome thing"
```

This produces:

```
feat: add awesome thing

Signed-off-by: Your Name <your@email.com>
```

If you forget to sign off, amend the commit:

```
git commit --amend -s
```

A GitHub Actions check (`DCO`) verifies every commit in a PR. PRs without
sign-offs will not be merged.

## Copyright

By contributing you agree that your contributions are licensed under the
[Mozilla Public License v2.0](LICENSE) and that copyright in go-SOMEIP remains
with Matt Jones. The DCO sign-off transfers no copyright — it only affirms you
have the right to contribute under the existing license.

## Design principles

go-SOMEIP is the authoritative Go SOME/IP implementation. Bridge packages
(e.g. `bridge/dds`, `bridge/mqtt`) live inside this repo, following the
same pattern as go-DDS. Sibling projects that consume go-SOMEIP depend on
it, not the other way around.
