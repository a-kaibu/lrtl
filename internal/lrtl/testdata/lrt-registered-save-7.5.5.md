# Registered-column save prefix, LRTimelapse 7.5.5

## Scope and provenance

`lrt-registered-save-7.5.5.json` contains 864 synthetic observations from the
installed Windows 7.5.5 JAR. Every case records both the first and second save
on the same frame cache. `scripts/probes/RegisteredSaveOracle.java` is an
original reflection-based observer. It loads all registered cells from synthetic
XMP, supplies synthetic private internal-exposure caches, optionally assigns
selected cells from another synthetic frame, and calls the genuine store method.

The method stops at the history stage with a null `LRTimelapseMain.m()`.
The observer asserts that stop and removes the nondeterministic history namespace
from the synthetic result. **The full save does not complete.** No application
main object, license state or proprietary method is replaced. The JSON records
the completed metadata effects before that stop; it is not evidence of a complete
GUI workflow or full frame initialization. No decompiled code is distributed.

The 864 cases cover:

- All 755 numeric columns eligible for selected copying: 612 Double and 143
  Integer columns, among 861 registered columns at indices 5 through 865.
- Missing selected values across four cached Quality states; namespace carry
  between columns, explicit prefixes and repeated saves.
- Thirty combinations of HasSettings and cached rating.
- Legacy reference fields and Photoshop sidecar fields.
- Legacy and modern internal HG/Deflicker/Global slots, role names, newly
  created mask arrays, URI properties and qualified numeric properties.
- Forty-two internal-exposure formatting cases, including signed zero, rounding
  boundaries and adjacent doubles, infinities, NaN and very large values.

## Implemented behavior

The default `copy-settings --save-state lrt-7.5.5` and
`init --frame-state lrt-7.5.5` now run the registered-column save loop.
Present numeric columns are normalized even when they were not selected for
copying. Standard Double values use six decimal places and Integer values use
the registered Java-compatible conversion. String and Boolean cells retain their
cached spelling. Existing WB cleanup and unrounded crop detection still apply.

The native null-cell branch retains the preceding present column's schema URI.
Consequently a missing copied cell does not always delete its corresponding
Camera Raw property. It can instead delete an identically named property under
the preceding schema. An explicitly prefixed root with a different schema is
left alone. For example, clearing ProcessVersion after a present lrt:Quality can
retain crs:ProcessVersion and remove lrt:ProcessVersion. This behavior is retained
for compatibility. The CLI displays final selected values as well as source
cache values. `--save-state preserve` skips this save loop and preserves the
earlier selected-copy behavior.

HasCrop is also written back into the cached column. A second save using that
cache can therefore have a different namespace carry for later empty columns.
Tests compare first and second outputs independently; they do not assume first
save idempotence. Reloading a rounded tiny crop can separately change HasCrop.

Positive cached ratings are written as integers. A nonpositive cached rating
becomes zero when HasSettings is case-insensitively `true`; other spellings such
as `1` do not activate that rule. The observed frame rating setter maps 1 to 4.
Thus the default profile can change an existing rating, including -1 to 0.
HG marking accepts explicit zero as unrated and preserves the original zero
spelling when the lrtl marker is removed.

Internal exposures are saved separately with US `DecimalFormat("0.########")`
semantics: at most eight fractional digits, no trailing zeros, signed zero,
NaN and the infinity symbol. Legacy slots are Gradient indices 5/6/7. Modern
slots default to MaskGroup indices 5/6/7, with mappings from descendant
CorrectionName properties beginning `#LRT internal use`; HG, Deflicker and Global
are detected in that priority order, and the last matching group wins. The native
prefix match also accepts CorrectionNameExtra. Existing target caches/mappings
survive selected copying; an uncached modern mapping is computed after column
writes. Reserved internal slots are not appended when absent.

