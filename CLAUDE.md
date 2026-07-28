# go-SOMEIP — CLAUDE.md

## Module

```
github.com/SoundMatt/go-SOMEIP
```

Go 1.25+, MPL-2.0, Copyright © 2026 Matt Jones.

## Repo layout

```
someip.go                   # Core interface: types, errors, Service, Server, Subscription
someip_test.go
adapt.go                    # RELAY adapter module (spec §13.7.1): Adapt, ToMessage, FromMessage
cmd/go-someip/               # RELAY-conformant CLI (spec §11, §12): version, capabilities,
                             # status, convert, send
codec/                      # SOME/IP wire-frame encode/decode
  codec.go
  codec_test.go
  fuzz_test.go
mock/                       # In-process transport (no network)
  mock.go
  mock_test.go
  fuzz_test.go
sd/                         # SOME/IP-SD Service Discovery
  sd.go
  sd_test.go
  udp/                      # Real-socket SOME/IP-SD daemon (multicast offer/find/subscribe)
udp/                         # UDP transport (unreliable unicast)
  udp.go
  udp_test.go               # build tag: integration
tcp/                         # TCP transport (reliable unicast, session management)
tp/                           # SOME/IP-TP segmentation and reassembly
e2e/                          # AUTOSAR E2E protection (Profile 01, Profile 05)
examples/
  quickstart/
    server/main.go
    client/main.go
docker/
  Dockerfile                # multi-stage: server + client images
  docker-compose.yml
.github/
  workflows/
    ci.yml
    dco.yml
    docker-publish.yml
    release.yml
```

## Build

```bash
go build ./...
go vet ./...
go test -race ./...
go test -tags integration -race ./udp/...
```

## CI overview

| Job | Trigger | Notes |
|---|---|---|
| test | push/PR | Cross-platform matrix (ubuntu/macos/windows, Go 1.25/1.26) |
| benchmark-smoke | push/PR | 1 iteration each benchmark in mock/ |
| fuzz-short | push/PR | 10 s per fuzz target (codec, mock) |
| test-udp | push/PR | integration tag; skips if udp/ absent |
| generate | push/PR | go generate + git diff --exit-code |
| lint | push/PR | golangci-lint v2.12.2 |
| gofusa | push/PR | go-FuSa v0.36.0 full lifecycle: check, 100% requirement trace, cyber, vuln, qualify (RELAY spec §20.1.2) |
| relay-conform | push/PR | `relay conform --strict` + `relay interop` against RELAY spec v1.11 |
| dco | PRs only | Signed-off-by on every commit |
| docker-publish | push main/tags | multi-arch amd64/arm64 to GHCR |
| release | vX.Y.Z tag | Regenerate go-FuSa artifacts (fmea, tara, vuln, qualify-report, safety-case, sbom, provenance) |

## Design constraints

- Bridge packages (`bridge/dds`, `bridge/mqtt`, etc.) live in this repo, like go-DDS.
- No CGo — pure Go only.
- All exported types carry `//fusa:req` annotations.
- License header on every .go file.
- DCO `Signed-off-by` on every commit.
