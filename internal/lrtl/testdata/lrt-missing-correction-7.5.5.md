# LRT 7.5.5 missing positional correction observations

Sixteen observations were taken from installed 7.5.5 build 1059. They cover
Gradient, CircularGradient, Paint, and MaskGroup corrections at positions 1 and
2, separately for LocalExposure2012 and CorrectionActive.

Each synthetic three-frame column is [.4, missing, .8] or [True, missing, False].
The middle frame starts with an empty XMP packet and no correction array. The
runtime's ordinary missing-column helper fills the middle cell from the
previous cell. The native frame-to-XMP writer then materializes its registered
root correction path. The observed middle resource contains .400000 or True;
its newly created array is an RDF Bag and has the requested number of items,
with empty preceding items where needed. The JSON records these observations,
not application code or user metadata.

Probe boundary: the frame writer's later application/history stamping reaches
an uninitialized application singleton and throws a NullPointerException after
the correction properties have been written. The probe accepts only this
specific tail failure, then inspects and serializes the in-memory XMP using the
runtime parser/serializer. This does not establish a complete native GUI save,
Auto-Transition workflow, or Camera Raw round trip. The missing-column helper
and correction-path materialization themselves were executed.

A separate isolated attempt to write only a nested selector column into a
packet with no parent correction array fails in the native XPath array writer.
It is not evidence for copying a whole selector tree. Go therefore expands
missing selected root correction numbers and CorrectionActive, while retaining
the existing selected-selector/range-resource policy. Non-keyframe template
cloning is a separate existing policy, not established by these observations.

These investigations also exposed an RDF serialization issue: property
attributes alongside rdf:parseType="Resource" are rejected by the installed
Adobe XMP parser. Generated resource fields now use child property elements.
The optional TestGeneratedMaskResourcesAdobeParser compiles an original small
reflection harness and parses modern/legacy initialization and four generated
missing-correction packets in the installed runtime. It requires Java/javac and
LRTL_NATIVE_JAR; ordinary tests validate generated RDF shape without Java.
