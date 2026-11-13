# SQL queries

`rocket query` loads one replay into a private, in-memory SQLite database and
executes one read-only query. No database or SQLite installation is needed.
Options precede the replay path:

```sh
rocket query --sql "SELECT name, goals, shots FROM players WHERE goals > 0 ORDER BY goals DESC" match.replay
rocket query --format json --sql "SELECT team, count(*) AS players FROM players GROUP BY team" match.replay
rocket query --decode-network --format csv --sql "SELECT frame, time, x, y, z FROM positions WHERE kind = 'ball' AND time BETWEEN 10 AND 15 ORDER BY frame" match.replay > ball.csv
```

`--format` accepts `table` (the default), `json`, or `csv`; `--crc` accepts
`always` (the default), `never`, or `on-error`, as for the other commands.

## Tables

Column names below are stable in schema version 1. Missing optional values
are SQL `NULL`. SQLite represents booleans as integers `0` and `1`; `_derived`
flags identify values inferred by go-rocket rather than recorded directly.

| Table | Columns |
| --- | --- |
| `match` | `map`, `date`, `match_type`, `team_size`, `team0_score`, `team1_score`, `winner`, `team0_score_derived`, `team1_score_derived`, `winner_derived` |
| `teams` | `team`, `name`, `score`, `score_derived`, `is_winner`, `winner_derived` |
| `players` | `name`, `team`, `score`, `goals`, `assists`, `saves`, `shots`, `is_bot`, `platform`, `online_id`, `epic_account_id` |
| `goals` | `frame`, `player`, `team` |
| `highlights` | `frame`, `car`, `ball`, `goal_actor` |
| `positions` | `actor_id`, `instance`, `kind`, `object_name`, `frame`, `time`, `x`, `y`, `z` |

`match` has one row; `teams` has rows for Blue (`team = 0`) and Orange
(`team = 1`). Team scores and winners may be `NULL` when unknown. Player
statistics are `NULL` when absent, not zero. Player IDs are text; in
particular, `online_id` is a decimal string so the full unsigned 64-bit range
is preserved. Replay dates retain their original text and have no inferred
timezone.

`positions` requires `--decode-network`: querying it without that option is an
error. With decoding enabled, it contains only known car and ball spawn and
rigid-body samples, not interpolated positions. `frame` is zero-based, `time`
is in seconds, and coordinates use go-rocket's normalized trajectory units.
Actor IDs can be reused; `(actor_id, instance)` identifies an actor lifetime.
Actors are not automatically mapped to player names.

## SQL and outputs

Each invocation runs one `SELECT` (or `WITH ... SELECT`) query, with joins,
grouping, sorting, and filtering. The database is read-only: writes, schema
changes, `PRAGMA`, `ATTACH`/`DETACH`, extensions, and multiple statements are
rejected. Common core, aggregate, and date functions are available (for
example `count`, `sum`, `avg`, `min`, `max`, `round`, `coalesce`, `lower`,
`substr`, and `strftime`).

SQLite arithmetic rules apply: an overflowing integer expression may become a
floating-point value and lose precision, while integer `sum` overflow is an
error. Casting a large text ID to an integer can also lose information. Keep
IDs as text; use a lossless JSON number parser for other large integers.
Non-finite floating-point input and output are rejected.

Table output includes a header even for zero rows. SQL `NULL` is shown as
`NULL`, an empty text value as `""`, and the literal text `NULL` as `"NULL"`.
Control characters are escaped in both column names and values.

JSON uses ordered columns and row arrays, preserving duplicate column names
and distinguishing `null`, empty text, zero, and numeric types:

```json
{"schema_version":1,"columns":["name","goals"],"rows":[["Example",0]]}
```

Empty results have `"rows":[]`. SQLite BLOB values are base64 strings in all
three formats; select `typeof(column)` as well if a JSON consumer needs to
distinguish a BLOB from text.

CSV includes a header even for zero rows; both `NULL` and empty text render as
empty fields. Use JSON when you need to distinguish them.

All data is collected and formatted before writing to stdout, so parse, query,
limit, and encoding errors do not produce partial results. A failure writing
to the output device can still leave partial bytes.

## Limits and performance

Queries accept replays up to 64 MiB. Results are capped at 100,000 rows and
128 columns, and a query that runs longer than 30 seconds is stopped.

The full replay and database are held in memory. With `--decode-network`, all
positions are decoded and loaded even when SQL filters by time or does not
select positions. In single local runs on macOS/arm64 (Apple M1 Pro, Go 1.24.1),
the following queries measured wall time and maximum resident memory:

| Replay | Metadata `count(*)` | Decoded positions `count(*)` |
| --- | --- | --- |
| CLI fixture: 26,211 bytes, 248 frames, 4 positions | 0.43 s / 104 MiB | 0.30 s / 105 MiB |
| go-rocket `samples/boxcars/d52eb.replay`: 1,326,120 bytes, 17,815 frames, 30,791 positions | 0.30 s / 104 MiB | 0.38 s / 115 MiB |

These are illustrative single-run measurements, not guarantees; machine,
replay, query, and output sizes affect both time and memory.
