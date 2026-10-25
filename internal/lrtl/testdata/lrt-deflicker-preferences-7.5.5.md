# LRT 7.5.5 global Deflicker preferences

`lrt-deflicker-preferences-7.5.5.json` is a synthetic subset of the global
`LRTimelapse.config` schema. The installed 7.5.5 configuration confirmed the
four field names and their JSON types; this fixture uses test values rather than
copying any local preference values.

The settings control default black/white exclusion, each cutoff percentage, and
hard versus soft cutoff. Sequence-level
`deflickerIgnoreBlacksWhites` values override the global enabled/disabled state.
Tests cover parsing and range validation, explicit CLI overrides, and batch
output equivalence.
