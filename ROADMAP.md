# go-SOMEIP Roadmap

## Vision

go-SOMEIP is a modern, Go-native SOME/IP library for automotive service-oriented communication.

The project focuses on:

- Correct AUTOSAR SOME/IP wire protocol implementation
- Modern developer experience with zero-dependency mock
- Safety-oriented design (go-FuSa integration)
- Pure Go — no CGo, no external C libraries
- Automotive Ethernet and zonal architecture deployments

go-SOMEIP does not replicate every AUTOSAR SOME/IP feature. It targets the subset
needed for practical automotive middleware development in Go.

Protocol correctness matters.
Developer productivity matters.
API simplicity matters.
AUTOSAR spec completeness does not.

---

## Guiding Principles

1. Pure Go first — no CGo, no system libraries required
2. Standards where they provide value (AUTOSAR PRS_SOMEIPProtocol)
3. Simplicity over completeness
4. Testability by default — zero-dependency mock transport
5. Safety as a first-class concern (go-FuSa)
6. Bridges welcome — `bridge/dds`, `bridge/mqtt`, etc. live in this repo
7. Automotive Ethernet as a primary deployment target

---

## Release Plan

| Version | Packages | Theme |
|---|---|---|
| v0.1.0 | `someip`, `codec`, `mock`, `sd`, `udp`, Docker | Foundation — core interface, wire codec, mock, SD, UDP, quickstart |
| v0.2.0 | `tcp` | TCP transport — reliable unicast with session management |
| v0.3.0 | `sd/udp` | SOME/IP-SD over UDP — real service discovery with periodic offer refresh |
| v0.4.0 | `tp` | SOME/IP-TP — segmented transport for large payloads (>1400 bytes) |
| v0.5.0 | `e2e` | E2E protection — CRC and counter headers (AUTOSAR E2E Profile 01/05) |
| v0.6.0 | `bridge/dds` | Bidirectional SOME/IP ↔ DDS bridge (go-DDS) |
| v0.7.0 | `bridge/mqtt` | Bidirectional SOME/IP ↔ MQTT bridge (go-mqtt) |
| v0.8.0 | `bridge/grpc` | SOME/IP ↔ gRPC bridge — method calls as Unary RPCs, events as server-streaming |
| v0.9.0 | `bridge/rest` | SOME/IP ↔ REST/SSE bridge — HTTP endpoints for methods, SSE stream for events |
| v1.0.0 | — | Stable API, full test coverage, production-ready |

---

## Milestone Details

### v0.1.0 — Foundation

- [x] Core interface (`someip.go`): types, errors, `Service`, `Server`, `Subscription`
- [x] Wire codec (`codec/`): `Encode`/`Decode`, fuzz targets
- [x] Mock transport (`mock/`): in-process `Bus`, `Server`, `Service`
- [x] SOME/IP-SD (`sd/`): entry codec, IPv4 option, in-process `Registry`
- [x] UDP transport (`udp/`): `Server` and `Service` over UDP unicast
- [x] Docker quickstart: server + client images, docker-compose

### v0.2.0 — TCP Transport

- [x] `tcp/`: TCP `Server` and `Service` with session management
- [ ] Connection lifecycle: connect, keepalive, reconnect backoff
- [ ] Concurrent call multiplexing over a single TCP connection

### v0.3.0 — SOME/IP-SD over UDP

- [x] `sd/udp/`: periodic OfferService announcements, FindService with response
- [ ] Subscribe/SubscribeAck eventgroup management
- [ ] TTL refresh timers, stop-offering on shutdown

### v0.4.0 — SOME/IP-TP

- [x] `tp/`: segmentation and reassembly for payloads > MTU
- [ ] TP header bit (bit 5 of MessageType)
- [ ] Reassembly timeout and partial-frame cleanup

### v0.5.0 — E2E Protection

- [x] `e2e/`: E2E Profile 01 (CRC-8) and Profile 05 (CRC-32)
- [ ] Counter-based freshness
- [ ] Integration with `Server.Emit` and `Service.Call`

### v0.6.0 — DDS Bridge

- [ ] `bridge/dds/`: bidirectional SOME/IP ↔ DDS bridge using go-DDS
- [ ] Method calls forwarded as DDS RPC; events forwarded as DDS samples
- [ ] Topic/serviceID mapping configuration

### v0.7.0 — MQTT Bridge

- [ ] `bridge/mqtt/`: bidirectional SOME/IP ↔ MQTT bridge using go-mqtt
- [ ] Event notifications forwarded to MQTT topics; MQTT → SOME/IP request mapping
- [ ] QoS and topic prefix configuration

### v0.8.0 — gRPC Bridge

- [ ] `bridge/grpc/`: SOME/IP ↔ gRPC bridge
- [ ] SOME/IP method calls mapped to gRPC Unary RPCs
- [ ] SOME/IP event streams mapped to gRPC server-streaming RPCs
- [ ] Protobuf payload pass-through; serviceID/methodID → service/method reflection

### v0.9.0 — REST/SSE Bridge

- [ ] `bridge/rest/`: SOME/IP ↔ REST bridge
- [ ] HTTP POST endpoint per SOME/IP method (path `/services/{svcID}/{methodID}`)
- [ ] SSE stream for SOME/IP event notifications (`GET /events/{svcID}/{eventID}`)
- [ ] JSON payload encoding; configurable port and path prefix
