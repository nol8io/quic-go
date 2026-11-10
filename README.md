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

Branch `master` is the upstream release named in [`nol8/UPSTREAM`](nol8/UPSTREAM) plus exactly
these changes.

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
   - `integrationtests/self/mtu_test.go` caps its client at upstream's 1452 bytes. From 8192,
     Path MTU discovery doesn't get within 25 bytes of the test path's MTU during that test's
     transfer.
   - `config_test.go` gives the new field a value in its every-field-set helper.
2. **Module path.** Upstream's `github.com/quic-go/quic-go` becomes
   `github.com/nol8io/quic-go`. It changes in every `go.mod` file and Go import, in the
   generated mocks, and in the package paths in CI and build scripts. Links to upstream
   issues, pull requests and documentation still point upstream.
3. This section of the README.
4. **Fork maintenance.** `nol8/UPSTREAM` names the upstream release that `master` holds.
   The scripts `nol8/sync.sh`, `nol8/sync-pr.sh` and `nol8/advisories.sh` move the fork to
   a new upstream release and check upstream's security advisories. The workflow
   `.github/workflows/nol8-upstream.yml` runs them (see
   [Updating to a new upstream release](#updating-to-a-new-upstream-release) and
   [Ownership and security](#ownership-and-security)). None of them is part of the library.

Nothing else changes. To see the full delta, fetch the upstream release named in
`nol8/UPSTREAM`, without upstream's other tags, and compare it with `master`. Most of the
delta is the module path.

```
git fetch --no-tags https://github.com/quic-go/quic-go.git refs/tags/vX.Y.Z:refs/upstream/vX.Y.Z
git diff refs/upstream/vX.Y.Z master
```

Compare content, not history. `master`'s history is upstream's up to v0.65.0, followed by
the nol8 commits. Each later sync is one commit on `master`, so upstream's commits after
v0.65.0 reach `master`'s content but not its history. Only `nol8/UPSTREAM` says which
upstream release `master` contains.

Upstream's workflows are unchanged. Three of them need upstream's own infrastructure, so
they are disabled in this repository's Actions settings: "ClusterFuzzLite PR fuzzing",
"Benchmarks" (CodSpeed) and "Build interop Docker image". GitHub disables scheduled
workflows in a fork by default, so upstream's scheduled ones don't run here either.

## Versions

Tags are `vX.Y.Z-nol8.N`: upstream release `vX.Y.Z` with fork release `N` on it. This
repository keeps none of upstream's tags, so only `-nol8.N` tags exist. Go orders
`vX.Y.Z-nol8.N` before `vX.Y.Z`, so depend on an exact fork version, for example:

```
go get github.com/nol8io/quic-go@v0.65.0-nol8.2
```

Releases are tagged on `master`. A sync to upstream `vX.Y.Z` is released as
`vX.Y.Z-nol8.1`, and each later fix of the fork's own on that release as the next `N`. Each
tag's message says what changed. A fix that an older release needs goes on a branch
`nol8/vX.Y.Z` made from that release's last tag, and is tagged there.

`v0.65.0-nol8.1` was built on an earlier branch, and its tag has been removed. The Go
module proxy still serves it, so that version name is never used again; `v0.65.0-nol8.2`
has the same library code.

## Updating to a new upstream release

The "Upstream sync" job in `.github/workflows/nol8-upstream.yml` runs on the first day of
each month, and on demand from the Actions tab (run the "nol8 upstream" workflow on
`master`). When upstream has a release newer than `nol8/UPSTREAM` and this repository has
no branch `sync/vX.Y.Z`, it runs `nol8/sync-pr.sh`. That builds the branch with
`nol8/sync.sh` (step 1 below), pushes it and opens a pull request into `master`; review it
from step 3 on. If the nol8 changes conflict with the release, the job opens nothing,
fails, and names the files in its summary; then follow all the steps below. To skip a
release, close its pull request and keep the branch.

The job pushes and opens the pull request with a fine-grained personal access token, kept
in the repository secret `NOL8_SYNC_TOKEN`; a pull request opened with the workflow's own
token wouldn't run CI. A member of the SDK team owns the token. It is limited to this
repository, with read and write access to contents, pull requests and workflows, because a
sync also brings upstream's changes to its workflow files. The job fails while the token
is missing or expired, so replace it before it expires.

By hand, work in a clone of this repository that holds no upstream tags: `git tag -l`
lists only this fork's tags. Never fetch upstream with its tags, and push branches with
`--no-follow-tags`, so that no upstream tag can reach this repository.

1. From a clean, up-to-date checkout of `master`, run `nol8/sync.sh vX.Y.Z`. It fetches
   the two upstream releases it needs, without tags, and creates the local branch
   `sync/vX.Y.Z`: upstream `vX.Y.Z` with the module rename, plus the nol8 changes, as one
   commit on top of `master`. It merges the nol8 changes in and follows files that upstream
   renamed.
2. If the nol8 changes conflict with the new release, including in a file that upstream
   deleted or a directory it renamed, the script stops and names the files. Resolve them
   in the worktree it prints, stage the result with `git add` or `git rm`, and run
   `nol8/sync.sh --finish <worktree>`. To give up instead, run the command it prints for
   that.
3. If the script lists files that still name `github.com/quic-go/quic-go` outside a URL,
   check each one. Fix any that need the new path on the branch in step 4; later syncs
   carry the fix as one of the nol8 changes. If a kind of file will keep needing it, add
   it to `rename_module` in `nol8/sync.sh` through a separate pull request into `master`.
4. On the branch, fix anything upstream's changes broke in the nol8 changes, and keep the
   list of changes above true to the branch. Then run `go build ./...`, `go vet ./...` and
   `go test -race ./...` in the root module, and `go vet ./...` in
   `integrationtests/fips` and `integrationtests/gomodvendor`. Commit the fixes to the
   branch.
5. Push the branch with `git push --no-follow-tags origin sync/vX.Y.Z` and open a pull
   request into `master`. After review, squash-merge it.
6. Tag the merge commit with an annotated tag `vX.Y.Z-nol8.1`, naming the upstream release
   in its message, and push only that tag, for example
   `git push --no-follow-tags origin v0.66.0-nol8.1`, never `--tags`.

## Ownership and security

- **Owner:** the nol8 Data Path SDK team. It moves the fork to each upstream release the
  SDK adopts.
- **Upstream security fixes:** applied within 7 days of their release, by syncing to the
  upstream release that contains the fix (run the "nol8 upstream" workflow on demand, or
  `nol8/sync.sh`), or as the next `-nol8.N` of the current release.
- **Advisory watch:** the owners watch upstream's releases and
  [security advisories](https://github.com/quic-go/quic-go/security/advisories); a monthly
  check alone can't meet the 7 days. `govulncheck` matches advisories by module path, so a
  scan of this fork never reports upstream quic-go's own advisories. The "Upstream
  advisories" job in `.github/workflows/nol8-upstream.yml` therefore runs two checks, every
  month, on every pull request and on demand:
  - `nol8/advisories.sh` checks the upstream release in `nol8/UPSTREAM` against the Go
    vulnerability database, and fails on any upstream quic-go advisory that affects it. A
    sync's pull request therefore checks the release it moves to. Run
    `nol8/advisories.sh vX.Y.Z` to check a release before syncing to it.
  - `govulncheck ./...` covers this fork's dependencies and the standard library.

  GitHub notifies the user who added or last changed the workflow's schedule when a
  scheduled run fails. It disables scheduled workflows in a fork by default, and in a public
  repository after 60 days without activity. The owners check on the Actions tab that the
  workflow is enabled, and re-enable it with `gh workflow enable nol8-upstream.yml`.
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
