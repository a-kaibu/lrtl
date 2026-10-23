# Selected-column metadata copying (LRTimelapse 7.5.5)

## CLI

```powershell
lrtl copy-settings --list-properties
lrtl copy-settings reference.xmp SEQUENCE --properties Exposure2012,Contrast2012
lrtl copy-settings reference.jpg SEQUENCE --properties WhiteBalance,Temperature,Tint --write
lrtl copy-settings reference.xmp SEQUENCE --properties 'MaskGroupBasedCorrections[1].CorrectionMasks[1].Left' --write
```

The full-XML copy phase is used when `--properties` is absent. The current CLI
also defaults to WB/crop save preparation; use `--save-state preserve` for the
isolated copying behavior measured below. See
[`lrt-save-state-7.5.5.md`](lrt-save-state-7.5.5.md) for that added stage.
Column selection intersects the existing image selection flags; the properties
are comma-separated, deduplicated by expanded name/path, then processed in the
native registry order. Registered slash paths and the CLI's dot notation are
accepted. Unprefixed names use the Camera Raw namespace. Source prefixes in
the actual XML do not have to match CLI prefixes.

Dry runs show the normalized source values and `remove` for absent cells.
Source-image/sidecar exclusion, hardlink protection, shared-sidecar checks,
whole-plan validation, backup and replacement are shared with full copying.
Source XML, images and unselected destination properties are retained. Selected
missing values delete their destination property; an existing empty string is
different from absence. There is no multi-file rollback on an I/O write failure.

The registry has 866 entries: Double 613, Integer 145, Boolean 30 and String 78.
The native selected-column branch starts at index 5, leaving **861 eligible
columns**. File name, EXIF dimensions, capture time and the external exposure
default are outside that range. XMP rating is not a registered property column.
The current registry has no array-valued columns: global/local curve arrays,
arbitrary structures and unregistered mask positions cannot be selected by
this operation. Existing full copying and curve commands are separate paths.

`internal/lrtl/data/lrt-columns-7.5.5.json` contains observed names, indices and
type labels. It is factual interoperability data, without application code,
private settings or image metadata. It is embedded in the pure Go executable;
Java and an installed application are only optional test dependencies.

## Native evidence and scope

The inspected caller `cOM7.A(tQa,tQa,boolean,boolean)` has two branches. The
false/full flag branch loops from cell 5 to the destination cache length, checks
the column header's selection state, then assigns the source cell obtained by
`tQa.a(int): Object` with `tQa.a(int,Object): void`. It does not interpolate or
apply slider bounds. The true branch is the separately implemented all-schema
XML paste. No GUI controller or license object is constructed or replaced by
the observation harness.

The original [`SelectedColumnOracle.java`](../../../scripts/probes/SelectedColumnOracle.java)
initializes the registry and invokes:

- `Ooa.A(): Ooa`, `Ooa.A(): int`, `Ooa.a(int): String`,
  `Ooa.A(int): Class` for registry facts.
- Private `tQa.A(String,String,Wd,int,boolean): void` for typed cell loading.
- The public cell getter/setter for copying its cached value.
- `tQa.A(Rsa,ArrayList,boolean): void` with a synthetic single output column
  named `ProbeValue` to observe the stored scalar format. Numeric formatting is
  based on the value object's type, independently of that synthetic name.

**The complete native store call does not finish outside the application.**
It writes the test cell first, then its later history stage raises a null main
application error (`LRTimelapseMain.m()`). The harness reads the already written
test cell, asserts that known later error, and records `save_completed: false`
and `post_store_error`. These are **cell load/store measurements**, not evidence
that the entire GUI save/paste workflow succeeded. The application/library and
its licensing state are not patched to make that call finish.

`lrt-selected-columns-7.5.5.json` contains all 866 registry definitions and
1,001 synthetic source/cell/stored-value observations. Every eligible column
has a measurement; additional cases cover missing/empty values, half decimal
rounding, large magnitudes, negative zero, hex literals, suffixes, Unicode BMP
digits, invalid input, non-finite values, and 64 seeded IEEE double samples.
Native history IDs are not included in this reference.

## Typed value behavior

- **Double:** Java-style ASCII whitespace, leading-plus cleanup, decimal/hex
  and `f`/`d` suffix syntax. Invalid numeric strings become zero, as observed
  in the frame loader. Stored finite values use the native six fractional
  digits and decimal half-up rounding. Negative zero is retained. `NaN` and
  infinities retain the native text spelling. No slider-range clamp is applied.
- **Integer:** first try a signed 32-bit integer, including BMP decimal digits
  accepted by Java's integer parser. If that fails, load a double and apply
  Java `Math.round` followed by narrowing to 32 bits. Thus -12.5 becomes -12;
  overflow can wrap rather than clamp. Invalid input becomes zero.
- **String/Boolean:** the observed cell loader keeps the text as a string.
  Empty, whitespace and original capitalization are preserved. Copying does
  not reinterpret a Boolean column through the CLI's slider/flag editor.
- **Absent property:** a null cell removes the selected destination property.
  The source's other schemas are not appended in this mode.

The Go implementation uses a bounded scalar path grammar and expanded
namespace names. It supports `crs`, `crd`, `lrt` and the other registered
namespaces. XML parsing and root replacement reuse the full-copy semantic
model; unchanged root properties retain their original representation.

