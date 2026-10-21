# Native radial-mask arctangent

This small pure Go adaptation of Sun fdlibm's
[`s_atan.c`](https://netlib.org/fdlibm/s_atan.c) and
[`e_atan2.c`](https://netlib.org/fdlibm/e_atan2.c) retains their permissive Sun
notice in the source and `LICENSE`. It adds no CGo or runtime dependency.

LRT's radial-mask helper uses Java's arctangent arithmetic. Go's standard
arctangent is based on Cephes and can differ by a last bit. In the measured
square-mask case, that difference contributes to selecting a different
equivalent rotation, changing the whole interpolated path. The package is
used only by the independent radial transition implementation.

`testdata/java-atan2-25.json` contains 2,304 original Java 25 `Math.atan2`
observations as hexadecimal binary64 input/output bits: 256 special-value and
range-boundary pairs, plus 2,048 deterministic SplitMix64 pairs. Go compares
every non-NaN result exactly, including signed zero. NaN results are checked
semantically because payload propagation is not a portable contract.

Reproduce with the original public-API-only probe:

```powershell
javac -d work/atan-classes scripts/probes/AtanOracle.java
java -cp work/atan-classes lrtlprobe.AtanOracle work/java-atan2-25.json
go test ./internal/fdlibm -count=1
```

The separate high-precision sine/cosine calculation used for anchor candidate
selection is project code in `lrtl/mask_radial_precision.go`, not part of this
fdlibm adaptation. It is bounded to the already reduced angle interval.
