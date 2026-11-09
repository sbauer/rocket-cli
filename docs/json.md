# Summary JSON

Run `rocket-cli summary --format json match.replay`. The command writes one
indented JSON object and a newline to stdout; diagnostics go to stderr.

The versioned contract has `schema_version: 1`, `metadata`, and `match`:

- `metadata` contains `class_name`, `engine_version`, `licensee_version`,
  `net_version`, `levels`, `keyframes` (`time`, `frame`, `position`), and
  `tickmarks` (`frame`, `description`). Keyframe position is the library's
  serialized replay offset, not an actor's spatial position.
- `match` contains `map`, `date`, `match_type`, `team_size`, `winner`,
  `winner_derived`, `teams`, `players`, `goals`, and `highlights`.
- Teams have `index`, `name`, `score`, `score_derived`, `is_winner`,
  `winner_derived`, and `players` (names).
- Players have `name`, `team`, `score`, `goals`, `assists`, `saves`, `shots`,
  `is_bot`, `platform`, `online_id`, and `epic_account_id`.
- Goals have `frame`, `player`, and `team`. Highlights have `frame`, `car`,
  `ball`, and `goal_actor`.

Absent optional scalar fields are `null`, distinct from recorded zero, false,
or empty string. Collections are always arrays, including empty arrays when
unavailable. Team indices 0 and 1 represent Blue and Orange. Derived results
come from go-rocket and are explicitly flagged.

`online_id` is a decimal **string**, preserving unsigned 64-bit identifiers in
clients such as JavaScript. `epic_account_id` is also a string. Replay dates
remain the original string; no timezone is assumed.

Raw network bytes and untyped property maps are intentionally not part of this
summary contract. Parse/data/encoding failures exit 1, usage errors exit 2,
and successful commands exit 0. Failed parsing emits no JSON.
