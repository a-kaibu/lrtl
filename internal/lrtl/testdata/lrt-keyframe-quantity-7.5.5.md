# Keyframe quantity oracle — LRTimelapse 7.5.5

`lrt-keyframe-quantity-7.5.5.json` stores direct results from the installed
LRTimelapse 7.5.5 build 1059. The probe creates synthetic frame models, supplies
their `Preview EV` values and optional ratings, then invokes `cOM7.A(int,int)`
the public `cOM7.A()` measurement-range helper, `cOM7.A(int,int)` quantity
helper, and the wizard's `cOM7.A(Rsa,int,int[],int,boolean)` helper from the
installed JAR. It does not launch or display the application window. The
fixture contains eight named cases, a 747-case quantity boundary sweep, and
eight before/after rating cases. Go tests compare the measured range, quantity,
and selected indices without requiring Java or LRTimelapse.

The quantity cases cover a flat short sequence, the frame-count factor, the
strict 50-frame spacing boundary on both sides, the 250-frame minimum-anchor
rule, spacing caps, an adjacent 2-star/3-star pair that cancels a measurement
jump, and a single Holy Grail marker that raises the minimum count without
cancelling the jump. The placement cases cover 4/5-star targets, clearing and
replacing the requested rating, shifts to the nearest free preceding frame, the
ninth-frame boundary, and no available preceding slot.

The boundary sweep uses 23 sequence lengths from 2 to 2500 frames, targets
immediately below, at, and above integer quantities, plus the 50-frame spacing
cap and 250-frame minimum-count boundaries. It includes positive and negative
Preview EV offsets, adjacent 2*/3* pairs in each rating order, same-star pairs,
nonadjacent markers, an intervening 4-star mark, and single 2*/3*/4*/5* marks.
The sweep also stores the native corrected measurement range. That comparison
found an off-by-one source range in lrtl: LRT seeds the first measurement but
starts updating its minimum and maximum with frame 2. Frame 1 can still supply
the previous value for an adjacent HG-pair correction. lrtl now follows that
ordering before applying the count formula.

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
loader. It does not modify the installed JAR or add it to the repository. These
are direct helper comparisons: they do not verify LRT's Preview EV image
generation, selected-row mode, full wizard UI, or the complete XMP save flow.
