# Native preview JPEG fixture

`lrt-native-preview-7.5.5.jpg` is an original synthetic gradient, encoded by
the installed LRTimelapse 7.5.5 (2026-09-14), build 1059 preview encoder.
It contains no vendor photograph or executable code.

The input was a Java `BufferedImage(32, 16, TYPE_INT_RGB)`, filled with:

```java
for (int y = 0; y < 16; y++) {
    for (int x = 0; x < 32; x++) {
        image.setRGB(x, y, ((40 + x * 5) << 16) |
                           ((20 + y * 8) << 8) | (200 - x * 3));
    }
}
```

Reflection selected the static `tQa` method returning `byte[]` and accepting
one `BufferedImage` argument. Its result was saved unchanged: 672 bytes,
JPEG header `ff d8 ff e0`. No preview quality setting was changed by the probe.

Calling the static methods of `lrtimelapse.LRTimelapseMain` returning `File`
with one `File` argument on `work/native-preview-probe/frame.001.NEF` gave:

- `h(File)`: `work/native-preview-probe/.lrt/previews/frame.001.lrtpreview`
- `j(File)`: `work/native-preview-probe/.lrt/visual/frame.001.lrtpreview`

These observations establish the locations and JPEG encoding used here.
They do not establish freshness, camera/visual pixel equivalence, Adobe
color management equivalence, or a native Visual Preview rendering pipeline.
