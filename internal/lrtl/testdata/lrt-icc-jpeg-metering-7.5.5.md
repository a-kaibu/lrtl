# LRT 7.5.5 JPEG ICC metering

The JPEG metering path was compared with the installed LRTimelapse 7.5.5
(build 1059) on Windows 11 with Eclipse Temurin Java 25. The sample is a
53x31 JPEG encoded from `native-deflicker-measure-02.jpg` and tagged with a
synthetic Display P3 D65 matrix/TRC ICC profile. The profile was changed from
the embedded CC0 sRGB profile's primaries while retaining its sRGB transfer
curves.

The sample was also tested with only its ICC APP2 segment removed. LRT's AWT
JPEG decode produced the same PNG bytes in both cases (SHA-256
`6e69eb845d67776fd0e93377fe20f5e738649213a26f774913b2985807f7b3a0`). Its
Native Deflicker and static histogram measurements both returned
`0.5091265317554522` EV for each version. The tagged and untagged JPEG hashes,
decoded raster, and measurement are recorded in
[`lrt-icc-jpeg-metering-7.5.5.json`](lrt-icc-jpeg-metering-7.5.5.json).

This establishes that LRT 7.5.5 does not apply this embedded ICC profile in
the tested JPEG metering path. `TestNativeDeflickerJPEGICCIgnoredByLRT` checks
the embedded profile description, removes only its APP2 segment, verifies the
resulting JPEG hash, and asserts identical Go metering pixels and EV. It also
checks the saved LRT raster hash and its EV oracle. Other ICC profile types,
camera JPEGs, and encoder combinations remain unverified.
