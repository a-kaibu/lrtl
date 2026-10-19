# LRT 7.5.5 mask-angle postprocessing observations

Observed on installed LRTimelapse 7.5.5 build 1059, Java 25, Windows, 2026-10-19.
The JSON contains original synthetic inputs and numeric component observations;
it includes no application implementation or user metadata.

An isolated controller calls the void `K(Rsa)` postprocessing helper. The
sequence mask version is explicitly 1 or 2. One frame contains six registered
angle columns: Circular 1/2 and MaskGroup 1/8/9/10, each at CorrectionMasks[1].
Inputs are absent, -225, -135, -90, -45.001, -45, 0, 45, 45.001, 90, 135, 225.
There are 24 invocations and 144 observed column results.

Version 1 adjusts Circular 1/2; version 2 adjusts MaskGroup 8/9. MaskGroup 1/10
and the inactive layout remain unchanged. Absent cells remain absent. The
active columns add 90 below -45, then subtract 90 above 45, each at most once.
Exact boundaries are unchanged. This is not general angle wrapping: -225
becomes -135, and 225 becomes 135 after one invocation.

The Go tests compare 72 results in packets containing only the corresponding
layout, in three valid RDF representations (216 persisted-value checks). They
include modern slots 1/10 as unaffected controls, own selector IDs, unknown
metadata, serialization/parsing, CLI dry-run/write, original backups, image
bytes and no-op repeats for the values adjacent to +/-45.

The native probe reads the in-memory frame columns, without invoking the GUI,
whole Auto-Transition worker or native XMP writer. The Go side also exercises
its own persistence path. This is evidence for the postprocessing component
and pure-layout Go saving, not complete Auto-Transition or Camera Raw parity.
Mixed-layout tests now compare all six columns in each observation, across
three RDF forms (432 additional checks). They explicitly select legacy for
version 1 and use automatic MaskGroup detection for version 2. Inactive-layout
columns survive unchanged. Copy, missing-column and spline branches also have
dispatch regressions; their numeric spline values use the existing Go sampler
as a control and do not represent additional native spline observations.
CLI/batch tests check first-frame detection, per-sequence choices, backups,
dry runs, image retention, stable repeats and invalid options without writes.
The native format-detection observations are in lrt-mask-format-7.5.5.json.
Aspect-aware radial geometry is separate and remains unimplemented.
