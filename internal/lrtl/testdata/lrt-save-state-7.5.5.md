# WB/crop save preparation (LRTimelapse 7.5.5)

## CLI and migration

`copy-settings` now defaults to `--save-state lrt-7.5.5` after both full-XML and
selected-column copying. `--save-state preserve` retains the isolated copy
behavior of `da97960`, including the selected-column type conversion already
implemented there. This switch does not mean that all metadata is left intact:
the requested copy still runs.

```powershell
# Copy a crop coordinate and derive its activation state.
lrtl copy-settings reference.xmp SEQUENCE --properties CropLeft --write

# RAW-style WB: copy the name and both absolute numbers together.
lrtl copy-settings reference.xmp SEQUENCE --properties WhiteBalance,Temperature,Tint --write

# Reproduce the previous isolated copy phase.
lrtl copy-settings reference.xmp SEQUENCE --properties CropLeft --save-state preserve --write
```

Dry runs report the profile and final Quality/WhiteBalance/HasCrop per changed
sidecar. Image selection, reference exclusion, shared-sidecar protection,
preflight validation and original backups still apply. Images and the reference
are never edited. All stages run on a detached XML document; only final semantic
root changes are committed. Cancelled intermediate edits cannot move unchanged
properties and create spurious backup files.

## Observed save-state rules

| State | Implemented behavior |
| --- | --- |
| WB label | Missing WhiteBalance becomes `As Shot`; empty strings remain present. |
| Exact `As Shot` | Removes Temperature, Tint, IncrementalTemperature and IncrementalTint. |
| Other labels, cached quality exactly `FLAT` | Removes absolute Temperature/Tint. |
| Other labels and other/missing quality | Removes IncrementalTemperature/IncrementalTint. |
| Nine number columns | CropTop/Left/Bottom/Right/Angle plus the four WB numbers are written through their registered native numeric types. Missing cells remain absent. |
| Crop activation | Strictly compare cached Top/Left/Angle with 0 and Bottom/Right with 1. Missing cells use those defaults. The old HasCrop flag does not decide activation. |
| HasCrop output | Canonical `True` or `False`, derived before numeric serialization. |
| Cached quality | A nonempty quality cache is written back to `lrt:Quality`. An empty/missing cache does not write a replacement. |

WB label and quality comparisons are case-sensitive and do not trim text.
`as shot`, `AS SHOT`, empty and whitespace labels are distinct from `As Shot`;
`flat` is distinct from `FLAT`. Numeric parsing/formatting reuses the measured
column implementation: Java-compatible integer conversion, six fractional
digits for Double fields, no slider-range clamp, signed zero and native
non-finite spellings. Invalid numeric strings become zero as in the cell loader.

The crop decision uses the **unrounded cache**. For example a copied left bound
of `0.0000004` writes `0.000000` but still yields HasCrop=True in that operation.
Inf/NaN also differ from the defaults; -0 compares equal to 0. A subsequent
reload can change the decision for values rounded to defaults. This is not
replaced with an epsilon test or universal fresh-load idempotence guarantee.

Full copying reads the cache from the merged packet. Selected-column copying
starts with the destination cache and replaces only selected numeric cells from
the source, before the source value loses precision. The frame's separate
quality cache is not updated by assigning its visible column. Thus copying a
different Quality value can be undone by the nonempty original cache during
save preparation. That behavior was measured rather than inferred from the new
XML label.

These rules can update WB/crop values outside `--properties`. An explicit empty
reference clears Camera Raw settings during full copying, then the default save
stage adds WB/HasCrop state. Use `preserve` when the isolated XML phase is wanted.

## Native observations and provenance

[`SaveStateOracle.java`](../../../scripts/probes/SaveStateOracle.java) is an
original synthetic reflection harness. It uses the installed 7.5.5 metadata
registry and invokes these components:

- `tQa.h(Wd): void`: WB cleanup; this call completes normally.
- `tQa.j(): boolean`: activation from the cached crop numbers; completes normally.
- The existing typed cell loader/getter/setter for raw source and destination
  caches, including actual column assignment before crop evaluation.
- `tQa.A(Rsa,ArrayList,boolean): void`: a save prefix with the relevant numeric,
  WB-label and crop-flag columns, retaining unrelated packet properties.

`lrt-save-state-7.5.5.json` contains 267 WB conditions, 124 crop conditions and
six copied-cache conditions: **397 conditions in total**. Attribute, element
and multiple-description encodings are included. The WB matrix covers missing,
empty, case variants and other labels with eight quality states, plus three
explicit XML-label/cache mismatches. Crop inputs include default/missing values,
negative zero, tiny values, malformed numbers, hex, infinities and NaN. Copied
caches cover removal and precision loss, and overriding a selected HasCrop flag.

