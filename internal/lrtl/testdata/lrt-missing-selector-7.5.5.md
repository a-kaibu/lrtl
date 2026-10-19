# LRT 7.5.5 missing first-selector observations

Forty synthetic observations execute the installed 7.5.5 build 1059 ordinary
column missing-value helper and frame-to-XMP writer. No application code, image,
or user metadata is included. The fixture contains values read back from the
runtime XMP objects, plus their observed RDF array forms.

The three-frame columns are [.4, missing, .8] for ZeroY (Gradient/MaskGroup) or
Top (Circular), and [True, missing, False] for Flipped (Circular/MaskGroup).
Positions 1 and 2 are tested. Four target modes are used for each column:

- Parent exists with no CorrectionMasks property.
- Parent exists with an empty CorrectionMasks RDF Seq.
- Parent exists with an empty first item in that Seq.
- Parent array is absent; a separate LocalExposure2012 column
  [.125, missing, .125] is filled and written before the selector column.

The missing column receives its previous value, including on the target frame.
The writer creates a Bag for a new root/selector array and preserves existing
Seq arrays. The target parent's own CorrectionSyncID, CorrectionName and .125
exposure survive when that parent already exists. Source selector IDs are not
copied. A newly materialized parent receives the filled numeric exposure but
does not copy the source correction ID.

As with the preceding missing-correction probe, application/history stamping
later reaches an uninitialized application singleton. Only that specific
NullPointerException after correction/selector writes is accepted; the resulting
in-memory XMP is inspected and serialized with the native runtime. These are
component observations, not a successful complete GUI save or Camera Raw
round trip. Isolated selector writing with no existing/prepared parent remains
unsupported; it failed in the earlier native writer probe.

Go compares all forty values, array forms/counts, parent IDs and parent exposure.
Additional tests cover CLI dry runs, saves, original backups, image preservation,
no-op repeats, a boolean-only parent, zero-valued parent availability, invalid
later array shapes without partial writes, and native registered-field limits.
The optional installed-parser test now accepts 46 generated packets: the
previous six initialization/correction cases and these forty selector cases.

The new first-selector path is restricted to the native registered positional
linear/radial fields and Flipped. It does not invent What, MaskSyncID, MaskName,
brush data, range structures, or unsupported selector components. Go rendering
and native Camera Raw interpretation of incomplete selectors are outside this
XMP-writer comparison.
