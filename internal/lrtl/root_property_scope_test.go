package lrtl

import "testing"

func TestCLIReadersIgnoreNestedRootNamedProperties(t *testing.T) {
	packet, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:lrt="http://lrtimelapse.com/" xmlns:ol="urn:open-lrtl:metadata:1.0"><crs:Look><rdf:Description crs:CropLeft="0.8" crs:CropRight="0.2" crs:HasCrop="True" crs:Exposure2012="invalid" crs:ConvertToGrayscale="invalid" lrt:ExternalExposureDefault="4" ol:HolyGrailCorrection2012="invalid" ol:HolyGrailXMPTarget="invalid" ol:DeflickerCorrection2012="invalid" ol:DeflickerXMPTarget="invalid"/></crs:Look></rdf:Description></rdf:RDF></x:xmpmeta>`))
	if err != nil {
		t.Fatal(err)
	}
	if value, found, err := packet.GetScalar("crs:Exposure2012"); err != nil || !found || value != "invalid" {
		t.Fatalf("fixture must expose nested value through recursive lookup: value=%q found=%t err=%v", value, found, err)
	}

	crop, err := readCropMetadata(packet)
	if err != nil || crop != (cropMetadata{Right: 1, Bottom: 1}) {
		t.Fatalf("nested crop values affected image crop: crop=%+v err=%v", crop, err)
	}

	exposure, err := readXMPExposurePacket(packet)
	if err != nil || exposure != 0 {
		t.Fatalf("nested exposure values affected global exposure: exposure=%g err=%v", exposure, err)
	}

	correction, target, err := readDeflickerTracking(packet)
	if err != nil || correction != 0 || target != globalXMPExposureTarget() {
		t.Fatalf("nested values affected Deflicker tracking: correction=%g target=%v err=%v", correction, target, err)
	}

	correction, _, err = readHolyGrailRenderCorrection(packet)
	if err != nil || correction != 0 {
		t.Fatalf("nested values affected Holy Grail correction: correction=%g err=%v", correction, err)
	}

	adjustments := &xmpBasicAdjustments{}
	if err := readXMPGrayscale(packet, adjustments); err != nil || adjustments.grayscale {
		t.Fatalf("nested grayscale value affected image settings: grayscale=%t err=%v", adjustments.grayscale, err)
	}

	measurements, available, err := readLRTKeyframeMeasurements([]Frame{{}}, []*XMPPacket{packet})
	if err != nil || available || len(measurements) != 0 {
		t.Fatalf("nested value was used as keyframe measurement: values=%v available=%t err=%v", measurements, available, err)
	}
}

func TestMaskEditProtectionIgnoresNestedManagedTargets(t *testing.T) {
	packet, err := parseXMP([]byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:ol="urn:open-lrtl:metadata:1.0"><crs:MaskGroupBasedCorrections><rdf:Seq><rdf:li rdf:parseType="Resource" crs:What="Correction" crs:CorrectionActive="True" crs:CorrectionAmount="1" crs:LocalExposure2012="0"/></rdf:Seq></crs:MaskGroupBasedCorrections><crs:Look><rdf:Description ol:HolyGrailXMPTarget="MaskGroupBasedCorrections:1"/></crs:Look></rdf:Description></rdf:RDF></x:xmpmeta>`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setMaskProperty(packet, maskEditPath{"MaskGroupBasedCorrections", 1, 0, "LocalExposure2012"}, ".25"); err != nil {
		t.Fatalf("nested tracking target incorrectly protected a user mask: %v", err)
	}
}
