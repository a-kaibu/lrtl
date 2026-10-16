# Native LRT reference ordering

`lrt-reference-order-7.5.5.json` records six synthetic sorting observations
from LRTimelapse 7.5.5 build 1059 on 2026-10-17. It contains original test
inputs and numeric permutation outputs, without vendor executable code.

The Java reflection probe initialized `tQa` and `Rsa`, resolved the public
static `tQa.Xa` column, and called the static `tQa.K(int)` returning String.
The observed column name was `xmp:CreateDate`.

For each case, the probe constructed `Rsa(-1)` and inserted
`tQa(Object[4096])` frames in the listed input order. Array slot zero held
the filename, and the resolved date column held each listed date or null.
It invoked the zero-argument `Rsa.M()` returning void and recorded the
original frame indices by object identity after sorting.

Cases cover unequal dates, equal dates with numeric filenames, missing
dates on every frame, timezone offsets, case-insensitive stability,
Greek sigma and Turkish I, supplementary Unicode and BMP filenames.
In particular, the native comparator orders date strings without UTC
normalization, and equal dates fall back to filename text ordering.

These observations cover the comparator's input column and permutation.
They do not establish how LRT originally populates that column from all
possible EXIF containers, or a stable result for partially missing dates.
