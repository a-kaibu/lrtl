# Native long-term filter presets and reference curves

## CLI workflow

```powershell
lrtl long-term SEQUENCE --lrt-filter-preset settings.json --output selection.json
lrtl long-term SEQUENCE --lrt-filter-preset settings.json --lrt-filter-preset-name daylight --output selection.json
lrtl long-term SEQUENCE --lrt-filter-preset filter.json --lrt-filter-color on --output selection.json
lrtl render SEQUENCE --selection selection.json --output result.avi
```

The option accepts a single native `FilterPreset` object or a settings object
containing a `filterPresets` map. Without an explicit name, the observed
`default` entry is preferred, otherwise a single available entry is used.
An ambiguous map requires an exact name. The file is read without modification.

Using a saved preset selects the `lrt-7.5.5` metric profile automatically.
An explicit conflicting `--metric-profile lrtl` is an error. The ordinary
profile and static range-only workflow remain available without a preset.

`--lrt-filter-color auto` reads the settings-level
`filterContrastAndHueActive`; absent that field, contrast/hue are disabled.
`on` and `off` explicitly override that switch, including for a standalone
preset. Luminance and time/day conditions are independent of that switch.
The preset's Lock and ShowCurve values control native UI behavior and do not
change evaluation of its saved threshold/curve values.

Imported numeric slider settings use the native widget bounds: smoothing
0..50, Low/High 0..100, Constant offset -100..100. Values outside those ranges
are clamped. Time bounds are validated as 00:00..24:00. Unknown preset fields,
including native identity/UI properties, are not copied into the used model.
JSON files must be regular files of at most 16 MiB.

## Selection order and arithmetic

The observed native worker begins with the entire sequence, applies weekday
and clock conditions, then luminance. When color filtering is enabled,
contrast follows luminance, and hue follows contrast. Each later axis builds
its reference from the frames that survived earlier axes.

- **Inactive:** smoothing zero with Constant disabled leaves that axis alone,
  including its Low/High thresholds.
- **Constant:** mean of the current selected values plus offset/150, clamped
  to 0..1. Hue uses normalized arithmetic HSB, without circular unwrapping.
- **Short curves:** fewer than eight selected frames use the endpoint line
  over selected ordinal positions. One selected frame produces a native NaN
  target; both threshold comparisons then fail and retain the frame. Go omits
  that unavailable target from JSON rather than writing a non-finite number.
- **LOWESS:** slider/80 times the **integer** original-count/selected-count
  ratio controls bandwidth. Regression uses the selected samples' ordinal
  positions, not their original indices or capture-time spacing. The existing
  native-compatible LOWESS model handles the helper's bandwidth retry.
- **Thresholds:** subtract the reference from the measured value and reject
  at or beyond either bound. In native float32 order, bounds are
  `-(2 - Low/50)/10` and `(2 - (100-High)/50)/10`. Equality is rejected; these
  differ from a closed inclusive range. Hue uses normalized 0..1 values for
  decisions; its target is reported in degrees in the manifest.

Additional CLI `--luminance-range`, `--contrast-range` and `--hue-range`
conditions intersect the native result **after** its curves have been built.
They do not silently change the samples used by the native stages.

The manifest includes the normalized used preset, color switch and time-stage
status under `lrt_filter`. Each frame records available reference values and
the first exclusion reason (`time`, `luminance`, `contrast`, `hue`, or `range`).
The regular selection consumer still checks source identities and uses the
recorded inclusion flags; rendering does not recalculate the filter.

## Weekdays, clocks and missing metadata

Both clock endpoints are excluded, at minute precision. An overnight start
after its end does not wrap; it selects no valid clock. Equal start/end also
selects none. All weekdays and 00:00..24:00 form a native shortcut that retains
even midnight without inspecting timestamps.

The observed worker's time-stage availability comes from the sequence's
capture-date availability, rather than the preset's `timeFilterActive` field.
That serialized field is retained in the used preset but does not gate the
native model. Any unavailable date disables the whole time stage and records
`time_filter_skip_reason: "capture times incomplete"`. A present, unparseable
date is retained by the time stage; the following metric filters still apply.