The `expected` WB packets are the complete direct cleanup-helper result.
`expected_active` is the direct crop predicate result. `saved` records the
deterministic numerical/WB/crop save prefix. **The full store call still does
not complete:** a later history stage requires `LRTimelapseMain.m()` and raises
the known null-main error outside the app. The harness asserts that exact stage,
marks `store_completed:false`, and removes its generated history schema before
recording the prefix. No main/license stub or patched binary is used. This is
not a claim that the complete GUI save workflow ran successfully.

`lrt-save-state-root-scope-7.5.5.json` separately records direct SDK lookups after
WB cleanup. Root Temperature/CropRight are absent while the same names under a
nested RDF Description remain 777/.5. The Go renderer previously read nested
descriptions as possible global settings, which could fail WB validation or
use the wrong crop boundary. `GetRootScalar` now bounds the basic-tone/WB and
crop readers to root descriptions. A regression test retains that nested
metadata and compares AVI pixels with a control packet lacking the annotation.

Bare `rdf:RDF` documents are also supported by the shared root-property helper.
Unrecognized structures without a root metadata description are rejected before
copying, preventing them from being treated as empty source settings.

## Tests and reproduction

Normal tests compare all 397 conditions and their saved prefixes, direct and
wrapped RDF, source immutability, frozen-cache repeat behavior, final-only root
commit, backups, profile selection, late-error preflight, cached quality
writeback, and crop/WB AVI pixels against explicit controls. The prior metadata
and column-only tests continue to cover `preserve` and their isolated helpers.

```powershell
go test ./internal/lrtl -run '^(TestNativeSave|TestSaveState|TestCopySettings|TestSelectedColumn|FuzzNativeSaveState)' -count=1
go test -race ./internal/lrtl -run '^(TestNativeSave|TestSaveState|TestCopySettings|TestSelectedColumn|TestMetadataPaste|TestXMPBasic|TestCrop|FuzzNativeSaveState)' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeSaveState$' -fuzztime 60s -parallel 2

javac -encoding UTF-8 -d work/metadata-copy-native/classes scripts/probes/MetadataPasteOracle.java scripts/probes/SaveStateOracle.java
java --enable-native-access=ALL-UNNAMED -cp "work/metadata-copy-native/classes;C:/Program Files/LRTimelapse 7/LRTimelapse.jar" lrtlprobe.SaveStateOracle work/save-state-replay.json
java --enable-native-access=ALL-UNNAMED -cp "work/metadata-copy-native/classes;C:/Program Files/LRTimelapse 7/LRTimelapse.jar" lrtlprobe.SaveStateOracle work/save-state-scope.json --scope

$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run '^TestSaveStateInstalledComponents$' -count=1
```

The optional test replays the installed component observations, then sends
Go-generated bare-RDF results through its parser/serializer. Native binaries
and decompiled code are not distributed. The executable and all new production
logic are pure Go; Java is only for optional local comparison.

### Validation recorded on 2026-10-23

- The final `go test ./... -count=1 -timeout 20m` run passed with installed
  FFmpeg, LRTimelapse and ExifTool comparisons enabled (765.801 s for
  `internal/lrtl`), including existing image/video regressions.
- All 397 installed-component observations replayed successfully. The separate
  root-scope observation also reproduced the checked-in JSON byte for byte.
- Focused race tests passed with the installed-component comparison enabled
  (75.456 s). This run covered 84.4% of statements in `save_state.go` and
  86.2% in `copy_settings.go`; these are focused-run file coverage figures.
- `FuzzNativeSaveState` completed 500,608 executions in a 60-second fuzz run;
  the metadata-paste fuzz target completed another 154,315 executions.
- Focused Go 1.23 tests passed. Go 1.26 `go vet ./...` and the
  `CGO_ENABLED=0` executable build succeeded.
- The built executable copied a selected crop value into three synthetic PNG
  frames and generated AVI files. Dry run, source/image preservation, one
  backup per sidecar and repeat-without-writes passed. Changed video frames
  matched an explicit crop-metadata control.

## Remaining scope

This profile applies to `copy-settings`; `init` now also shares WB/crop save
preparation and adds explicit image-based quality reconciliation (see
[frame initialization](lrt-frame-state-7.5.5.md)). Full frame initialization,
classification within `copy-settings` itself, all other
numeric columns, HG/deflicker cache writes, rating/date/dimension side effects,
history and UI state are not yet reproduced. Scalar state values are required;
invalid structured state is rejected during CLI preflight. Other editing
commands do not yet share the complete save preparation.

The renderer remains an independent approximation of Camera Raw. In particular
IncrementalTemperature/Tint now have an independent sRGB pixel approximation
for root `lrt:Quality=FLAT`, documented in the frame-initialization notes. The metadata comparisons do not
prove Adobe pixel equivalence or complete LRTimelapse feature parity.
