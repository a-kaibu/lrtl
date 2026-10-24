# Native metadata paste XML phase (LRTimelapse 7.5.5)

## Scope

`copy-settings SOURCE_IMAGE_OR_XMP DIRECTORY` implements the XML phase of
full metadata paste. The current CLI also performs a separate default
[WB/crop save-state stage](lrt-save-state-7.5.5.md); use `--save-state preserve`
to reproduce the isolated XML behavior described here. The source is read once; its image and effective sidecar
are excluded from writes. Output is a sidecar, including for images whose
starting XMP is embedded. The implementation and dependencies are pure Go.
Java and an installed application are needed only for optional comparison tests.

The native caller removes the destination Camera Raw schema, then calls an
all-schema append helper with `doAllProperties=true`, `replaceOldValues=false`.
This **can copy capture information** when the destination lacks it. Existing
non-Camera-Raw simple values, including empty strings, take precedence.
This is not a develop-only clipboard operation.

The full native caller subsequently reparses/reinitializes its frame model,
including mask/default metadata. The CLI now implements its
[date-cache and HasSettings reload](lrt-full-paste-date-7.5.5.md), and registered
save preparation, but complete mask/default reconstruction and cache reload
remain unfinished. These XML comparisons alone do not cover those stages. Selected-column
copy is a separate `--properties` mode described in
[`lrt-selected-columns-7.5.5.md`](lrt-selected-columns-7.5.5.md).
GUI clipboard state, native dirty flags and self-paste alias behavior are also
outside this feature. CLI references are immutable snapshots.

## Measurements

`lrt-metadata-paste-7.5.5.json` contains 42 synthetic input/output observations,
each with first-paste `Expected` and a second in-memory paste `Repeated`.
Inputs are original generated RDF packets; no user images, private settings,
decompiled source or proprietary binaries are included.

The original reflection harness is
[`MetadataPasteOracle.java`](../../../scripts/probes/MetadataPasteOracle.java).
It initializes the metadata registry, calls the two XML helpers, and serializes
the result. Reflection selects methods by name, return type and parameter types
because the obfuscated binary contains overloads that ordinary source-style
name lookup cannot distinguish. It does not invoke the clipboard, GUI workflow,
licensing routines, or preference persistence.

Observed classes/method descriptors for this installed version:

- Registry initialization: `Class.forName("tQa")`.
- Parser: `ABA.A(String): Wd`.
- Schema removal: `xOa.A(Wd, String): void`.
- Append: `ieA.A(Wd, Wd, boolean, boolean): void`.
- Serializer: `ABA.A(Wd, bCA): String`, options `new bCA(16)`.
- Full-paste caller inspected separately: `cOM7.A(tQa,tQa,boolean,boolean)`.

The cases cover scalars, missing capture fields, empty source/destination,
recursive structs, Bag/Seq/Alt, language alternatives, URI versus literal
items, qualified values, nested arrays, Camera Raw curves and modern masks,
DC normalization, empty item/field pruning, prefix collisions, alternate Camera
Raw prefixes, and different `rdf:about` subjects.

## Observed merge rules

- All destination `http://ns.adobe.com/camera-raw-settings/1.0/` properties
  are removed before append, including destination-only and unknown properties.
- Other namespaces keep existing ordinary simple values. Missing properties
  are copied from any source schema, including EXIF, XMP, TIFF and LRT.
- Structs recursively add missing fields. Type/option mismatches keep the
  existing value. Existing fields are not overwritten by source scalar values.
- Existing ordinary arrays append items not already matched. Scalar match
  distinguishes URI from literal and compares language, but custom simple
  qualifier values do not distinguish otherwise matching items. Struct items
  need the same field count and matching named fields. Nested array matching
  is asymmetric: every source item must be found in the destination item;
  destination extras and order do not prevent a match.
- Language alternatives retain the existing text for each language and add
  missing languages. A new `x-default` is inserted first. Language tags are
  normalized, for example `en-us` becomes `en-US`.
- DC contributor/language/publisher/relation/subject/type normalize to Bag;
  creator/date to Seq; description/rights/title to AltText. Simple properties
  become single-item arrays, with `x-default` for AltText. AltText repair adds
  `x-repair` to nonempty unqualified simple items and drops empty unqualified
  or composite items. Its serialized language is subsequently normalized.