Go reads XMP DateTimeOriginal/CreateDate/ModifyDate, then supported image EXIF.
It uses capture wall-clock fields and never modification time. Common EXIF/ISO
forms are accepted. The native date parser's lenient modern date normalization
and accepted prefix are modeled, including the measured February-30 case.
Ancient calendar cutovers, extreme date fields, camera-specific metadata
precedence and all native image-decoder date conversions are not established.

## Evidence and limits

Measured with installed LRTimelapse 7.5.5 build 1059 and Java 25 on Windows,
2026-10-22. No application implementations, preferences, binaries or license
state changes are included.

`lrt-longterm-curves-7.5.5.json` records native constructor defaults, the
default preset key/date-format/missing-date marker, and **924 native curve
helper cases**. It covers original counts 1/2/5/7/8/12/33, full and stride-three
selections, six signal patterns, six smoothing settings and five Constant
offsets. Target values are compared within 1e-10, including semantic NaN checks.

`lrt-longterm-time-7.5.5.json` records **36 native worker cases**, with 52 date
inputs each: weekdays, boundary clocks, missing/empty/n/a dates, an invalid
date, lenient normalization and a timezone suffix. The original harness calls
the native time worker and sequence availability/shortcut checks on isolated
synthetic objects. Its table subclass suppresses only the final status-display
callback. No windows are created and no license checks or results are changed.

The metric-axis **helpers**, defaults and native caller arithmetic/order were
examined separately. The complete native luminance/contrast/hue GUI worker was
not used as an end-to-end oracle: its UI-enabling path depends on initialized
application state. Do not interpret helper comparisons as proof that every
application workflow has identical final selection. Native reference curves
are combined with independently implemented threshold and sequencing logic.
The inspected caller's threshold expressions also have 101 pairs of recorded
JVM float32 bounds, compared bit for bit. These are arithmetic observations,
not additional executions of the complete native metric worker. CLI discovery
order is used; saved native UI sorting and preview-cache ordering are not
automatically inferred from the global preset file.

Tests also cover preset selection/defaults/clamping/malformed input, stage
order, original selection density, static-range composition, inactive stages,
one/zero remaining frames, decision provenance, JSON finiteness, CLI preset
overrides, a real nine-frame filtered AVI, source preservation and failure with
a corrupt later image while an existing selection output is present.

```powershell
javac -d work/filter-classes scripts/probes/LongTermFilterOracle.java
java -cp 'work/filter-classes;C:\Program Files\LRTimelapse 7\LRTimelapse.jar' lrtlprobe.LongTermFilterOracle work/curves.json
javac -cp 'C:\Program Files\LRTimelapse 7\LRTimelapse.jar' -d work/filter-classes scripts/probes/LongTermTimeOracle.java
java -cp 'work/filter-classes;C:\Program Files\LRTimelapse 7\LRTimelapse.jar' lrtlprobe.LongTermTimeOracle work/time.json
go test ./internal/lrtl -run '^TestLRTLongTerm|^TestLongTermNative' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzLRTLongTermFilter$' -fuzztime 10s -parallel 2
```

Normal Go tests use the stored synthetic references. No new module dependency
or CGo requirement is introduced. RAW/Camera Raw preview equivalence and the
native curve editor UI remain outside the verified implementation.

## Verification record

On 2026-10-22, the focused suite passed on Go 1.26.9 with the race detector
(9.051 seconds) and on Go 1.23.0 (2.908 seconds). A 10-second fuzz run passed
47,316 generated filters, checking deterministic selection and finite JSON.
`go vet ./...` and a `CGO_ENABLED=0` Windows CLI build passed. Freshly compiled
probes reproduced both stored native-reference files' SHA-256 hashes.

Focused ordinary-test statement coverage was 90.9% for curve generation,
89.7% for stage application, 100% for the time stage and 93.4% for preset
loading. This records exercised code rather than proving unobserved native
workflows. The latest focused run also includes the JVM threshold-bit and
late consumed-XMP failure cases added during the full-suite run.

The full `go test -json ./... -count=1 -timeout 20m` passed with installed
FFmpeg, native application-JAR and ExifTool reference checks enabled
(685.905 seconds for the lrtl package). Implementation files stayed unchanged
during that run; the later test-only assertions were also rerun in the
focused ordinary/race/Go-1.23 checks above.
