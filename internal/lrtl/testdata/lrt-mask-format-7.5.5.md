# LRT 7.5.5 sequence mask-format detection

Observed with installed LRTimelapse 7.5.5 build 1059 on Windows, Java 25,
2026-10-19. The probe creates an isolated sequence, an opaque synthetic NEF
placeholder and an adjacent valid XMP sidecar. It invokes `Rsa.A(File)` with
the image path, then `Rsa.K()` for the effective version. The in-memory global
defaultMaskVersion is explicitly 2 to exercise fallback without opening UI.
No global preference file is saved. These are original synthetic observations,
not application code or user metadata.

| Case | First image's sidecar content | Detected | Effective |
|---|---|---|---|
| 0 | No correction properties | -1 | 2 |
| 1 | Empty Gradient Seq | 1 | 1 |
| 2 | Empty Circular Seq | -1 | 2 |
| 3 | Empty Paint Seq | -1 | 2 |
| 4 | Empty MaskGroup Seq | 2 | 2 |
| 5 | Empty Gradient and MaskGroup Seq | 2 | 2 |

The folder caller selects its first image for this detector. Go tests compare
the six effective values, explicit overrides and first-frame-only selection.
The probe does not decode RAW pixels, load a full folder or invoke a GUI. Go
keeps its existing image ordering and sidecar precedence. Its default is modern
when detection is absent; `--mask-format legacy` can explicitly choose the
other preference without importing or changing native global settings.