- Empty source scalars, fields and array items are pruned recursively, even
  when cloning a missing property. Parents emptied by that pruning are omitted.
  Existing destination empty values are preserved. Duplicate nonempty items
  in an entirely new array are preserved; the existing-array merge path has
  separate matching behavior.
- Matching happens before source struct-item empty fields are pruned. As a
  result, `clone-array-struct-empty` gains another otherwise identical pruned
  struct on the second paste in the actual native helper. The reference records
  this behavior; it is not valid to assert universal paste idempotence.
- Prefixes and XML representation may change; matching uses expanded names.
  Destination `rdf:about` is retained.

One native serialization anomaly is deliberately represented semantically:
`new-qualified-language` emits both an outer `xml:lang` attribute and a nested
`xml:lang` qualifier of the same value. Feeding that exact native text back to
its own parser fails with a duplicate qualifier error. Go emits one language
qualifier, and the installed parser accepts the Go packet. Tests collapse equal
duplicate language qualifiers for comparison and reject conflicting qualifiers.

These observations do not establish support for every possible RDF/XMP form,
all private schemas, native frame initialization, or Lightroom pixel output.

## CLI and preservation tests

`xmp_paste_test.go` compares all 42 first and second results as semantic trees,
checks source immutability and serialization/reload, and independently asserts
capture/rating retention and Camera Raw replacement. Duplicate metadata is
rejected before mutation. Fuzzing checks source immutability, no mutation on
failure, and semantic serialization round trips for bounded packets.

`copy_settings_test.go` covers dry runs, ratings, selection JSON and inversion,
reference-image exclusion, embedded references, newest sidecar precedence,
empty explicit XMP, missing reference metadata, late invalid targets, shared
sidecars, hardlinked references, one backup per output, ordinary repeat paste,
`--no-backup`, help, and generated AVI frame bytes against explicit settings.

All candidates are validated before writes. Each output uses the existing
temporary-file replacement and backup mechanism. A later filesystem write
failure does not roll back already saved sidecars; this is not a multi-file
transaction. Images and the source reference are not edited.

For the initial full-XML implementation (`72fed3b`), local validation on
2026-10-23 passed the focused tests on Go 1.23 and 1.26,
the race detector with installed-parser comparison, a 60-second fuzz run
(655,900 executions), `go vet ./...`, and a `CGO_ENABLED=0` executable build.
The focused coverage profile measures 347/364 statements (95.3%) in
`xmp_paste.go` and 96/113 (85.0%) in `copy_settings.go`; these are file-level
figures, not repository-wide coverage. The native observation replay produced
the same bytes as the committed 42-case reference file.
The Go 1.26 full suite with installed FFmpeg, XMP parser and ExifTool comparisons
also passed (`go test -json ./... -count=1 -timeout 20m`, 764.196 seconds for
the main package). The additional malformed-input and representation tests
were included in the focused race and Go 1.23 runs after that full run started.

## Reproduce

```powershell
javac -encoding UTF-8 -d work/metadata-copy-native/classes scripts/probes/MetadataPasteOracle.java
java --enable-native-access=ALL-UNNAMED -cp "work/metadata-copy-native/classes;C:/Program Files/LRTimelapse 7/LRTimelapse.jar" lrtlprobe.MetadataPasteOracle work/metadata-paste-replay.json

go test ./internal/lrtl -run '^(TestMetadataPaste|TestCopySettings|FuzzMetadataPaste)' -count=1
go test -race ./internal/lrtl -run '^(TestMetadataPaste|TestCopySettings|FuzzMetadataPaste)' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzMetadataPaste$' -fuzztime 10s -parallel 2

# Optional: parse and serialize all Go-generated first-paste packets with LRT.
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run '^TestMetadataPasteInstalledParser$' -count=1
```

The optional test compiles the original harness in a temporary directory, then
uses its `--roundtrip DIRECTORY` mode. Round-trip outputs use `CREATE_NEW`.
Installed Java/Javac must be available on PATH. Native obfuscated method names
and observations are version-specific; regenerate and inspect before applying
these expectations to a different application version.
