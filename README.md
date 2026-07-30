# go-SOMEIP

A generic Go library for [SOME/IP](https://www.autosar.org/standards/foundation) (Scalable service-Oriented MiddlewarE over IP). Works in automotive Ethernet stacks, zonal architectures, simulation, and testing.

The API is a stable Go interface. Implementations are swappable without changing application code.

[![CI](https://github.com/SoundMatt/go-SOMEIP/actions/workflows/ci.yml/badge.svg)](https://github.com/SoundMatt/go-SOMEIP/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/SoundMatt/go-SOMEIP.svg)](https://pkg.go.dev/github.com/SoundMatt/go-SOMEIP)

## Packages

| Package | Description | Requires |
|---|---|---|
| `mock` | In-process transport. Zero dependencies. Default for development and testing. | Nothing |
| `codec` | SOME/IP wire-frame serialization and deserialization. | Nothing |
| `sd` | SOME/IP Service Discovery (SOME/IP-SD): entry/IPv4-option codec, in-process `Registry`. | Nothing |
| `sd/udp` | Real-socket SOME/IP-SD daemon (multicast offer/find/subscribe). | Nothing |
| `udp` | Pure-Go UDP transport — unreliable unicast and multicast. | Nothing |
| `tcp` | Pure-Go TCP transport — reliable unicast with session management. | Nothing |
| `tp` | SOME/IP-TP segmentation and reassembly for payloads > MTU. | Nothing |
| `e2e` | AUTOSAR E2E protection (Profile 01 CRC-8, Profile 05 CRC-32). | Nothing |
| `cmd/go-someip` | RELAY-conformant CLI: `version`, `capabilities`, `status`, `convert`, `send`. | Nothing |

## RELAY conformance

go-SOMEIP is a RELAY-conformant port (RELAY spec v2.0; see `SpecVersion` in
`someip.go`). The RELAY adapter lives in `adapt.go` (spec §13.7.1):

- `Adapt(Service) relay.Caller` — wraps a native [`Service`] as a
  protocol-agnostic `relay.Caller`/`Node` (§10.3). `Subscribe` reads
  `relay.WithEventID` to select the SOME/IP event group and honours the
  `relay.BackPressurePolicy` (§10.5).
- `Message.ToMessage()` / `FromMessage(relay.Message)` — lossless conversion
  between a native [`Message`] and the universal `relay.Message` envelope
  (§15.7.6).

### Meta keys

| Key | Meaning |
|---|---|
| `someip.client_id` | 16-bit Client ID |
| `someip.session_id` | 16-bit Session ID |
| `someip.msg_type` | numeric `MessageType` (round-trip) |
| `someip.msg_type_name` | human-readable message type (diagnostic; ignored on decode) |
| `someip.return_code` | 8-bit Return Code |
| `someip.interface_version` | 8-bit Interface Version |

### CLI

```bash
go-someip version        # tool + spec version (JSON)
go-someip capabilities   # declared RELAY capabilities (JSON)
go-someip status         # health/status
go-someip convert        # frame ↔ relay.Message conversion
go-someip send           # NDJSON message sink
```


## Install

```bash
go get github.com/SoundMatt/go-SOMEIP
```

## Quick start

```go
import (
    "context"

    someip "github.com/SoundMatt/go-SOMEIP"
    "github.com/SoundMatt/go-SOMEIP/mock"
)

bus := mock.NewBus()

// Server side
srv, _ := bus.NewServer(someip.ServiceID(0x1234), someip.InstanceID(0x0001))
srv.RegisterMethod(someip.MethodID(0x0001), func(ctx context.Context, req someip.Message) ([]byte, error) {
    return []byte("pong"), nil
})

// Client side
svc, _ := bus.NewService(someip.ServiceID(0x1234), someip.InstanceID(0x0001))
resp, _ := svc.Call(context.Background(), someip.MethodID(0x0001), []byte("ping"))
fmt.Println(string(resp.Payload)) // pong
```

## Docker quickstart

```bash
docker compose -f docker/docker-compose.yml up
```

See [docker/](docker/) for the full quickstart configuration.

## Switching implementations

```go
// Development / tests — no network needed:
import "github.com/SoundMatt/go-SOMEIP/mock"
bus := mock.NewBus()
srv, _ := bus.NewServer(serviceID, instanceID)
svc, _ := bus.NewService(serviceID, instanceID)

// Production — pure-Go UDP:
import "github.com/SoundMatt/go-SOMEIP/udp"
srv, _ := udp.NewServer(udp.ServerConfig{...})
svc, _ := udp.NewService(udp.ServiceConfig{...})

// Production — pure-Go TCP (reliable):
import "github.com/SoundMatt/go-SOMEIP/tcp"
srv, _ := tcp.NewServer(tcp.ServerConfig{...})
svc, _ := tcp.NewService(tcp.ServiceConfig{...})
```

## Related projects

| Project | Role |
|---|---|
| [go-DDS](https://github.com/SoundMatt/go-DDS) | Data-centric pub/sub (DDSI-RTPS) |
| [go-mqtt](https://github.com/SoundMatt/go-mqtt) | IoT pub/sub (MQTT v3.1.1) |
| [go-FuSa](https://github.com/SoundMatt/go-FuSa) | Functional safety toolkit (ISO 26262, IEC 61508) |
| [go-RCP](https://github.com/SoundMatt/go-RCP) | Remote control plane for zonal Ethernet |

## License

[Mozilla Public License v2.0](LICENSE) © 2026 Matt Jones