## Parent arrays and partial metadata

The native store component prepares the last indexed array in a property
path, appending at most ten struct items before assigning the leaf. A new
array uses Bag; existing Bag/Seq/Alt form and items are retained. This can
create an absent outer correction array and its selected record, but it cannot
create a nested mask array when its outer indexed parent is missing. Merely
appending an implicit untyped item is insufficient for named children.

`lrt-selected-column-paths-7.5.5.json` records nine native parent-preparation
observations. They include creation of outer correction/RangeMask structures,
an empty-to-index-11 failure, missing outer parents for a nested mask array,
existing nested parents and failures across gaps. Later history errors are
retained as provenance. Native history is removed from the recorded output
with the schema-removal helper so regeneration is deterministic.

Those tests compare creation of the **selected array branch**. The native
store also writes its separate cached HG/deflicker defaults into sibling items;
that unrelated postprocessing is not a selected-cell assignment and is not
claimed by this comparison. Independent tests ensure that the CLI retains
recipient IDs, other mask coordinates and other selected-frame metadata.

The CLI plans all changes before writing, so native preparation failures become
errors without partial XML or earlier sidecar changes. For incomplete target
sequences, prepare the structures with `init --write` or full metadata copying.
The command does not invent missing IDs, map masks by ID, copy an entire mask
shape for one scalar selection, or reinterpret reserved/unregistered slots.

## Remaining differences from the complete application

Full frame initialization and cached default synthesis have not been ported
into this command. WB/crop numerical serialization, WB cleanup, derived HasCrop
and cached quality writeback are now covered by the separate default save-state
stage. HG/deflicker state, other number columns, rating/date side effects,
history and dirty/UI bookkeeping remain outside it. The column observations
alone do not imply those earlier/later stages. Select the necessary WB columns
together; the default profile derives crop activation from geometry, while
`--save-state preserve` retains the isolated column-copy behavior. The existing
renderer remains approximate and does not apply IncrementalTemperature/Tint.

## Tests and reproduction

Normal tests cover all 1,001 typed observations, every eligible existing path,
aliases and native column order, null deletion, ordinary repeat/idempotence,
parent preparation, no mutation on failure, reference exclusion, image/filter
selection, shared outputs, backups and AVI frame bytes against an independent
exposure-only control. Fuzzing checks source immutability, destination retention
on errors and semantic idempotence after serialization.

```powershell
go test ./internal/lrtl -run '^(TestSelectedColumn|FuzzSelectedColumns)' -count=1
go test -race ./internal/lrtl -run '^(TestSelectedColumn|TestCopySettings|TestMetadataPaste|FuzzSelectedColumns)' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzSelectedColumns$' -fuzztime 60s -parallel 2

javac -encoding UTF-8 -d work/metadata-copy-native/classes scripts/probes/MetadataPasteOracle.java scripts/probes/SelectedColumnOracle.java
java --enable-native-access=ALL-UNNAMED -cp "work/metadata-copy-native/classes;C:/Program Files/LRTimelapse 7/LRTimelapse.jar" lrtlprobe.SelectedColumnOracle work/selected-columns-replay.json
java --enable-native-access=ALL-UNNAMED -cp "work/metadata-copy-native/classes;C:/Program Files/LRTimelapse 7/LRTimelapse.jar" lrtlprobe.SelectedColumnOracle work/selected-columns-registry.json --registry
java --enable-native-access=ALL-UNNAMED -cp "work/metadata-copy-native/classes;C:/Program Files/LRTimelapse 7/LRTimelapse.jar" lrtlprobe.SelectedColumnOracle work/selected-column-paths.json --paths

# Replays the installed registry and cells, then parses Go packets with LRT.
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run '^TestSelectedColumnsInstalledComponents$' -count=1
```

The optional test compares registry definitions and source/cell/stored facts,
then feeds Go-generated results through the installed parser/serializer and
compares complete packet semantics. It requires the user's installation and
compatible Java/Javac on PATH. No proprietary binary is included in the repo.
The registry and obfuscated descriptors are specific to the measured version.

## Initial column-mode validation, 2026-10-23 (`da97960`)

- All 1,001 component cases, nine parent-preparation cases and installed-parser
  round trips passed. Eight additional JVM Formatter cases cover large decimal
  aliases and carry across the six-digit rounding boundary.
- Focused tests passed on Go 1.23 and 1.26; the race detector passed with the
  installed registry/column replay and native XMP parser enabled.
- A 60-second fuzz run completed 1,140,724 executions without a failure.
- The Go 1.26 full suite with installed FFmpeg, ExifTool and XMP comparisons
  passed: `go test -json ./... -count=1 -timeout 20m` (750.448 seconds for the
  main package). The final ordering and large-number regression tests also
  passed in focused race runs after the full suite had started.
- Focused file coverage: `selected_columns.go` 236/264 statements (89.4%),
  `copy_settings.go` 131/152 (86.2%) and `xmp_paste.go` 349/365 (95.6%). These
  figures are for the listed files, not repository-wide coverage.
- `go vet ./...` and a `CGO_ENABLED=0` executable build passed. The executable
  copied only exposure on three synthetic PNGs, retained recipient temperature
  and contrast, preserved one original backup per file, made no changes on
  repeat, and produced AVI frames equal to an explicit exposure-only control.
