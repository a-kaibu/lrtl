package lrtl

import (
	"path/filepath"
	"strings"
)

// The model loader uses Integer.parseInt rather than the registered-column
// converter's trimming/extra-sign behavior. Reuse its BMP decimal digit support
// after enforcing the strict lexical form.
func nativeReloadInteger(value string) (int32, bool) {
	unsigned := value
	if strings.HasPrefix(unsigned, "+") || strings.HasPrefix(unsigned, "-") {
		unsigned = unsigned[1:]
	}
	if unsigned == "" || unsigned != selectedJavaTrim(unsigned) || strings.HasPrefix(unsigned, "+") || strings.HasPrefix(unsigned, "-") {
		return 0, false
	}
	return selectedJavaInteger(value)
}

func nativeReloadRating(value string) int32 {
	rating, _ := nativeReloadInteger(value)
	if rating == 1 {
		return 4
	}
	return rating
}

// Only the immediate parent name is tested, case-sensitively. This is a model
// cache rule; the save layer still decides whether to rewrite the XML rating.
func nativeFrameRating(rating int32, image string) int32 {
	if rating != 2 && rating != 3 {
		return rating
	}
	if absolute, err := filepath.Abs(image); err == nil {
		image = absolute
	}
	if (rating == 2 || rating == 3) && strings.HasPrefix(filepath.Base(filepath.Dir(image)), "LRT_") {
		return 0
	}
	return rating
}

// Rating is a global XMP property. SDK-style strings may be literal values,
// qualified literals, rdf:value wrappers, or URI values; nested RDF subjects
// are not alternate locations for a frame's rating.
func readRootRating(packet *XMPPacket) (string, bool, error) {
	return captureTimestampScalar(packet, "xmp:Rating")
}

func hasRootMetadataProperty(packet *XMPPacket, property string) (bool, error) {
	uri, local, err := expandedName(property)
	if err != nil {
		return false, err
	}
	root := packet.findRootProperty(uri, local)
	return root.attribute != nil || root.element != nil, nil
}

func removeRootMetadataProperty(packet *XMPPacket, property string) error {
	uri, local, err := expandedName(property)
	if err != nil {
		return err
	}
	before, after, err := saveStateMaps(packet)
	if err != nil {
		return err
	}
	delete(after, pasteName{uri, local})
	_, err = writeChangedPasteRoots(packet, before, after)
	return err
}
