# Synthetic JPEG inputs

All JPEG images here are generated RGB/gray arithmetic patterns, not application
binaries or camera photographs. `generate.py` uses Pillow 12.3.0 only to create
test inputs; Pillow/Python is not needed to build, run or test lrtl.

Dimensions: 64×32 and 17×13. RGB channels are `(13x+31y) mod 256`,
`(20x+17y) mod 256`, `(27x+3y) mod 256`; gray is `(19x+43y) mod 256`.
The larger RGB fixture has JPEG qualities 30/75/95, the smaller quality 75.
Subsampling values 0/1/2 are 4:4:4/4:2:2/4:2:0. Both baseline and progressive
forms are included; gray fixtures use quality 75.
Four more RGB 4:2:0 inputs exercise block/row restart intervals in baseline
and progressive scans, bringing the fixture set to 32 images.

`ffmpeg-8.0.1.json` records SHA-256 hashes and byte counts obtained directly
from the installed LRT 7.5.5 FFmpeg 8.0.1, separately for raw decoded planes
and full-range `yuv444p10le` working planes after an unscaled Lanczos conversion.
The Go decoder/kernel did not generate those reference outputs.
