# nol8 fork of quic-go

This is nol8's fork of [quic-go](https://github.com/quic-go/quic-go), imported as
`github.com/nol8io/quic-go` by the nol8 Data Path Go SDK and by Argus. Everything below
this section is upstream's README, unchanged.

## Why it exists

Upstream caps every QUIC packet at 1452 bytes of UDP payload
(`internal/protocol.MaxPacketBufferSize`), and no setting changes that. Iris links run on
networks with larger MTUs, so this fork lets Path MTU discovery grow packets up to
8192 bytes. A Go library can't impose a `replace` directive on the applications that use
it, so the change ships as a fork with its own module path.

## What differs from upstream

Release branch `nol8/vX.Y.Z` is upstream tag `vX.Y.Z` plus exactly these changes.

1. **Packet size.** This is Argus's change (Argus `e9d6c34`), ported to the upstream
   version.
   - `MaxPacketBufferSize` is 8192 instead of 1452. This constant also sizes every
     receive buffer, pooled packet buffer and pooled STREAM frame, and sets the size
     below which `Stream.Write` copies data and returns. A lower `MaxPacketSize`
     doesn't bring back upstream's memory use or `Write` behaviour.
   - The same constant caps `InitialPacketSize`. Upstream lowers a value above 1452
     to 1452 without an error; the fork keeps values up to 8192. An application that
     set 1500 now sends 1500-byte packets, and its handshake fails on a path that
     can't carry them.
   - With GSO, a send batch holds as many packets of the current size as fit in
     20 KiB: 14 at 1452 bytes, as upstream, and 2 once packets reach 8192.
   - `Config.MaxPacketSize` sets this side's limit:
     - 0 means 8192.
     - `Listen` and `Dial` reject values outside 1200–8192. A `Config` returned by
       `GetConfigForClient` isn't validated, so its value is clamped to that range.
     - It's advertised to the peer as `max_udp_payload_size`.
     - Path MTU discovery never probes above the lower of it and the peer's value.
     - An `InitialPacketSize` above it is lowered to it, and so is the 1280-byte
       default.
     - The peer's value doesn't lower a packet size already above it: an explicit
       `InitialPacketSize`, or the 1280-byte default when the peer advertises
       1200–1279. This is upstream's behaviour.
   - `Conn.MaxUDPPayloadLimits()` reports the local, peer and effective limits, and is
     safe to call from any goroutine. The effective limit is the Path MTU discovery
     ceiling; in the two cases above, the packet size in use can be larger.
   - New tests cover the setting.
   - Upstream tests whose inputs assumed the 1452-byte ceiling now size those inputs from
     `MaxPacketBufferSize`, with every assertion kept: `send_stream_test.go`,
     `integrationtests/self/zero_rtt_test.go` and `integrationtests/self/datagram_test.go`.
   - `config_test.go` gives the new field a value in its every-field-set helper.
2. **Module path.** Upstream's `github.com/quic-go/quic-go` becomes
   `github.com/nol8io/quic-go`. It changes in every `go.mod` file and Go import, in the
   generated mocks, and in the package paths in CI and build scripts. Links to upstream
   issues, pull requests and documentation still point upstream.
3. This section of the README.

Nothing else changes. To see the full delta, compare a release branch with its upstream
tag.

## Versions

Tags are `vX.Y.Z-nol8.N`: upstream `vX.Y.Z` with nol8 revision `N`. This repository keeps
no upstream tags. Go orders `vX.Y.Z-nol8.N` before `vX.Y.Z`, so depend on an exact fork
version:

```
go get github.com/nol8io/quic-go@v0.65.0-nol8.1
```

## Ownership and security

- **Owner:** the nol8 Data Path SDK team.
- **Upstream security fixes:** applied within 7 days, released as a new `-nol8.N` tag or a
  new release branch.
- **Advisory watch:** `govulncheck` doesn't map upstream advisories to this module path.
  The owners therefore watch upstream security advisories and releases instead.
- **Reporting:** report an issue in the fork's own change to the Data Path SDK team.
  Report anything else to upstream, as described in [SECURITY.md](SECURITY.md).

## End of life

nol8 is proposing upstream that the maximum packet size become a configuration option.
Once upstream supports it, the SDK and Argus move back to upstream quic-go and this
repository is archived.

---

<div align="center" style="margin-bottom: 15px;">
  <img src="./assets/quic-go-logo.png" width="700" height="auto">
</div>

# A QUIC implementation in pure Go


