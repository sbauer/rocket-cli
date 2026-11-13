# rocket

A read-only Go CLI for inspecting Rocket League `.replay` files using
[go-rocket](https://github.com/sbauer/go-rocket). View match details in the
terminal, export a JSON summary, or explicitly decode gameplay frames and
car/ball trajectories. The CLI does not implement its own replay parser or
modify input files.

## Install

Building or installing from source requires **Go 1.24 or newer**:

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

Inspection commands accept exactly one file or, for `summary`, a library
selector. Put options **before** the file path or selector.
Quote paths containing spaces. Use `--` before a file whose name starts with
a dash.

| Command | Output | Network decoding |
| --- | --- | --- |
| `summary` | Readable match/player/goal tables, or JSON | Off |
| `replays list` | Readable replay-library table | Off |
| `frames` | JSON with ordered actor events and attribute updates | On |
| `trajectories` | JSON with known car/ball position samples | On |

```sh
rocket summary match.replay
rocket summary "/path/to/My Match.replay"
rocket summary --format json match.replay > summary.json
rocket frames match.replay > frames.json
rocket trajectories match.replay > trajectories.json
rocket summary -- --example.replay
rocket replays list
rocket summary latest
rocket summary 3
rocket summary --dir "/path/to/TAGame/Demos" "My match"
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

See [summary JSON](docs/json.md) and [gameplay exports](docs/gameplay.md) for
field definitions, units, and attribute-value encoding.

### Options

| Option | Commands | Default | Meaning |
| --- | --- | --- | --- |
| `--format text\|json` | `summary` | `text` | Choose readable or JSON output |
| `--dir directory` | `summary`, `replays list` | Auto-detect | Override the replay library |
| `--crc always\|never\|on-error` | All inspection commands and `replays list` | `always` | Control replay section CRC validation |
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
| `1` | File, parse, CRC, typed-data, network decode, encoding, or output error |
| `2` | Invalid command, option, or argument count |

### Replay library

`replays list` lists regular `.replay` files directly in the library, with
1-based positions, filename, saved replay name, date, map, match type, and
Blue/Orange scores where available. File extensions are case-insensitive.
Ordering uses **file modification time**, newest first, with filename as a
stable tie-breaker; the recorded date is displayed unchanged. Copying files
can change this order, and positions can change as the library changes.
Subdirectories are not scanned.

`summary` accepts `latest`, a position such as `3`, or an exact filename,
filename without its extension, or saved `ReplayName`. Names are
case-sensitive. Ambiguous names fail with matching paths; use a position or
explicit path instead. An existing local path always wins over a selector,
even if its filename is `latest` or `3`. Absolute paths and paths containing
directory separators are always treated as file paths. `frames` and
`trajectories` continue to require file paths.

Directory precedence is `--dir`, then `ROCKET_REPLAY_DIR`, then discovery.
The override is a path to the `TAGame/Demos` directory itself. Windows uses
the actual Documents known folder, including redirected/OneDrive Documents,
then `My Games/Rocket League/TAGame/Demos` (shared by Steam and Epic).
Linux checks standard Steam, Flatpak Steam, and secondary libraries from
`steamapps/libraryfolders.vdf`, using Proton app ID `252950`. It also checks
the default `~/.wine` prefix, prefixes under `~/Games`, Heroic's
`~/Games/Heroic/Prefixes`, and standard/Flatpak Bottles prefixes. Wine user
folders are checked for Documents, My Documents, and OneDrive/Documents.
macOS checks `~/Library/Application Support/Rocket League/TAGame/Demos`
for legacy installations; the game no longer officially supports macOS.
Custom launcher/prefix layouts may require an override. Multiple detected
libraries produce an error rather than an arbitrary choice; symlink aliases
of the same library are deduplicated.

The pinned go-rocket API has no public header-only parser. Listing parses
each replay sequentially with **network decoding disabled**, still reading
and validating its body; no parser is duplicated in the CLI. A damaged or
unsupported replay gets an `unreadable metadata` row and a diagnostic on
stderr, while other files remain listed (exit status `0` for a successfully
written list, even with metadata diagnostics). Missing metadata displays as
`unknown`. Selecting an unreadable replay fails normally. `latest` and
position selection only parse the selected replay; name selection inspects
metadata to detect ambiguous saved names. Large libraries can therefore
take time to list or search by name. Replay inputs larger than 64 MiB are
rejected before parsing; this limit also applies to `summary`, `frames`, and
`trajectories`. No replay, index, or cache is written.

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
larger than the replay itself. There is no replay editing, stdin input, or
multi-file/batch export mode.

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