# Keyframe quantity oracle — LRTimelapse 7.5.5

`lrt-keyframe-quantity-7.5.5.json` stores direct results from the installed
LRTimelapse 7.5.5 build 1059. The probe creates synthetic frame models, supplies
their `Preview EV` values and optional ratings, then invokes `cOM7.A(int,int)`
and the wizard's `cOM7.A(Rsa,int,int[],int,boolean)` helper from the installed
JAR. It does not launch or display the application window. The checked-in
fixture contains eight exact quantity results, eight selected-index results,
and eight before/after rating cases. The regular Go tests compare these values
without requiring Java or LRTimelapse.

The quantity cases cover a flat short sequence, the frame-count factor, the
strict 50-frame spacing boundary on both sides, the 250-frame minimum-anchor
rule, spacing caps, an adjacent 2-star/3-star pair that cancels a measurement
jump, and a single Holy Grail marker that raises the minimum count without
cancelling the jump. The placement cases cover 4/5-star targets, clearing and
replacing the requested rating, shifts to the nearest free preceding frame, the
ninth-frame boundary, and no available preceding slot.

One observed behavior is intentionally kept as a safety difference: LRT's
placement helper overwrites an endpoint 2-star/3-star Holy Grail rating when it
places an ordinary keyframe there. lrtl preserves the Holy Grail rating at that
endpoint. The Go test asserts both the installed LRT output and this documented
lrtl guard behavior. A separate CLI test writes a sequence with both kinds of
ratings, verifies the replacement positions, backup creation, unchanged source
images, and stable results on a repeated run.

To regenerate the fixture on Windows with the same LRT installation:

```powershell
$env:JAVA_HOME = 'C:\Program Files\Eclipse Adoptium\jdk-25.0.1.8-hotspot'
New-Item -ItemType Directory -Force "$env:TEMP\lrtl-keyframe-oracle" | Out-Null
& "$env:JAVA_HOME\bin\javac.exe" -d "$env:TEMP\lrtl-keyframe-oracle" scripts\probes\LRTKeyframeQuantityOracle.java
& "$env:JAVA_HOME\bin\java.exe" -cp "$env:TEMP\lrtl-keyframe-oracle" LRTKeyframeQuantityOracle "$env:TEMP\lrt-keyframe-quantity.json" 'C:\Program Files\LRTimelapse 7\LRTimelapse.jar'
```

The probe copies the installed JAR to the temporary directory without its
signature metadata so Java can load the obfuscated classes through one class
loader. It does not modify the installed JAR or add it to the repository. The
fixture checks the count helper only; Preview EV image generation, wizard row
selection, and final XMP persistence have separate compatibility limits.
