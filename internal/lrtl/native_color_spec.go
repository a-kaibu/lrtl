package lrtl

import (
	"fmt"
	"strings"
)

type nativeColorSpec struct {
	source         string
	gamut, dynamic int
}

func (spec nativeColorSpec) validate() error {
	if spec.gamut < 0 || spec.gamut > 1 || spec.dynamic < 0 || spec.dynamic > 1 {
		return fmt.Errorf("invalid native gamut/dynamic range")
	}
	if spec.source != "" && spec.source != "BT709" && spec.source != "BT2020" {
		return fmt.Errorf("invalid native source color")
	}
	if spec.dynamic == 1 && spec.gamut != 1 {
		return fmt.Errorf("PQ output requires BT2020 gamut")
	}
	source := spec.source
	if source == "" {
		source = "BT709"
	}
	_, _, err := nativeColorPlan(source, spec.gamut, spec.dynamic, 0)
	return err
}

func nativeColorPlan(source string, gamut, dynamic, videoRange int) (filter, metadata string, err error) {
	inMatrix, inPrimary, inTransfer := "709", "709", "709"
	outMatrix, outPrimary, outTransfer := "709", "709", "709"
	wide := gamut == 1
	if wide {
		outMatrix, outPrimary, outTransfer = "2020_ncl", "2020", "2020_10"
		if dynamic == 1 {
			outTransfer = "smpte2084"
		}
	}
	if strings.Contains(source, "2020") {
		inMatrix, inPrimary, inTransfer = "2020_ncl", "2020", "2020_10"
		if wide {
			inTransfer = outTransfer
		}
		trc := "bt2020-10"
		if dynamic == 1 {
			trc = "smpte2084"
		}
		metadata = "color_primaries=bt2020:color_trc=" + trc + ":colorspace=bt2020nc"
	} else {
		metadata = "color_primaries=bt709:color_trc=bt709:colorspace=bt709"
		if wide {
			return "", metadata, fmt.Errorf("Wide gamut rendering not supported for intermediary sequences in sRGB or AdobeRGB color space. Please reexport via LRTExport.")
		}
	}
	rangeName := "limited"
	if videoRange == 1 {
		rangeName = "full"
	}
	filter = fmt.Sprintf(",zscale=rangein=full:range=%s:matrixin=%s:matrix=%s:primariesin=%s:primaries=%s:transferin=%s:transfer=%s", rangeName, inMatrix, outMatrix, inPrimary, outPrimary, inTransfer, outTransfer)
	return filter, metadata, nil
}

func renderNativeColorSpec(options RenderOptions) (nativeColorSpec, error) {
	spec := nativeColorSpec{source: "BT709"}
	input := strings.ToLower(strings.TrimSpace(options.InputColor))
	if input == "" {
		input = "bt709"
	}
	if input == "bt2020" {
		spec.source = "BT2020"
	} else if input != "bt709" {
		return spec, fmt.Errorf("input-color must be bt709 or bt2020")
	}
	gamut := strings.ToLower(strings.TrimSpace(options.OutputGamut))
	if gamut == "" {
		gamut = "bt709"
	}
	if gamut == "bt2020" {
		spec.gamut = 1
	} else if gamut != "bt709" {
		return spec, fmt.Errorf("output-gamut must be bt709 or bt2020")
	}
	dynamic := strings.ToLower(strings.TrimSpace(options.DynamicRange))
	if dynamic == "" {
		dynamic = "sdr"
	}
	if dynamic == "pq" {
		spec.dynamic = 1
	} else if dynamic != "sdr" {
		return spec, fmt.Errorf("dynamic-range must be sdr or pq")
	}
	if spec.dynamic == 1 && spec.gamut != 1 {
		return spec, fmt.Errorf("PQ output requires --output-gamut bt2020")
	}
	_, _, err := nativeColorPlan(spec.source, spec.gamut, spec.dynamic, 0)
	return spec, err
}

func (spec nativeColorSpec) filter(value, pixelFormat string, planar bool) string {
	if spec.source == "" {
		spec.source = "BT709"
	}
	rangeID := 0
	if value == "full" {
		rangeID = 1
	}
	filter, metadata, _ := nativeColorPlan(spec.source, spec.gamut, spec.dynamic, rangeID)
	prefix := ""
	if !planar {
		prefix = "pad=ceil(iw/2)*2:ceil(ih/2)*2,scale=iw:ih:out_range=full:flags=lanczos,format=yuv444p10le,"
	}
	return prefix + "setparams=" + metadata + ",setsar=1/1" + filter + ",format=" + pixelFormat
}

func (spec nativeColorSpec) h26xPixelFormat(sampling string) (string, error) {
	pix, err := h26xPixelFormat(sampling)
	if err == nil && pix == "yuv420p" && (spec.gamut == 1 || spec.dynamic == 1) {
		pix = "yuv420p10le"
	}
	return pix, err
}

func (spec nativeColorSpec) primaries() string {
	if spec.gamut == 1 {
		return "bt2020"
	}
	return "bt709"
}
func (spec nativeColorSpec) matrix() string {
	if spec.gamut == 1 {
		return "bt2020nc"
	}
	return "bt709"
}
func (spec nativeColorSpec) transfer() string {
	if spec.dynamic == 1 {
		return "smpte2084"
	}
	return "unspecified"
}
func (spec nativeColorSpec) x264Params(value string) string {
	rangeName := "off"
	if value == "full" {
		rangeName = "on"
	}
	trc := "bt709"
	if spec.gamut == 1 {
		trc = "bt2020-10"
	}
	return "colorprim=" + spec.primaries() + ":fullrange=" + rangeName + ":transfer=" + trc + ":colormatrix=" + spec.matrix()
}
func (spec nativeColorSpec) x265Params(value string) string {
	trc, matrix := "bt709", spec.matrix()
	if spec.gamut == 1 {
		trc = "bt2020-10"
		if spec.dynamic == 1 {
			trc = "smpte2084"
		}
	} else if strings.Contains(spec.source, "2020") {
		matrix = "bt2020nc"
	}
	params := "colorprim=" + spec.primaries() + ":range=" + value + ":transfer=" + trc + ":colormatrix=" + matrix
	if spec.dynamic == 1 {
		params += ":master-display=G(13250,34500)B(7500,3000)R(34000,16000)WP(15635,16450)L(10000000,1):max-cll=1000,400"
	}
	return params
}
func (spec nativeColorSpec) proresMetadata() string {
	trc := "bt709"
	if spec.gamut == 1 {
		trc = "auto"
		if spec.dynamic == 1 {
			trc = "smpte2084"
		}
	}
	return "prores_metadata=color_primaries=" + spec.primaries() + ":color_trc=" + trc + ":colorspace=" + spec.matrix()
}
