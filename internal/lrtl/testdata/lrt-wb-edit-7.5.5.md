# Numeric WB editing (LRTimelapse 7.5.5)

`set` and `set-sequence` now prepare WB state when editing the active
Temperature/Tint pair. Previously editing a temperature could leave
`WhiteBalance=As Shot`, allowing a subsequent native-profile save to discard it.

## CLI behavior

```powershell
lrtl set frame001.xmp Temperature 6500.9 --dry-run
lrtl set frame001.xmp Temperature 6500.9
lrtl set-sequence SEQUENCE Tint 20 --ratings 4,5 --write
lrtl set-sequence SEQUENCE IncrementalTemperature -15 --write
```

The four WB numeric inputs are truncated toward zero and clamped to their
integer slider ranges. This changes the old unbounded numeric-set behavior
for these four properties. Selected-column copying retains its separate
native load/serialization rules and does not use these slider bounds.

| Property | Minimum | Maximum | Missing companion default |
| --- | ---: | ---: | ---: |
| Temperature | 2000 | 50000 | 5850 |
| Tint | -150 | 150 | 8 |
| IncrementalTemperature | -100 | 100 | 0 |
| IncrementalTint | -100 | 100 | 0 |

- The exact cached quality label `FLAT` selects the incremental pair. Other
  labels, including missing labels and `flat`, select the absolute pair.
  Image extensions are not used to guess quality.
- Editing the active pair sets root `crs:WhiteBalance=Custom`, writes both
  pair members and removes the inactive pair.
- The edited member uses the requested bounded integer. The companion uses
  its root XMP value loaded with native Integer conversion, then clamped to
  the slider range. Missing companions use the table's defaults.
- Editing the inactive pair updates that one scalar without changing the WB
  label, companion or other pair. The normal app UI hides that slider; the
  callback branch is nevertheless measured and available through the CLI.
- WB previews show all five resulting fields, including absent fields.
  Existing selection, preflight validation, backup and repeat-edit behavior
  remain in effect. Invalid structured active WB state is rejected before
  any sidecar is written.

All global numeric edits, including Preview EV, target root RDF descriptions.
A same-name field nested in a mask, preset or other struct is retained and
cannot absorb a global edit.

## Native observations

`lrt-wb-edit-7.5.5.json` contains 960 synthetic conditions: four cached quality
labels × four original WB labels × three sets of numeric cache inputs × four
edited properties × five requests. Inputs include missing values, fractional
rounding, out-of-range integers, NaN and integer wraparound. Requests exercise
truncation and slider bounds. Each input includes nested same-name metadata.

The original [`WBEditOracle.java`](../../../scripts/probes/WBEditOracle.java)
uses the installed registry, typed frame loading and genuine Cqa/GSa controls.
It obtains bounds from the registry, initializes sliders from synthetic cache
cells/default values, sets the edited slider, and invokes `qqa.A(Epa,double)`.

**This observes a callback prefix, not a completed GUI edit.** The observer
uses `sun.misc.Unsafe.allocateInstance` solely to create an editor shell
without running its GUI constructor. It installs its synthetic frame and
four genuine controls, leaving controller `g` null. Every callback must raise
the specific null-controller error after WB cache assignment. The fixture
records `callback_completed:false` and `editor_constructor_skipped:true`.
No application binary or licensing state is patched or replaced. Full GUI,
keyframe/rating updates, redraw, preview and history are not executed.

Normal tests compare all 960 cache states in wrapped and bare RDF forms.
Untouched XML integers retain their lexical form and are compared through
native typed loading; this is not a full serializer comparison for unrelated
fields. Tests also cover root scope, failed-edit atomicity, late-invalid-frame
preflight, previews, selection/backups, repeat stability, and AVI pixels
against independently specified WB metadata. A following `copy-settings`
save must retain the edited Custom WB.

```powershell
go test ./internal/lrtl -run '^(TestWBEdit|TestSet|FuzzWBNumericEdit)' -count=1
go test -race ./internal/lrtl -run '^(TestWBEdit|TestSet|TestSaveState|TestCopySettings|FuzzWBNumericEdit)' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzWBNumericEdit$' -fuzztime 60s -parallel 2

javac -encoding UTF-8 -d work/wb-edit-probe scripts/probes/MetadataPasteOracle.java scripts/probes/WBEditOracle.java
java --enable-native-access=ALL-UNNAMED --sun-misc-unsafe-memory-access=allow -cp "work/wb-edit-probe;C:/Program Files/LRTimelapse 7/LRTimelapse.jar" lrtlprobe.WBEditOracle work/wb-edit-replay.json

$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run '^TestWBEditInstalledCallbackPrefix$' -count=1
```

Java/the installed app are optional comparison dependencies. Production
editing remains pure Go. The fuzz regression for XML attribute CR
normalization passes input through XML parsing before editing, matching CLI
input rather than arbitrary unparsed in-memory attributes.

## Validation recorded on 2026-10-23

- The full suite with installed LRTimelapse, FFmpeg and ExifTool comparisons
  enabled passed (781.471 s for `internal/lrtl`), including existing video
  regressions and the new installed WB callback-prefix replay.
- The final full suite passed with CGO disabled, optional comparison variables
  unset and FFmpeg absent from PATH (109.345 s for `internal/lrtl`). Optional
  installed-component/codec tests skip in this configuration. The geometry
  rejection test's accidental FFmpeg dependency was fixed by passing the
  existing test backend and asserting it receives no input.
- All 960 installed callback-prefix observations replayed successfully.
- Focused race tests, including installed WB/save-prefix comparisons, passed
  (16.179 s). That run covered 91.7% of statements in `wb_edit.go`; this is
  a focused file coverage result, not whole-project coverage.
- `FuzzWBNumericEdit` passed 123,506 executions in a 60-second fuzz run.
  The initial CR-normalization test error was retained as an explicit seed
  after matching the CLI input-parsing boundary.
- Focused Go 1.23 tests, Go 1.26 `go vet ./...` and a `CGO_ENABLED=0`
  executable build passed.
- The built executable edited three synthetic PNG sidecars and generated
  AVI videos. WB mode, truncation/clamping, inactive-pair removal, single and
  sequence dry runs, original backup bytes, repeat-without-writes and image
  preservation passed. Video frames changed and matched explicit WB controls.

## Remaining differences

The companion defaults model the synthetic controls' initial values. Full
EXIF/CRD original-WB initialization and persistent GUI slider state are not
reproduced. The CLI uses root `lrt:Quality` instead of the app's full image
classification/cache initialization. It does not mark a keyframe, update GUI
state, or run the complete native save workflow. Other numeric controls
retain their existing numeric formatting and validation; their native slider
limits are not added by this change.

The renderer remains an independent approximation of Camera Raw. Absolute
Temperature/Tint affect the basic-render path. Incremental WB rendering is
now available for root `lrt:Quality=FLAT` using the independent sRGB mapping
documented in [frame initialization](lrt-frame-state-7.5.5.md). Image-based
quality classification is available through `init`; numeric editing itself
continues to use the root label.
This does not establish Adobe pixel equivalence or full feature parity.
