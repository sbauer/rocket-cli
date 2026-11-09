# Gameplay JSON exports

`rocket frames match.replay` and `rocket trajectories match.replay`
explicitly enable go-rocket's network decoder. Both commands emit one JSON
object with `schema_version: 1`. They accept the same `--crc` option as
`summary`. Summary commands never decode network frames.

## Frames

The frame export contains `objects`, `names`, and `frames`. Each frame has a
zero-based `index`, `time`, `delta`, and ordered `actors`. Each actor event has
`actor_id`, nullable `spawn`, `deleted`, and ordered `attributes`. Spawn events
include `object_index`, resolved `object_name`, optional `name_index`, integer
`location` (`x`, `y`, `z`), and byte-packed `rotation` axes (`yaw`, `pitch`,
`roll`). Resolve a non-null name index against `names`.

Attributes include `stream_id`, `object_index`, resolved `object_name`,
`value_type` (the Go type), and `value`. Unlike the CLI-defined envelope,
attribute values follow the pinned go-rocket types and Go JSON encoding:
structs retain their exported Go field names, integers remain exact JSON
numbers, and byte slices are base64 strings. Use a lossless number parser for
64-bit IDs in attribute values; summary player IDs are strings.

Spawn locations retain raw integer network units. Do not mix these directly
with trajectory positions, which go-rocket normalizes by dividing spawn
coordinates by 100. Frames, actor events, and updates retain stream order.
Actor IDs may be reused after deletion.

## Trajectories

The trajectory export contains `trajectories`, each with `actor_id`, `instance`,
`kind` (`car` or `ball`), `object_name`, and `samples`. The pair `(actor_id,
instance)` identifies an actor lifetime. Each sample has zero-based `frame`,
`time`, and `position` (`x`, `y`, `z`), using go-rocket's normalized coordinates.
Times and frame deltas are in seconds.

Only known spawn and rigid-body positions are exported. Samples are not
interpolated, and actors are not automatically mapped to player names.
Collections are arrays even when empty; optional spawn/rotation fields are
null when absent.

Network formats unsupported by go-rocket fail with exit 1 and no JSON; the
CLI does not skip unsupported attributes or report partial decoding as success.
These exports retain decoded data in memory and can be much larger than the
original replay.
