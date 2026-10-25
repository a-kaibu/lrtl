# LRT 7.5.5 missing selector-range observations

The installed 7.5.5 build 1059 missing-value helper and frame-to-XMP writer
were exercised on 80 synthetic columns: four correction collections
(`GradientBasedCorrections`, `CircularGradientBasedCorrections`,
`PaintBasedCorrections`, and `MaskGroupBasedCorrections`), correction positions
1 and 2, five numeric properties, and a middle frame with the selector range
resource either present or absent. Every case includes a first selector. No
application code, images, or user metadata are included.

The properties and inputs are:

| Property | Input |
| --- | --- |
| Type | `[1, missing, 1]` |
| ColorAmount | `[.25, missing, .75]` |
| LumMin | `[.1, missing, .8]` |
| LumMax | `[.2, missing, .9]` |
| LumFeather | `[.15, missing, .65]` |

The native helper forward-fills the middle value in all five columns. The
writer serializes each value to the nested path, creating the middle frame's
`CorrectionRangeMask` when it was absent. It leaves existing `PointModels`
untouched and does not copy samples into a newly created resource. This
establishes component behavior for numeric selector-range fields; it does not
establish creation of a missing selector, creation of the `LumRange` string,
or a complete native Auto-Transition/save workflow.

The writer's later history stamping reaches an uninitialized application
singleton when called outside the GUI. The probe accepts only that specific
post-write `NullPointerException`, then reads the in-memory XMP values. The JSON
fixture records collection, position, target resource state, inputs, filled and
serialized values, and per-frame sample payloads. Normal Go tests compare all
80 cases against this fixture, run the probe again when `LRTL_NATIVE_JAR` is
set, and cover CLI dry-run/write/backup/no-op repeat, image preservation,
sample behavior, and malformed input without partial writes.

Re-run the installed probe with Java/Javac and the installed jar:

```powershell
$env:LRTL_NATIVE_JAR='C:\Program Files\LRTimelapse 7\LRTimelapse.jar'
go test ./internal/lrtl -run '^TestMissingSelectorRangeInstalledReplay$' -count=1 -v
```

The probe source is `scripts/probes/MissingSelectorRangeOracle.java`. The Go
CLI has no Java dependency.
