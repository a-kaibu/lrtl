# LRT 7.5.5 missing selector-range field observations

Five synthetic numeric columns call the installed 7.5.5 build 1059 missing-value
helper and frame-to-XMP writer. Each frame already has a first selector and a
`CorrectionRangeMask` resource. Only the selected numeric property is absent in
the middle frame; the containing range resource and its `PointModels` are kept.
No application code, images or user metadata are included.

The columns are:

| Property | Input |
| --- | --- |
| Type | `[1, missing, 1]` |
| ColorAmount | `[.25, missing, .75]` |
| LumMin | `[.1, missing, .8]` |
| LumMax | `[.2, missing, .9]` |
| LumFeather | `[.15, missing, .65]` |

The native helper forward-fills the middle value in all five columns. The native
writer serializes the values to the existing nested property path and leaves
each frame's own `PointModels` untouched. This establishes component behavior
for an existing selector range resource; it does not establish creation of a
missing selector-level `CorrectionRangeMask` or a complete GUI save workflow.

The writer's later history stamping reaches an uninitialized application
singleton when called outside the GUI. The probe accepts only that specific
post-write `NullPointerException`, then reads the in-memory XMP values. The
fixture records the input, filled values, serialized values and retained sample
payloads. Normal Go tests compare all five columns against this fixture, verify
the CLI save/backup/no-op repeat, and ensure malformed structures fail before
writing files.

Re-run the installed probe with Java/Javac and the installed jar:

```powershell
$env:LRTL_NATIVE_JAR='C:\Program Files\LRTimelapse 7\LRTimelapse.jar'
go test ./internal/lrtl -run '^TestMissingSelectorRangeInstalledReplay$' -count=1 -v
```

The probe source is `scripts/probes/MissingSelectorRangeOracle.java`. The Go
CLI has no Java dependency.
