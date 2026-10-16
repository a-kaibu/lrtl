# LRT 7.5.5 Deflicker sequence settings observations

The JSON contains six synthetic settings records serialized by the installed
LRTimelapse 7.5.5 build 1059 runtime. No image, user project, application code,
license data, or executable is included.

For each of accuracy -1/0/1 and multipass false/true, a new
`lrtimelapse.settings.SequenceProperties` object was given known values:
smoothing .25/.5/1.1 respectively, constant enabled only for accuracy 1,
three passes, five images, and filename sorting enabled. The same runtime JSON
serializer used by the native sequence save path produced the records. Only
version/build and the relevant settings fields are retained here.

This verifies field names, JSON types, and numeric/boolean persistence. It does
not assert that importing the controls reproduces native RAW development or
the native multipass workflow. Existing worker calculation observations cover
the numerical correction separately.

The sequence flag `deflickerIgnoreBlacksWhites` chooses global defaults (0),
enabled (1), or disabled (2). The actual black/white percentages and hard-cut
preference live in global LRT settings, not these records. The CLI requires
explicit histogram options for mode 1 and rejects them for mode 2. For mode 0
or an absent flag, it uses the command's explicit options or its normal default;
it does not read installed LRT global preferences.

Tests compare imported controls with explicit CLI controls, including parallel
batch operation, date ordering and saved ROI, and reject incomplete/invalid
settings before saving sidecars. Missing controls are rejected rather than
substituting the developer's local LRT preferences. Pass counts are interpreted
only while multipass is enabled; the CLI currently supports up to eight passes.
