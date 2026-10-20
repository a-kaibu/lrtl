package lrtl

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type lrtRenderPreset struct {
	SavedAspectRatio     *float64 `json:"-"`
	Name                 string   `json:"name"`
	ID                   string   `json:"ID"`
	Encoder              string   `json:"encoder"`
	FrameRate            string   `json:"frameRate"`
	OutputResV           int      `json:"outputResV"`
	OutputQuality        int      `json:"outputQuality"`
	SubSampling          string   `json:"subSampling"`
	Speed                float64  `json:"speed"`
	Interpolation        int      `json:"interpolation"`
	MotionBlur           bool     `json:"motionBlur"`
	MotionBlurLevel      int      `json:"motionBlurLevel"`
	Audio                bool     `json:"audio"`
	AudioTrack           string   `json:"audioTrack"`
	VideoRange           int      `json:"videoRange"`
	Gamut                int      `json:"gamut"`
	DynamicRange         int      `json:"dynamicRange"`
	FadeOut              bool     `json:"fadeOut"`
	FadeTime             int      `json:"fadeTime"`
	Sharpen              bool     `json:"sharpen"`
	CAS                  bool     `json:"cas"`
	RemoveAirplanes      bool     `json:"removeAirplanes"`
	RemoveAirplanesLevel int      `json:"removeAirplanesLevel"`
	Denoise              bool     `json:"denoise"`
	DenoiseLevel         int      `json:"denoiseLevel"`
	DeflickerLuma        bool     `json:"deflickerLuma"`
	DeflickerChroma      bool     `json:"deflickerChroma"`
}

func decodeLRTRenderPreset(data []byte) (lrtRenderPreset, error) {
	// Native JSON deserialization starts with the RenderPreset constructor.
	preset := lrtRenderPreset{Encoder: "h264", FrameRate: "29.97", OutputResV: 1080, OutputQuality: 1, SubSampling: "420", Speed: 1, FadeTime: 3, DenoiseLevel: 1, RemoveAirplanesLevel: 1}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return preset, err
	}
	if fields == nil {
		return preset, fmt.Errorf("render preset must be a JSON object")
	}
	for _, name := range []string{"encoder", "frameRate", "outputResV", "outputQuality"} {
		value, found := fields[name]
		if !found || string(value) == "null" {
			return preset, fmt.Errorf("render preset must contain %s", name)
		}
	}
	if err := json.Unmarshal(data, &preset); err != nil {
		return preset, err
	}
	return preset, nil
}

