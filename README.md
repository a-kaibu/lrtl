# LumaFrame XMP tools (`lrtl`)

An independent, open-source starting point for interoperable timelapse keyframe editing. The first milestone focuses on Lightroom-compatible XMP sidecars: inspect a sequence, mark keyframes, and linearly interpolate an explicit set of scalar Camera Raw controls.

This project is not affiliated with or endorsed by LRTimelapse or Adobe. It does not include their code, artwork, or bundled tools.

## Current scope

- Find common still-image formats in one folder and sort names naturally (`frame2` before `frame10`).
- Read and write external `.xmp` sidecars while retaining unknown RDF properties.
- Mark a normal keyframe with `xmp:Rating=4` or a crop keyframe with `xmp:Rating=5`.
- Linearly interpolate selected numeric `crs:` develop controls between keyframes, including legacy controls such as `Exposure` and `RedHue` and newer controls such as `Exposure2012` and `Highlights2012`.
- Make a timestamped backup before each write by default.
- Preview interpolation without changing files; pass `--write` to apply it.

The current version does not render RAW files or video, analyze image luminance, understand embedded XMP, or reproduce LRTimelapse's masks, tone curves, deflicker, Holy Grail compensation, or UI. Its interpolation is based on natural filename order. Numeric controls outside the explicit whitelist are left untouched; categorical, boolean, circular color-wheel hue, array, and structured properties need different interpolation rules. Where the first or last image already has a numeric setting, it uses that value as an edge anchor and preserves the endpoint XMP.

## Install

Requires Go 1.23 or newer to build. The XML library is pure Go, so the compiled CLI has no native runtime dependencies.

```powershell
go install github.com/a-kaibu/lrtl/cmd/lrtl@latest
```

## Commands

```powershell
# Inspect images, sidecars, and detected keyframes
lrtl scan "D:\Timelapse\Sequence"

# Mark a frame (writes a backup sidecar first)
lrtl mark "D:\Timelapse\Sequence\IMG_0005.CR3" --kind default
lrtl mark "D:\Timelapse\Sequence\IMG_0012.CR3" --kind crop

# Preview a transition, then explicitly apply it
lrtl interpolate "D:\Timelapse\Sequence"
lrtl interpolate "D:\Timelapse\Sequence" --write

# Undo a marker created by lrtl, restoring the prior Lightroom rating
lrtl unmark "D:\Timelapse\Sequence\IMG_0005.CR3"
```

`unmark --force-rating` is available for an existing four- or five-star XMP marker that was not created by this tool. It removes that rating, so use it only when that rating is serving as the keyframe marker.

## Development

```powershell
go test ./...
go build -o lrtl.exe ./cmd/lrtl
```

See [reverse-engineering notes](docs/reverse-engineering.md) for the compatibility observations and known gaps that guide this milestone.

## License

MIT. See [LICENSE](LICENSE).
