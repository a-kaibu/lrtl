# LRT 7.5.5 missing root range-mask observations

144 synthetic cases execute the installed LRTimelapse 7.5.5 build 1059 ordinary
missing-column helper and frame-to-XMP writer. No application code, image or
user metadata is included.

The cases cover Gradient, CircularGradient, Paint and MaskGroup corrections at
positions 1 and 2; Type, ColorAmount, LumMin, LumMax, LumFeather and LumRange;
and three target states: existing parent with no range property, existing parent
with an empty range struct, and no parent correction array at all.

Three-frame numeric columns are [1, missing, 2] for Type and [.4, missing, .8]
for the other numeric fields. LumRange uses two distinct synthetic text values
around the missing middle cell. The missing helper forward-fills the middle
cell, and the writer creates the required root correction Bag and range struct.
Existing correction arrays keep their Seq form. Existing parent ID and .125
exposure survive. No source PointModels or source correction ID is copied to
the materialized target.

As in the earlier correction/selector probes, the writer's later application
metadata stamping reaches an uninitialized application singleton. Only that
specific NullPointerException after the property writes is accepted; the
in-memory XMP is inspected and serialized using the installed runtime. This
establishes component behavior, not a complete GUI save, full Auto-Transition
workflow or Camera Raw pixel equivalence. The LumRange strings test text
persistence, not their semantic interpretation by Camera Raw.

Go tests compare all 144 target values, root array form/count, parent ID and
exposure. They check absence of copied source samples, preservation of own
samples, CLI dry runs/saves/backups/no-op repeats for all four collections,
malformed later ranges without earlier writes, and a LumRange-only parent
followed by selector creation in one pass. The installed Adobe XMP parser test
now accepts 190 generated packets, including these 144 cases.

Creation is restricted to the native registered root correction positions.
Missing selector-level ranges and unregistered/SyncID-only paths retain their
separate policy. Sample payloads, unsupported range parameters and Camera Raw
development are outside this comparison.
