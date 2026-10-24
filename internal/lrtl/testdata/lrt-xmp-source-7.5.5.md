# File-level XMP source selection, LRTimelapse 7.5.5

## Complete native file-reader observations

`lrt-xmp-source-7.5.5.json` records 76 original synthetic JPEG/JPEG-alias,
DNG/TIFF-header, TIFF/TIFF-alias and PNG cases. Each records image bytes,
optional stem sidecar text, actual filesystem timestamps in milliseconds and
nanoseconds, and the result of the genuine native file-level XMP reader.

The original `scripts/probes/XMPSourceOracle.java` invokes `nNa.A(File)` and
serializes its result. The method completes for all cases; this probe has no
history-stop workaround. No GUI constructor, main object, license state or
proprietary method is replaced. Scope ends at file loading; subsequent application
cache loading and save behavior are separate observations.

`scripts/probes/xmp_source_fixture.py` creates the original inputs using the
standard library and existing original JPEG/PNG fixture bytes. Go tests consume
the frozen data and do not require Python. Cases include optional embedded XMP,
missing/older/equal/newer sidecars, malformed sidecars and submillisecond borders.
TIFF-based DNG inputs exercise metadata loading without RAW pixel data.

## Observed rule

| Image type | Existing stem sidecar | No sidecar |
| --- | --- | --- |
| JPEG / DNG | Sidecar only when strictly newer than image; otherwise image | Embedded image XMP |
| TIFF | Sidecar regardless of relative timestamps | Embedded image XMP |
| PNG | Sidecar regardless of relative timestamps | No packet from this native loader |

JPEG/DNG comparison uses Java File.lastModified milliseconds. Equal timestamps
choose the image. A sidecar 0.8 milliseconds newer within the same millisecond
still loses; crossing the next millisecond changes the choice. The fixture
contains actual nanosecond times for replaying that boundary.

The image choice does not fall back to an older sidecar when embedded XMP is
absent. The native result is then null even if the old sidecar is valid. PNG
cases include a valid XMP iTXt packet to verify that this particular native file
reader still does not adopt embedded PNG XMP.

For malformed selected sidecars, the native reader logs failure and returns
null. lrtl retains its existing fatal parse error, preserving preflight behavior.
The tests match 58 loading outcomes and separately assert these 18 stricter errors
and unchanged files. Malformed older JPEG/DNG sidecars ignored by the timestamp
rule do not cause a parse error.

## Default initialization change

Default `init --frame-state lrt-7.5.5` now applies the JPEG/DNG timestamp rule to
the selected sidecar. Its preview prints `XMP source: image` or `sidecar`.
An image source can contain no XMP; initialization then starts from an empty
packet. Older sidecar-only settings can therefore be replaced by the selected
image metadata when `--write` is requested. Existing sidecar bytes are backed up
unless the existing explicit `--no-backup` option is used. Images are unchanged.

`--frame-state preserve` retains the earlier sidecar-first initialization and
the earlier preparation scope. Other commands continue to use their existing
sidecar-first readers. The existing candidate policy for stem and extension
sidecars is retained; this change applies native timestamp reconciliation to
that selected candidate. The native observations use stem sidecars only.

Initialization writes a canonical sidecar for an ignored existing sidecar even
when the embedded packet is already fully initialized. This makes the selected
state available to subsequent stateless CLI commands. A future-dated image also
sets the minimum saved timestamp: the temporary sidecar is made strictly newer
at native millisecond resolution before atomic rename. The two-second margin
also accommodates coarser filesystem timestamp resolution. The previous
future-sidecar advancement is retained. Successful repeat initialization reads
the newly saved sidecar and creates no additional backup.

## Verification

- Frozen tests replay all 76 image/sidecar/time arrangements and distinguish
  the 58 matching outcomes from the 18 selected-invalid-sidecar errors.
- The installed reader replay reproduced the data in the focused native suite
  (10.356 seconds), including actual submillisecond timestamps.
- CLI tests cover native/preserve profiles, future images, source preview,
  original image retention, exact backups, repeat without writes, fully prepared
  embedded packets with stale sidecars, and a later selected-invalid sidecar
  rejecting the plan before earlier outputs are written.
- Save-helper tests check future timestamp advancement, a newer image threshold,
  exact backups and failure without changing existing files for an invalid parent.
- On 2026-10-24, the full pure-Go suite passed in 121.323 seconds. Focused race
  tests passed in 11.944 seconds and Go 1.23 checks passed in 1.780 seconds.
  Go 1.26 vet and a CGO-disabled executable build passed.
- `FuzzXMPSourceTimestamps` completed 99,174 generated inputs in 45 seconds,
  covering eight image/embedded-packet variants, timestamp differences and
  selected/ignored malformed sidecars. It checks source selection and that image
  bytes, sidecar bytes and modification times remain unchanged during loading.
  Generated inputs are not additional native-reference observations.
- The built executable checked five JPEG/DNG/TIFF/PNG cases, including an image
  without embedded XMP, with preview/no-write checks, exact backup retention,
  future image timestamps and stable repeated initialization.
- A three-frame JPEG video started with sidecar Exposure2012=2. Initialization
  adopted embedded Exposure2012=1. FFmpeg-decoded RGB output changed from the old
  video and matched an embedded-EV1 JPEG control (64x32, SHA-256
  `081d4a4502b633118112b6ca1194052412dc142fd43c9415e40f5e55633808f1`).

## Reproduction

```powershell
go test ./internal/lrtl -run 'TestXMPSource|TestWritePacketNewer|FuzzXMPSource' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzXMPSourceTimestamps$' -fuzztime 45s

$env:LRTL_NATIVE_JAR='C:/Program Files/LRTimelapse 7/LRTimelapse.jar'
go test ./internal/lrtl -run '^TestXMPSourceInstalledFileReader$' -count=1

# Optional regeneration in a NEW directory, with fixed original timestamps.
python scripts/probes/xmp_source_fixture.py work/xmp-source-inputs
```

## Remaining scope

Application-wide source reconciliation, native filename/collision handling,
multiple embedded XMP-directory merging, additional container-reader behavior,
complete frame-cache loading, capture-date updates and history completion remain
separate work. Existing extended embedded-RAW readers outside these measured
native file-reader types retain their prior behavior. Video checks compare the
lrtl renderer with a controlled metadata input; Adobe rendering is outside scope.
