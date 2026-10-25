# Keyframe quantity oracle — LRTimelapse 7.5.5

`lrt-keyframe-quantity-7.5.5.json` stores direct results from the installed
LRTimelapse 7.5.5 build 1059 quantity helper. The probe creates synthetic frame
models, supplies their `Preview EV` values and optional 2-star/3-star ratings,
then invokes `cOM7.A(int,int)` from the installed JAR. It does not launch or
display the application window. The checked-in fixture contains the exact input
arrays and returned counts; the regular Go test compares against those values
without requiring Java or LRTimelapse.

The eight cases cover a flat short sequence, the frame-count factor, the strict
50-frame spacing boundary on both sides, the 250-frame minimum-anchor rule,
spacing caps, an adjacent 2-star/3-star pair that cancels a measurement jump,
and a single Holy Grail marker that raises the minimum count without cancelling
the jump. The neighboring unit tests retain additional compact arithmetic and
placement cases.

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
