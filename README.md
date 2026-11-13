# rocket

A read-only Go CLI for inspecting Rocket League `.replay` files using
[go-rocket](https://github.com/sbauer/go-rocket). View match details in the
terminal, query replay data with SQL, export a JSON summary, or explicitly
decode gameplay frames and car/ball trajectories. The CLI does not implement
its own replay parser or modify input files.

## Install

Building or installing from source requires **Go 1.24 or newer**. Querying
uses an embedded, CGO-free SQLite engine; no separate SQLite installation is
required.

```sh
go install github.com/sbauer/rocket-cli/cmd/rocket@latest
rocket --help
```

Ensure the Go binary directory (`go env GOBIN`, or `$(go env GOPATH)/bin` when
GOBIN is unset) is on your PATH.

To build from a checkout:

```sh
git clone https://github.com/sbauer/rocket-cli.git
cd rocket-cli
go build -o rocket ./cmd/rocket
./rocket --help
```

On Windows, build with `go build -o rocket.exe ./cmd/rocket` and run
`.\rocket.exe --help`.

Tagged release binaries require no Go installation. Assets are named
`rocket_<version>_<os>_<arch>` (with `.exe` for Windows), for Linux, macOS
(`darwin`), and Windows on `amd64` and `arm64`. Choose the asset matching your
machine from [Releases](https://github.com/sbauer/rocket-cli/releases).
On Linux/macOS, mark the downloaded binary executable with `chmod +x <file>`.
Releases include a `SHA256SUMS` manifest for verifying downloads.

## Commands

Each command accepts exactly one file. Put options **before** the file path.
Quote paths containing spaces. Use `--` before a file whose name starts with
a dash.

| Command | Output | Network decoding |
| --- | --- | --- |
| `summary` | Readable match/player/goal tables, or JSON | Off |
| `query` | Read-only SQL results as table, JSON, or CSV | Off unless requested |
| `frames` | JSON with ordered actor events and attribute updates | On |
| `trajectories` | JSON with known car/ball position samples | On |

```sh
rocket summary match.replay
rocket summary "/path/to/My Match.replay"
rocket summary --format json match.replay > summary.json
rocket query --sql "SELECT name, goals FROM players ORDER BY goals DESC" match.replay
rocket query --decode-network --format csv --sql "SELECT frame, x, y, z FROM positions WHERE kind = 'ball'" match.replay > ball.csv
rocket frames match.replay > frames.json
rocket trajectories match.replay > trajectories.json
rocket summary -- --example.replay
```

Summaries show available map, replay date, match type, team size, team scores,
winner, player statistics/identifiers, and goal frames. Missing fields display
as `unknown`; scores and winners inferred by go-rocket are marked `(derived)`.
Replay text control characters are escaped for readable terminal output.

`summary --format json` uses an explicit versioned schema, not a full dump of
the library's internal replay representation. Absent optional scalars are
`null`; collections are arrays, including empty arrays. Player online IDs are
decimal strings so clients do not lose unsigned 64-bit precision. Replay dates
retain their original strings without an assumed timezone.

See [SQL queries](docs/query.md), [summary JSON](docs/json.md), and
[gameplay exports](docs/gameplay.md) for schemas, limits, and examples.

### Options

| Option | Commands | Default | Meaning |
| --- | --- | --- | --- |
| `--format text\|json` | `summary` | `text` | Choose readable or JSON output |
| `--format table\|json\|csv` | `query` | `table` | Choose the query result format |
| `--sql <statement>` | `query` | Required | One read-only SQL query |
| `--decode-network` | `query` | Off | Decode known car/ball positions |
| `--crc always\|never\|on-error` | All commands | `always` | Control replay section CRC validation |
| `--help` / `-h` | Root and commands | | Print help |
| `--version` | Root | | Print embedded release version, or `dev` for source builds |

CRC validation is enabled by default. `never` skips CRC checks but still
requires the replay to parse successfully. `on-error` checks a section's CRC
only if parsing that section fails; it is **not** a standalone integrity check
and can accept a parsable replay with a bad CRC.

```sh
rocket summary --crc never match.replay
rocket summary --crc on-error --format json match.replay
rocket summary --help
rocket query --help
rocket frames --help
rocket trajectories --help
rocket --version
```

Successful data output goes to stdout; diagnostics go to stderr. Parsing and
encoding must succeed before JSON is written. An output-device failure still
returns an error even if some bytes have already been written.

| Exit status | Meaning |
| --- | --- |
| `0` | Success, including help and version |
| `1` | File, parse, CRC, typed-data, network decode, query, encoding, or output error |
| `2` | Invalid command, option, or argument count |

## Limitations

go-rocket is experimental and does not support every replay version, property
type, network class, or attribute. Unsupported data fails explicitly; the CLI
does not skip it or present a partial decode as complete. A summary can work
for a replay whose network data is unsupported because summaries never enable
network decoding.

Trajectories contain only known spawn and rigid-body positions. They are not
interpolated, and actor IDs are not automatically mapped to player names.
Actor IDs may be reused; `(actor_id, instance)` distinguishes trajectory
lifetimes. Decoded frames and exports are retained in memory and can be much
larger than the replay itself. There is no replay
editing, stdin input, or multi-file/batch mode.

## Development and checks

```sh
gofmt -w .
go mod tidy -diff
go vet -mod=readonly ./...
go test -mod=readonly -race ./...
go build -mod=readonly ./...
```

CI runs formatting/module checks, vet, race tests, and build on Go 1.24 and
Go 1.27.3 for branch pushes and pull requests, including dependent PR branches.
The integration suite builds the executable and runs it against the small,
attributed [replay fixture](internal/cli/testdata/README.md). Tests generate
invalid inputs and temporary binaries in test-owned directories.

## Releases

Pushing a version tag such as `v0.1.0` triggers the release workflow. It pins
Go 1.27.3 and builds six CGO-free binaries with embedded versions and SHA-256
checksums, then creates a GitHub release. Prerelease tags such as
`v0.1.0-beta.1` are marked as prereleases. CI only needs read permissions;
release publishing receives repository contents write permission.

To build release artifacts locally without publishing a tag:

```sh
GOTOOLCHAIN=go1.27.3 bash scripts/release.sh v0.1.0
```

Artifacts are written to `dist/`. Reproducibility assumes the same source,
version, and pinned Go toolchain. Keep CI's current-version target and the
release toolchain aligned when updating Go.