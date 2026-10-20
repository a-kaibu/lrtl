# LRT 7.5.5 range-filter observations

On 2026-10-20, an isolated `mQa` rendering component with a null UI reference
invoked its private RenderPreset/String color-filter builder. The installed
runtime is 7.5.5 build 1059, Java 25, Windows. Source label is synthetic BT709;
constructor-default SDR/gamut controls remain unchanged. videoRange values
0/1/2 are supplied through reflection, without modifying license/development
state, opening GUI, reading user media or running the complete native export.

The three original emitted-filter observations show rangein=full and output
range=full exactly when videoRange==1; otherwise range=limited. Matrix,
primaries and transfer identifiers remain 709. Go matches that range selection,
including the observed fallback for value 2. The native input at this stage is
already YUV; lrtl's explicit range path starts with its own RGB48 input, using
gbrp16le and zscale matrix GBR->709, and labels the output matrix as BT709.
It does not claim the complete native color-managed transformation.

Go's auto option preserves the former pipeline without zscale or new tags.
Explicit limited/full (including saved native settings) require FFmpeg zscale.
MOV/MP4 colr writing is enabled for explicit ranges so that ProRes also retains
the matrix label; transfer and primary declarations are not invented.

Actual FFmpeg 8.0.1 tests use original 256x128 RGB16 frames at 0/65535/32768.
H.264/H.265 outputs report tv/pc and produce Y=16/235/126 or 0/255/128.
DNxHR HQX produces 64/940/502 or 0/1023/512 with tv/pc. ProRes 422 produces
64/940/502 for limited, but full endpoints are clipped to 4/1019 around mid
512; ffprobe reports no range, even with explicit container color metadata.
These encoder/container limits are documented, not hidden by an auto fallback.
The tests extract Y directly before any decoder-to-RGB range conversion.

Regression tests cover raw RGB16 retention, all backend argument paths,
matrix/range signaling, saved preset and CLI precedence, batch forwarding,
invalid options without overwriting files, AVI full-range payload/index
retention and rejection of limited AVI. Native full-export pixel equivalence,
ICC/gamma/gamut/HDR behavior and complete player interoperability remain
unverified. Optional actual-video tests skip if FFmpeg/ffprobe or zscale is
unavailable; component and argument tests require only Go.