The save prefix removes `lrt:ReferenceKeyFrame`, ReferenceBoxX1/Y1/X2/Y2,
`photoshop:SidecarForExtension` and EmbeddedXMPDigest. The differently capitalized
ReferenceKeyframe and BoxX0/Y0 remain. Unknown metadata and mask IDs remain.

## Separate formatter observations

`internal-exposure-format-jdk25.tsv` contains 1,426 outputs of the public JDK 25
DecimalFormat API: exponent/mantissa boundaries with both signs and deterministic
random double bit patterns. The original
`scripts/probes/InternalExposureFormatOracle.java` does not load LRTimelapse.
These supplement the 42 internal-exposure cases that actually pass through the
installed LRT save prefix. The Go implementation uses decimal retention,
binary rounding and exact rational intervals for large integral values. This
finite sample is not an exhaustive comparison of all 64-bit patterns or JDKs.

## Reproduction

```powershell
javac -encoding UTF-8 -d work/full-store-probe scripts/probes/MetadataPasteOracle.java scripts/probes/SelectedColumnOracle.java scripts/probes/RegisteredSaveOracle.java
java --enable-native-access=ALL-UNNAMED -cp 'work/full-store-probe;C:/Program Files/LRTimelapse 7/LRTimelapse.jar' lrtlprobe.RegisteredSaveOracle work/registered-save-replay.json
java scripts/probes/InternalExposureFormatOracle.java work/internal-exposure-format-replay.tsv

# Frozen observations and CLI regressions need only Go.
go test ./internal/lrtl -run 'TestRegistered|TestHolyGrailExplicit|FuzzRegisteredSave' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzRegisteredSave$' -fuzztime 45s

# Recompile observers and compare installed-component outputs byte/structure-wise.
$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run 'TestRegisteredSaveInstalledPrefix|TestRegisteredInternalExposureInstalledFormat' -count=1
```

## Remaining scope

Full application frame-cache initialization, `.lrt` cache persistence, image
dimension/aperture/ISO/capture-date save effects, history completion and GUI state
are not reproduced here. Internal exposure cache construction is explicit in the
synthetic observer; it does not prove how every cache is populated by the app.
Other editing commands do not yet share this entire save prefix. Pixel rendering
remains an independent approximation and is not Adobe Camera Raw equivalence.

## Validation on 2026-10-23

- The full pure-Go suite passed in 129.664 seconds.
- The full installed-component suite, including native probes, FFmpeg video
  decoding and metadata comparisons, passed in 824.170 seconds. Supplemental
  focused checks also passed after the final formatter/line-ending adjustments.
- Frozen tests compare all 864 first/second-store observations and verify that
  every eligible numeric registry position is covered, with unique case names.
- Recompiled installed save and public-JDK formatter observers reproduced the
  checked-in observations (7.298 seconds for the focused replay).
- Focused race tests passed (14.313 seconds). CLI tests cover dry runs, the
  preserve profile, unselected numeric normalization, rating effects, namespace
  carry in the final preview, exact backups, original images and source files.
- Focused statement coverage is 89.9% for `registered_save.go` and 82.1% for
  `copy_settings.go`; these figures are not whole-repository coverage.
- `FuzzRegisteredSave` completed 1,020,218 executions; frame preparation completed
  487,724 and the earlier WB/crop component completed 481,392. Each target ran
  for 45 seconds, checking successful XML output/stability and failure without
  packet mutation. These execution counts are generated inputs, not independent
  native-reference observations.
- Focused Go 1.23 tests passed. Go 1.26 vet and a `CGO_ENABLED=0` executable build
  passed, with no new production dependencies.
- The built executable copied selected WB/ProcessVersion caches into three
  synthetic 32x24 PNGs. It checked namespace carry, dry-run preservation,
  unselected numeric rounding, rating zero, source/image preservation and exact
  backups, then generated a pure-Go MJPEG AVI. FFmpeg-decoded RGB bytes matched
  a separate explicit rounded-metadata control for all three frames (SHA-256
  `b3aefa91628bf0204a08baae1286801a078d4a54771fc880c5bf4c86900bf5d9`).