func loadLRTRenderPreset(path, name string) (lrtRenderPreset, error) {
	file, err := os.Open(path)
	if err != nil {
		return lrtRenderPreset{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return lrtRenderPreset{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16*1024*1024 {
		return lrtRenderPreset{}, fmt.Errorf("render preset must be a regular JSON file of at most 16 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, 16*1024*1024+1))
	if err != nil {
		return lrtRenderPreset{}, err
	}
	if len(data) > 16*1024*1024 {
		return lrtRenderPreset{}, fmt.Errorf("render preset exceeds 16 MiB")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return lrtRenderPreset{}, err
	}
	if object == nil {
		return lrtRenderPreset{}, fmt.Errorf("expected a render preset or LRT settings object")
	}
	finish := func(preset lrtRenderPreset, err error) (lrtRenderPreset, error) {
		if err != nil {
			return preset, err
		}
		if raw, present := object["renderConstrainCrop"]; present {
			var constrain bool
			if err := json.Unmarshal(raw, &constrain); err != nil {
				return preset, err
			}
			if constrain {
				var ratio float64
				if err := json.Unmarshal(object["renderAR"], &ratio); err != nil {
					return preset, fmt.Errorf("renderAR: %w", err)
				}
				if ratio < 1.0/16 || ratio > 16 {
					return preset, fmt.Errorf("saved renderAR must be between 1:16 and 16:1")
				}
				preset.SavedAspectRatio = &ratio
			}
		}
		return preset, nil
	}
	if _, single := object["encoder"]; single {
		preset, err := decodeLRTRenderPreset(data)
		if err == nil && name != "" && preset.Name != name {
			return preset, fmt.Errorf("preset name %q does not match %q", preset.Name, name)
		}
		return preset, err
	}
	if name == "" {
		last, found := object["lastRenderSettings"]
		if !found {
			return lrtRenderPreset{}, fmt.Errorf("settings JSON needs lastRenderSettings, or select renderPresets with --lrt-preset-name")
		}
		return finish(decodeLRTRenderPreset(last))
	}
	var presets []json.RawMessage
	if err := json.Unmarshal(object["renderPresets"], &presets); err != nil {
		return lrtRenderPreset{}, fmt.Errorf("renderPresets: %w", err)
	}
	var found []lrtRenderPreset
	for _, raw := range presets {
		var identity struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &identity); err != nil {
			return lrtRenderPreset{}, err
		}
		if identity.Name == name {
			preset, err := decodeLRTRenderPreset(raw)
			if err != nil {
				return preset, err
			}
			found = append(found, preset)
		}
	}
	if len(found) != 1 {
		return lrtRenderPreset{}, fmt.Errorf("expected one render preset named %q, found %d", name, len(found))
	}
	return finish(found[0], nil)
}

func applyLRTRenderPreset(options RenderOptions, preset lrtRenderPreset, path string, explicit map[string]bool) (RenderOptions, error) {
	unsupported := []string{}
	for _, field := range []struct {
		name   string
		active bool
	}{
		{"gamut", preset.Gamut != 0}, {"dynamicRange", preset.DynamicRange != 0},
	} {
		if field.active {
			unsupported = append(unsupported, field.name)
		}
	}
	if len(unsupported) > 0 {
		return options, fmt.Errorf("LRT render preset uses unsupported controls: %s", strings.Join(unsupported, ", "))
	}
	if !explicit["sharpen"] {
		options.OutputSharpen = preset.Sharpen
	}
	if !explicit["cas"] {
		options.OutputCAS = preset.CAS
	}
	if !explicit["denoise"] {
		options.OutputDenoise = 0
		if preset.Denoise {
			if preset.DenoiseLevel < 1 || preset.DenoiseLevel > 4 {
				return options, fmt.Errorf("enabled LRT denoiseLevel must be from 1 to 4")
			}
			options.OutputDenoise = preset.DenoiseLevel
		}
	}
	if !explicit["remove-airplanes"] {
		options.RemoveAirplanes = 0
		if preset.RemoveAirplanes {
			if preset.RemoveAirplanesLevel < 1 || preset.RemoveAirplanesLevel > 3 {
				return options, fmt.Errorf("enabled LRT removeAirplanesLevel must be from 1 to 3")
			}
			options.RemoveAirplanes = preset.RemoveAirplanesLevel
		}
	}
	options.NativeTiming = true
	if !explicit["interpolation"] {
		options.Interpolation = preset.Interpolation
	}
	if !explicit["output-deflicker-luma"] {
		options.OutputDeflickerLuma = preset.DeflickerLuma
	}
	if !explicit["output-deflicker-chroma"] {
		options.OutputDeflickerChroma = preset.DeflickerChroma
	}
	if preset.OutputQuality < 0 || preset.OutputQuality > 5 {
		return options, fmt.Errorf("LRT outputQuality must be between 0 and 5")
	}
	if !explicit["video-range"] {
		options.VideoRange = "limited"
		if preset.VideoRange == 1 {
			options.VideoRange = "full"
		}
	}
	if _, err := normalizeVideoRange(options.VideoRange); err != nil {
		return options, err
	}
	if !explicit["speed"] {
		value, err := validatePlaybackSpeed(preset.Speed)
		if err != nil {
			return options, err
		}
		options.PlaybackSpeed = value
	}
	if !explicit["fade-out"] {
		options.FadeOutSeconds = 0
		if preset.FadeOut {
			if preset.FadeTime < 0 {
				return options, fmt.Errorf("LRT fadeTime must be nonnegative")
			}
			options.FadeOutSeconds = float64(preset.FadeTime)
		}
	}
	if !explicit["codec"] {
		switch strings.ToLower(preset.Encoder) {
		case "h264":
			options.Codec = "h264"
		case "h265":
			options.Codec = "h265"
		case "prores":
			options.Codec = []string{"prores-proxy", "prores-lt", "prores", "prores-hq", "prores-4444", "prores-hq"}[preset.OutputQuality]
		case "dnxhr":
			options.Codec = []string{"dnxhr-lb", "dnxhr-sq", "dnxhr-hq", "dnxhr-hqx", "dnxhr-444", "dnxhr-hq"}[preset.OutputQuality]
		default:
			return options, fmt.Errorf("unsupported LRT encoder: %s", preset.Encoder)
		}
	}
	if options.Codec == "h264" || options.Codec == "h265" {
		if !explicit["subsampling"] {
			options.Subsampling = preset.SubSampling
		}
		if _, err := normalizeSubsampling(options.Subsampling); err != nil {
			return options, err
		}
		if !explicit["crf"] {
			values := []int{22, 18, 15, 10, 8, 0}
			if options.Codec == "h265" {
				values = []int{22, 20, 16, 10, 8, 0}
			}
			options.CRF = values[preset.OutputQuality]
		}
	} else if !explicit["subsampling"] {
		options.Subsampling = ""
	}
	if !explicit["fps"] {
		options.FrameRate = preset.FrameRate
		options.NativeFPS = true
	} else {
		options.NativeFPS = false
	}
	if _, err := options.frameRate(); err != nil {
		return options, err
	}
	options.NativeGeometry = !explicit["output-height"] && !explicit["long-edge"]
	if options.NativeGeometry {
		options.OutputHeight = preset.OutputResV
		options.LongEdge = 0
	}
	if !explicit["aspect"] && preset.SavedAspectRatio != nil {
		options.AspectRatio = *preset.SavedAspectRatio
	}
	if !explicit["motion-blur"] {
		options.MotionBlurFrames = 0
		if preset.MotionBlur {
			if preset.MotionBlurLevel == 0 || !validMotionBlurFrames(preset.MotionBlurLevel) {
				return options, fmt.Errorf("unsupported LRT motionBlurLevel: %d", preset.MotionBlurLevel)
			}
			options.MotionBlurFrames = preset.MotionBlurLevel
		}
	}
	if !explicit["audio"] {
		options.AudioPath = ""
		if preset.Audio {
			if strings.TrimSpace(preset.AudioTrack) == "" {
				return options, fmt.Errorf("audio is enabled but audioTrack is missing")
			}
			options.AudioPath = preset.AudioTrack
			if !filepath.IsAbs(options.AudioPath) {
				options.AudioPath = filepath.Join(filepath.Dir(path), options.AudioPath)
			}
		}
	}
	return options, nil
}
