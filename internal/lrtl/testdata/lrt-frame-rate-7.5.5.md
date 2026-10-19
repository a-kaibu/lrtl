# LRT 7.5.5 frame-rate label observations

Nine original synthetic label inputs were passed to the installed public static
`mQa` String-to-float frame-rate helper on 2026-10-19 (7.5.5 build 1059, Java 25,
Windows). The JSON records the returned Float string values. No GUI, license
state modification, native video export or user media was involved.

23.976, 29.97, 47.952 and 59.94 labels return the single-precision values of
24000/1001, 30000/1001, 48000/1001 and 60000/1001 respectively. Integer controls
are 24, 25, 30, 48 and 60. Go tests compare all nine values at float32 precision.
The installed RenderPreset default also stores the 29.97 label, motivating
support for these rates in the CLI instead of its former integer-only parser.

lrtl stores and exports exact reduced fractions. Decimal NTSC labels (including
trailing zeros) use the /1001 aliases. Explicit fractions are always literal:
2997/100 remains exactly 29.97, while 30000/1001 is the NTSC rate. Other decimal
rates such as 24.5 remain exact decimal fractions. Accepted rates are 1..240,
with reduced numerator/denominator fitting unsigned 32-bit AVI timing fields.
The broader arbitrary-fraction input syntax is a lrtl capability, not a claim
about the native helper's accepted labels or its full export command builder.

Tests compare generated AVI stream scale/rate, rounded main-header interval,
unchanged JPEG payload/index bytes, source retention, batch forwarding, SRT
alias equivalence and overflow-safe long-sequence timing. FFmpeg argument tests
cover H.264/H.265/ProRes/DNxHR with audio trimming. Optional actual FFmpeg/ffprobe
tests check AVI/H.264 MP4/ProRes MOV frame rate, count and duration, plus a short
audio input padded to a 30-frame NTSC video's duration. These tests pass with
the installed tools; they skip when either executable is absent.