[![Documentation](https://img.shields.io/badge/docs-quic--go.net-red?style=flat)](https://quic-go.net/docs/)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/quic-go/quic-go)](https://pkg.go.dev/github.com/quic-go/quic-go)
[![Code Coverage](https://img.shields.io/codecov/c/github/quic-go/quic-go/master.svg?style=flat-square)](https://codecov.io/gh/quic-go/quic-go/)
[![Fuzzing Status](https://oss-fuzz-build-logs.storage.googleapis.com/badges/quic-go.svg)](https://introspector.oss-fuzz.com/project-profile?project=quic-go)

quic-go is an implementation of the QUIC protocol ([RFC 9000](https://datatracker.ietf.org/doc/html/rfc9000), [RFC 9001](https://datatracker.ietf.org/doc/html/rfc9001), [RFC 9002](https://datatracker.ietf.org/doc/html/rfc9002)) in Go.

Other QUIC specifications:

* Unreliable Datagram Extension ([RFC 9221](https://datatracker.ietf.org/doc/html/rfc9221))
* Datagram Packetization Layer Path MTU Discovery (DPLPMTUD, [RFC 8899](https://datatracker.ietf.org/doc/html/rfc8899))
* QUIC Version 2 ([RFC 9369](https://datatracker.ietf.org/doc/html/rfc9369))
* QUIC Event Logging using qlog ([draft-ietf-quic-qlog-main-schema](https://datatracker.ietf.org/doc/draft-ietf-quic-qlog-main-schema/) and [draft-ietf-quic-qlog-quic-events](https://datatracker.ietf.org/doc/draft-ietf-quic-qlog-quic-events/))
* QUIC Stream Resets with Partial Delivery ([draft-ietf-quic-reliable-stream-reset-07](https://datatracker.ietf.org/doc/html/draft-ietf-quic-reliable-stream-reset-07) and [draft-ietf-quic-reliable-stream-reset-09](https://datatracker.ietf.org/doc/html/draft-ietf-quic-reliable-stream-reset-09))

quic-go also supports HTTP/3 ([RFC 9114](https://datatracker.ietf.org/doc/html/rfc9114)).

Other HTTP/3 specifications:

* QPACK: Field Compression for HTTP/3 ([RFC 9204](https://datatracker.ietf.org/doc/html/rfc9204))
* Extensible Prioritization Scheme for HTTP ([RFC 9218](https://datatracker.ietf.org/doc/html/rfc9218))
* HTTP Datagrams and the Capsule Protocol ([RFC 9297](https://datatracker.ietf.org/doc/html/rfc9297))

Related projects:

* [webtransport-go](https://github.com/quic-go/webtransport-go) implements WebTransport over HTTP/3 ([draft-ietf-webtrans-http3](https://datatracker.ietf.org/doc/draft-ietf-webtrans-http3/)).
* [masque-go](https://github.com/quic-go/masque-go) implements CONNECT-UDP ([RFC 9298](https://datatracker.ietf.org/doc/html/rfc9298)).
* [connect-ip-go](https://github.com/quic-go/connect-ip-go) implements CONNECT-IP ([RFC 9484](https://datatracker.ietf.org/doc/html/rfc9484)).

Detailed documentation can be found on [quic-go.net](https://quic-go.net/docs/).

## FIPS 140-3

Starting with v0.60, quic-go supports use in FIPS 140-3 environments when built with Go 1.26 or newer, using Go standard library cryptography for the QUIC code paths relevant in FIPS mode; see [FIPS140.md](FIPS140.md) for details.

## Projects using quic-go

| Project                                                   | Description                                                                                                                                                       | Stars                                                                                               |
| ---------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| [AdGuardHome](https://github.com/AdguardTeam/AdGuardHome) | Free and open source, powerful network-wide ads & trackers blocking DNS server.                                                                                   | ![GitHub Repo stars](https://img.shields.io/github/stars/AdguardTeam/AdGuardHome?style=flat-square) |
| [algernon](https://github.com/xyproto/algernon)           | Small self-contained pure-Go web server with Lua, Markdown, HTTP/2, QUIC, Redis and PostgreSQL support                                                            | ![GitHub Repo stars](https://img.shields.io/github/stars/xyproto/algernon?style=flat-square)        |
| [caddy](https://github.com/caddyserver/caddy/)            | Fast, multi-platform web server with automatic HTTPS                                                                                                              | ![GitHub Repo stars](https://img.shields.io/github/stars/caddyserver/caddy?style=flat-square)       |
| [cloudflared](https://github.com/cloudflare/cloudflared)  | A tunneling daemon that proxies traffic from the Cloudflare network to your origins                                                                               | ![GitHub Repo stars](https://img.shields.io/github/stars/cloudflare/cloudflared?style=flat-square)  |
| [frp](https://github.com/fatedier/frp)                    | A fast reverse proxy to help you expose a local server behind a NAT or firewall to the internet                                                                   | ![GitHub Repo stars](https://img.shields.io/github/stars/fatedier/frp?style=flat-square)            |
| [go-libp2p](https://github.com/libp2p/go-libp2p)          | libp2p implementation in Go, powering [Kubo](https://github.com/ipfs/kubo) (IPFS) and [Lotus](https://github.com/filecoin-project/lotus) (Filecoin), among others | ![GitHub Repo stars](https://img.shields.io/github/stars/libp2p/go-libp2p?style=flat-square)     |
| [gost](https://github.com/go-gost/gost)                   | A simple security tunnel written in Go                                                                                                                        | ![GitHub Repo stars](https://img.shields.io/github/stars/go-gost/gost?style=flat-square)            |
| [Hysteria](https://github.com/apernet/hysteria)           | A powerful, lightning fast and censorship resistant proxy                                                                                                         | ![GitHub Repo stars](https://img.shields.io/github/stars/apernet/hysteria?style=flat-square)        |
| [Mercure](https://github.com/dunglas/mercure)             | An open, easy, fast, reliable and battery-efficient solution for real-time communications                                                                         | ![GitHub Repo stars](https://img.shields.io/github/stars/dunglas/mercure?style=flat-square)         |
| [nodepass](https://github.com/NodePassProject/nodepass) | A secure, efficient TCP/UDP tunneling solution that delivers fast, reliable access across network restrictions using pre-established TCP/QUIC/WebSocket or HTTP/2 connections. | ![GitHub Repo stars](https://img.shields.io/github/stars/NodePassProject/nodepass?style=flat-square)  |
| [OONI Probe](https://github.com/ooni/probe-cli)           | Next generation OONI Probe. Library and CLI tool.                                                                                                                 | ![GitHub Repo stars](https://img.shields.io/github/stars/ooni/probe-cli?style=flat-square)          |
| [reverst](https://github.com/flipt-io/reverst)            | Reverse Tunnels in Go over HTTP/3 and QUIC                                                                                                                        | ![GitHub Repo stars](https://img.shields.io/github/stars/flipt-io/reverst?style=flat-square) |
| [RoadRunner](https://github.com/roadrunner-server/roadrunner) | High-performance PHP application server, process manager written in Go and powered with plugins | ![GitHub Repo stars](https://img.shields.io/github/stars/roadrunner-server/roadrunner?style=flat-square) |
| [syncthing](https://github.com/syncthing/syncthing/)      | Open Source Continuous File Synchronization                                                                                                                       | ![GitHub Repo stars](https://img.shields.io/github/stars/syncthing/syncthing?style=flat-square)     |
| [traefik](https://github.com/traefik/traefik)             | The Cloud Native Application Proxy                                                                                                                                | ![GitHub Repo stars](https://img.shields.io/github/stars/traefik/traefik?style=flat-square)         |
| [v2ray-core](https://github.com/v2fly/v2ray-core)         | A platform for building proxies to bypass network restrictions                                                                                                    | ![GitHub Repo stars](https://img.shields.io/github/stars/v2fly/v2ray-core?style=flat-square)        |
| [YoMo](https://github.com/yomorun/yomo)                   | Streaming Serverless Framework for Geo-distributed System                                                                                                         | ![GitHub Repo stars](https://img.shields.io/github/stars/yomorun/yomo?style=flat-square)            |

If you'd like to see your project added to this list, please send us a PR.

## Release Policy

quic-go always aims to support the latest two Go releases.

## Contributing

We are always happy to welcome new contributors! We have a number of self-contained issues that are suitable for first-time contributors, they are tagged with [help wanted](https://github.com/quic-go/quic-go/issues?q=is%3Aissue+is%3Aopen+label%3A%22help+wanted%22). If you have any questions, please feel free to reach out by opening an issue or leaving a comment.

## Supported by

We are very grateful to the following organizations for supporting quic-go with free access to their tools:

* [Codecov](https://app.codecov.io/gh/quic-go/quic-go): code coverage
* [CodeRabbit](https://www.coderabbit.ai/): AI code review
* [CodSpeed](https://app.codspeed.io/quic-go/quic-go): continuous benchmarking
* [Macroscope](https://macroscope.com/): AI code review
* [OSS-Fuzz](https://github.com/google/oss-fuzz/tree/master/projects/quic-go): continuous fuzzing

## License

The code is licensed under the MIT license. The logo and brand assets are excluded from the MIT license. See [assets/LICENSE.md](https://github.com/quic-go/quic-go/tree/master/assets/LICENSE.md) for the full usage policy and details.
