# Native timestamp subtitle clock

`subtitles --profile lrt-7.5.5` now implements the native timestamp SRT clock
and source-frame cue selection. The standard profile remains the default and
aligns individual cues to the final Go video timeline. The native profile
builds the SRT against the original sequence, before interpolation, temporal
latency and trim, as the installed timestamp generator does.

## Evidence and numerical behavior

`lrt-timestamp-clock-7.5.5.json` contains 1,344 observations returned directly
by the installed 7.5.5 build 1059 timestamp timecode helper. A reflection-only
probe invoked the helper with an ordinary null UI owner, synthetic frame
indices, fps labels and playback speeds. It did not modify helper bytecode or
licensing state. Only the input/output data is committed; the probe and local
inspection files are excluded from the repository.

The observations cover 12 fps labels, seven speeds from .0625 to 16, and 16
indices from zero through signed 32-bit maximum. They include NTSC labels,
fractional labels, second/minute/day boundaries, long durations and the
native seconds-field integer wrapping behavior. The Go implementation checks
every saved string exactly without requiring the installed application.

The timestamp helper uses its own fps parser: first four characters of the
label, special mappings for 23.9/29.9/47.9/59.9, otherwise a float32 parse.
This differs from the render encoder's rational fps translation. The clock
promotes the float32 fps and speed to double, computes their reciprocal step,
then multiplies the decimal representation by the source index. It truncates
that result to six significant decimal digits before splitting time fields.
Go uses `math/big.Rat` for these decimal operations, with no native dependency.

For example, at 24 fps and speed 1, source frame 3 becomes `00:00:00,124`,
and frame 6 becomes `00:00:00,249`. This preserves the native reciprocal's
decimal truncation rather than rounding these to 125 and 250 milliseconds.
Long sequences can lose subsecond precision. These are measured native
semantics, not a proposed general-purpose subtitle timing convention.

The cue-selection expression was inspected separately in the installed SRT
generator. `--every N` uses source frames, and -1 uses its float32 timestamp
fps. Selection uses a float32 remainder, multiplied by float32 playback speed,
then truncation to an integer equal to zero. Slow speeds can therefore select
adjacent source frames even when N is greater than one. The final original
source count is appended as the last cue endpoint. Interval tests pin these
cases; this selection evidence is distinct from the 1,344 direct timecode
measurements.

## Scope

The Go CLI uses its existing XMP/EXIF metadata precedence, configurable Go date
layout, and strict requirement that all source frames have timestamps. It
does not reproduce native missing-date tolerance, native timezone conversion
or global date/seconds settings. Invalid options, malformed metadata and SRT
delimiter injection retain the existing destination and remove temporary
files. Preset fps/speed and explicit CLI overrides feed the selected clock.

Timestamp burn-in still uses the existing Go font/layout. The native command's
Arial/libass subtitle style and its placement before trim/fade have been
identified; connecting this SRT path to native video overlay remains pending.
This change does not claim full application overlay or font-rendering parity.

```powershell
lrtl subtitles C:\photos\sequence --output C:\photos\native.srt --profile lrt-7.5.5 --every -1 --fps 29.97 --speed 1
go test ./internal/lrtl -run '^TestNativeTimestamp' -count=1
go test ./internal/lrtl -run '^$' -fuzz '^FuzzNativeTimestampClock$' -fuzztime 10s
```
