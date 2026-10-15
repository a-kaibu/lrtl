package lrtl

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
)

//go:embed gui.html
var guiAssets embed.FS

const (
	guiPageSize        = 80
	guiMaxPageSize     = 240
	guiMaxRequestSize  = 64 << 10
	guiMaxDecodePixels = 70_000_000
)

var guiThumbnailSlots = make(chan struct{}, 1)

func runGUI(arguments []string, stdout, stderr io.Writer) int {
	set := newFlagSet("gui", stderr)
	listen := set.String("listen", "127.0.0.1:8787", "local HTTP address (must use a loopback IP)")
	if err := set.Parse(arguments); err != nil {
		if done, code := finishFlagParse(set, err); done {
			return code
		}
	}
	if set.NArg() != 0 {
		return reportError(stderr, fmt.Errorf("Usage: lrtl gui [--listen 127.0.0.1:8787]"))
	}
	host, port, err := net.SplitHostPort(*listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return reportError(stderr, fmt.Errorf("--listen must use a loopback IP address and explicit port, such as 127.0.0.1:8787"))
	}
	if port == "" {
		return reportError(stderr, fmt.Errorf("--listen requires a port"))
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return reportError(stderr, err)
	}
	server := &http.Server{
		Handler:           newGUIHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Fprintf(stdout, "LumaFrame UI: http://%s/\nPress Ctrl+C to stop.\n", listener.Addr().String())
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return 0
	}
	return reportError(stderr, err)
}

func newGUIHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", serveGUIIndex)
	mux.HandleFunc("/api/sequence", serveGUISequence)
	mux.HandleFunc("/api/thumbnail", serveGUIThumbnail)
	mux.HandleFunc("/api/preview", serveGUIPreview)
	mux.HandleFunc("/api/xmp", serveGUIXMP)
	mux.HandleFunc("/api/keyframe", serveGUIKeyframe)
	mux.HandleFunc("/api/keyframes", serveGUIKeyframes)
	mux.HandleFunc("/api/interpolate", serveGUIInterpolate)
	mux.HandleFunc("/api/render", serveGUIRender)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		if !isLoopbackRequest(r) {
			http.Error(w, "local access only", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "same-origin request required", http.StatusForbidden)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "same-origin request required", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func isLoopbackRequest(r *http.Request) bool {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	remoteIP := net.ParseIP(remoteHost)
	if remoteIP == nil || !remoteIP.IsLoopback() {
		return false
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	requestIP := net.ParseIP(host)
	return requestIP != nil && requestIP.IsLoopback()
}

func serveGUIIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := guiAssets.ReadFile("gui.html")
	if err != nil {
		http.Error(w, "UI asset is unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

type guiFrame struct {
	Index           int    `json:"index"`
	Name            string `json:"name"`
	Keyframe        string `json:"keyframe"`
	ManagedKeyframe bool   `json:"managedKeyframe"`
	Rating          int    `json:"rating"`
	HasRating       bool   `json:"hasRating"`
	HasSidecar      bool   `json:"hasSidecar"`
	XMPError        string `json:"xmpError,omitempty"`
	Thumbnail       string `json:"thumbnail"`
}

type guiSequenceResponse struct {
	Directory string     `json:"directory"`
	Total     int        `json:"total"`
	Offset    int        `json:"offset"`
	Limit     int        `json:"limit"`
	Frames    []guiFrame `json:"frames"`
}

func serveGUISequence(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeGUIError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	directory, err := canonicalGUIDirectory(r.URL.Query().Get("directory"))
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, err.Error())
		return
	}
	offset, err := parseNonnegativeQuery(r, "offset", 0)
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := parseNonnegativeQuery(r, "limit", guiPageSize)
	if err != nil || limit < 1 || limit > guiMaxPageSize {
		writeGUIError(w, http.StatusBadRequest, "limit must be between 1 and 240")
		return
	}
	frames, err := DiscoverSequence(directory)
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, err.Error())
		return
	}
	end := offset + limit
	if end < offset || end > len(frames) {
		end = len(frames)
	}
	start := offset
	if start > len(frames) {
		start = len(frames)
	}
	response := guiSequenceResponse{
		Directory: directory, Total: len(frames), Offset: start, Limit: limit,
		Frames: make([]guiFrame, 0, end-start),
	}
	for _, frame := range frames[start:end] {
		row := guiFrame{Index: frame.Index, Name: filepath.Base(frame.Image), Thumbnail: thumbnailURL(directory, filepath.Base(frame.Image))}
		if frame.Sidecar != "" {
			row.HasSidecar = true
			packet, packetErr := loadPacket(frame)
			if packetErr != nil {
				row.XMPError = packetErr.Error()
			} else if packet != nil {
				row.Keyframe = packet.KeyframeKind()
				row.ManagedKeyframe, _ = packet.HasProperty("ol:KeyframeKind")
				row.Rating, row.HasRating = packet.Rating()
			}
		}
		response.Frames = append(response.Frames, row)
	}
	writeGUIJSON(w, http.StatusOK, response)
}

func parseNonnegativeQuery(r *http.Request, key string, fallback int) (int, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s must be a nonnegative integer", key)
	}
	return parsed, nil
}

func thumbnailURL(directory, name string) string {
	query := url.Values{}
	query.Set("directory", directory)
	query.Set("name", name)
	return "/api/thumbnail?" + query.Encode()
}

func serveGUIThumbnail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	directory, err := canonicalGUIDirectory(r.URL.Query().Get("directory"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.URL.Query().Get("name")
	frame, err := guiFrameByName(directory, name)
	if err != nil {
		http.Error(w, "image not found in sequence", http.StatusNotFound)
		return
	}
	select {
	case guiThumbnailSlots <- struct{}{}:
		defer func() { <-guiThumbnailSlots }()
	case <-r.Context().Done():
		return
	}
	data, config, _, err := readRenderInput(frame, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > guiMaxDecodePixels {
		http.Error(w, "image dimensions exceed thumbnail decoder limit", http.StatusRequestEntityTooLarge)
		return
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		http.Error(w, "could not decode preview image", http.StatusUnprocessableEntity)
		return
	}
	if orientation, orientErr := readEXIFOrientation(data); orientErr == nil {
		img = applyEXIFOrientation(img, orientation)
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 1 || height < 1 {
		http.Error(w, "empty preview image", http.StatusUnprocessableEntity)
		return
	}
	encoded, err := encodeGUIJPEG(img, 640, 84)
	if err != nil {
		http.Error(w, "could not encode preview image", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
}

func encodeGUIJPEG(img image.Image, maxSide, quality int) ([]byte, error) {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 1 || height < 1 {
		return nil, fmt.Errorf("empty preview image")
	}
	if width > maxSide || height > maxSide {
		scale := float64(maxSide) / float64(maxInt(width, height))
		width = maxInt(1, int(float64(width)*scale))
		height = maxInt(1, int(float64(height)*scale))
		resized := image.NewNRGBA(image.Rect(0, 0, width, height))
		xdraw.CatmullRom.Scale(resized, resized.Bounds(), img, bounds, xdraw.Over, nil)
		img = resized
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func serveGUIPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	directory, err := canonicalGUIDirectory(r.URL.Query().Get("directory"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	frame, err := guiFrameByName(directory, r.URL.Query().Get("name"))
	if err != nil {
		http.Error(w, "image not found in sequence", http.StatusNotFound)
		return
	}
	select {
	case guiThumbnailSlots <- struct{}{}:
		defer func() { <-guiThumbnailSlots }()
	case <-r.Context().Done():
		return
	}
	rawMode := r.URL.Query().Get("raw")
	if rawMode != "" && rawMode != "preview" && rawMode != "develop" {
		http.Error(w, "raw mode must be preview or develop", http.StatusBadRequest)
		return
	}
	if rawMode == "" && !supportedRasterExtension(frame.Image) && supportsRawDevelopment(frame.Image) {
		rawMode = "preview"
	}
	applyBasic, err := parseGUIBool(r.URL.Query().Get("basic"))
	if err != nil {
		http.Error(w, "basic must be true or false", http.StatusBadRequest)
		return
	}
	applyCrop, err := parseGUIBool(r.URL.Query().Get("crop"))
	if err != nil {
		http.Error(w, "crop must be true or false", http.StatusBadRequest)
		return
	}
	inputOptions := imageInputOptions{
		RawPreview: rawMode == "preview", RawDevelop: rawMode == "develop",
		ApplyXMPBasic: applyBasic, ApplyXMPCrop: applyCrop,
	}
	if rawMode == "develop" {
		inputOptions.DNGLabPath = r.URL.Query().Get("dnglab")
	}
	data, config, _, err := readRenderInputWithOptions(frame, inputOptions)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > guiMaxDecodePixels {
		http.Error(w, "image dimensions exceed preview decoder limit", http.StatusRequestEntityTooLarge)
		return
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		http.Error(w, "could not decode preview image", http.StatusUnprocessableEntity)
		return
	}
	orientation, err := readEXIFOrientation(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	img = applyEXIFOrientation(img, orientation)
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	adjustments, err := readSequenceXMPAdjustments([]Frame{frame}, width, height, inputOptions)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	img = adjustments.apply(0, img)
	encoded, err := encodeGUIJPEG(img, 1920, 90)
	if err != nil {
		http.Error(w, "could not encode adjusted preview", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
}

func parseGUIBool(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	return strconv.ParseBool(value)
}

func canonicalGUIDirectory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("choose a sequence directory")
	}
	if len(value) > 32760 {
		return "", fmt.Errorf("directory path is too long")
	}
	directory, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", directory)
	}
	return directory, nil
}

func guiFrameByName(directory, name string) (Frame, error) {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return Frame{}, fmt.Errorf("invalid image name")
	}
	path := filepath.Join(directory, name)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || !imageExtensions[strings.ToLower(filepath.Ext(name))] {
		return Frame{}, fmt.Errorf("image not found in sequence")
	}
	sidecar, err := findSidecar(path)
	if err != nil {
		return Frame{}, err
	}
	return Frame{Image: path, Sidecar: sidecar}, nil
}

type guiXMPResponse struct {
	Name       string            `json:"name"`
	Properties []string          `json:"properties"`
	Values     map[string]string `json:"values"`
}

func serveGUIXMP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		serveGUIXMPRead(w, r)
	case http.MethodPost:
		serveGUIXMPWrite(w, r)
	default:
		writeGUIError(w, http.StatusMethodNotAllowed, "GET or POST required")
	}
}

func serveGUIXMPRead(w http.ResponseWriter, r *http.Request) {
	directory, err := canonicalGUIDirectory(r.URL.Query().Get("directory"))
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, err.Error())
		return
	}
	frame, err := guiFrameByName(directory, r.URL.Query().Get("name"))
	if err != nil {
		writeGUIError(w, http.StatusNotFound, err.Error())
		return
	}
	packet, err := PacketForSidecar(frame.OutputSidecar())
	if err != nil {
		writeGUIError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	values := make(map[string]string)
	for _, property := range NumericCRSProperties {
		value, found, getErr := packet.GetScalar("crs:" + property)
		if getErr != nil {
			writeGUIError(w, http.StatusUnprocessableEntity, getErr.Error())
			return
		}
		if found {
			if _, valid := scalarNumber(value); valid {
				values[property] = strings.TrimSpace(value)
			}
		}
	}
	writeGUIJSON(w, http.StatusOK, guiXMPResponse{Name: filepath.Base(frame.Image), Properties: NumericCRSProperties, Values: values})
}

type guiXMPWriteRequest struct {
	Directory string `json:"directory"`
	Name      string `json:"name"`
	Property  string `json:"property"`
	Value     string `json:"value"`
}

func serveGUIXMPWrite(w http.ResponseWriter, r *http.Request) {
	if !requireGUIJSONPost(w, r) {
		return
	}
	var request guiXMPWriteRequest
	if !decodeGUIJSON(w, r, &request) {
		return
	}
	directory, err := canonicalGUIDirectory(request.Directory)
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, err.Error())
		return
	}
	frame, err := guiFrameByName(directory, request.Name)
	if err != nil {
		writeGUIError(w, http.StatusNotFound, err.Error())
		return
	}
	allowed := false
	for _, property := range NumericCRSProperties {
		if property == request.Property {
			allowed = true
			break
		}
	}
	if !allowed {
		writeGUIError(w, http.StatusBadRequest, "property is not an editable numeric Camera Raw control")
		return
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(request.Value), 64)
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, "value must be a finite number")
		return
	}
	writeGUICommandResult(w, []string{"set", frame.Image, request.Property, strconv.FormatFloat(value, 'g', 12, 64)}, runSet)
}

type guiKeyframeRequest struct {
	Directory string `json:"directory"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Remove    bool   `json:"remove"`
}

func serveGUIKeyframe(w http.ResponseWriter, r *http.Request) {
	if !requireGUIJSONPost(w, r) {
		return
	}
	var request guiKeyframeRequest
	if !decodeGUIJSON(w, r, &request) {
		return
	}
	directory, err := canonicalGUIDirectory(request.Directory)
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, err.Error())
		return
	}
	frame, err := guiFrameByName(directory, request.Name)
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, err.Error())
		return
	}
	packet, err := PacketForSidecar(frame.OutputSidecar())
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.Remove {
		if !packet.IsKeyframe() {
			writeGUIError(w, http.StatusConflict, "frame has no 4- or 5-star keyframe mark")
			return
		}
		err = UnmarkKeyframe(packet, true)
	} else {
		if request.Kind != "default" && request.Kind != "crop" {
			writeGUIError(w, http.StatusBadRequest, "kind must be default or crop")
			return
		}
		_, err = MarkKeyframe(packet, request.Kind)
	}
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, err.Error())
		return
	}
	backup, err := WritePacket(frame.OutputSidecar(), packet, true)
	if err != nil {
		writeGUIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeGUIJSON(w, http.StatusOK, map[string]any{"saved": frame.OutputSidecar(), "backup": backup})
}

type guiKeyframesRequest struct {
	Directory string `json:"directory"`
	Count     int    `json:"count"`
	Rating    int    `json:"rating"`
	DNGMode   string `json:"dngMode"`
	DNGLab    string `json:"dnglab"`
	XMPBasic  bool   `json:"xmpBasic"`
	XMPCrop   bool   `json:"xmpCrop"`
}

func serveGUIKeyframes(w http.ResponseWriter, r *http.Request) {
	if !requireGUIJSONPost(w, r) {
		return
	}
	var request guiKeyframesRequest
	if !decodeGUIJSON(w, r, &request) {
		return
	}
	directory, err := canonicalGUIDirectory(request.Directory)
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.Count < 1 || request.Count > 1000 || (request.Rating != 4 && request.Rating != 5) {
		writeGUIError(w, http.StatusBadRequest, "count must be 1-1000 and rating must be 4 or 5")
		return
	}
	if request.DNGMode != "" && request.DNGMode != "preview" && request.DNGMode != "develop" {
		writeGUIError(w, http.StatusBadRequest, "RAW mode must be none, preview, or develop")
		return
	}
	arguments := []string{"keyframes", directory, "--count", strconv.Itoa(request.Count), "--rating", strconv.Itoa(request.Rating), "--write"}
	if request.DNGMode == "preview" {
		arguments = append(arguments, "--raw-preview")
	}
	if request.DNGMode == "develop" {
		if strings.TrimSpace(request.DNGLab) == "" {
			writeGUIError(w, http.StatusBadRequest, "DNGLab path is required for RAW development")
			return
		}
		arguments = append(arguments, "--raw-develop", "--dnglab", request.DNGLab)
	}
	if request.XMPBasic {
		arguments = append(arguments, "--apply-xmp-basic")
	}
	if request.XMPCrop {
		arguments = append(arguments, "--apply-xmp-crop")
	}
	writeGUICommandResult(w, arguments, runKeyframes)
}

func serveGUIInterpolate(w http.ResponseWriter, r *http.Request) {
	if !requireGUIJSONPost(w, r) {
		return
	}
	var request struct {
		Directory  string `json:"directory"`
		Smoothness int    `json:"smoothness"`
	}
	if !decodeGUIJSON(w, r, &request) {
		return
	}
	directory, err := canonicalGUIDirectory(request.Directory)
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.Smoothness < 0 || request.Smoothness > 100 {
		writeGUIError(w, http.StatusBadRequest, "smoothness must be between 0 and 100")
		return
	}
	writeGUICommandResult(w, []string{"interpolate", directory, "--smoothness", strconv.Itoa(request.Smoothness), "--write"}, runInterpolate)
}

type guiRenderRequest struct {
	Directory string `json:"directory"`
	Output    string `json:"output"`
	FPS       int    `json:"fps"`
	Codec     string `json:"codec"`
	FFmpeg    string `json:"ffmpeg"`
	DNGMode   string `json:"dngMode"`
	DNGLab    string `json:"dnglab"`
	XMPBasic  bool   `json:"xmpBasic"`
	XMPCrop   bool   `json:"xmpCrop"`
	Deflicker bool   `json:"deflicker"`
	Overwrite bool   `json:"overwrite"`
}

var guiCodecs = map[string]bool{
	"h264": true, "h265": true, "prores": true, "prores-hq": true,
	"dnxhr-lb": true, "dnxhr-sq": true, "dnxhr-hq": true, "dnxhr-hqx": true, "dnxhr-444": true,
}

func serveGUIRender(w http.ResponseWriter, r *http.Request) {
	if !requireGUIJSONPost(w, r) {
		return
	}
	var request guiRenderRequest
	if !decodeGUIJSON(w, r, &request) {
		return
	}
	directory, err := canonicalGUIDirectory(request.Directory)
	if err != nil {
		writeGUIError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.Output = strings.TrimSpace(request.Output)
	if request.Output == "" || len(request.Output) > 32760 {
		writeGUIError(w, http.StatusBadRequest, "output path is required")
		return
	}
	if request.FPS < 1 || request.FPS > 240 {
		writeGUIError(w, http.StatusBadRequest, "fps must be between 1 and 240")
		return
	}
	if request.Codec != "" && !guiCodecs[request.Codec] {
		writeGUIError(w, http.StatusBadRequest, "unsupported codec")
		return
	}
	if request.DNGMode != "" && request.DNGMode != "preview" && request.DNGMode != "develop" {
		writeGUIError(w, http.StatusBadRequest, "RAW mode must be none, preview, or develop")
		return
	}
	arguments := []string{"render", directory, "--output", request.Output, "--fps", strconv.Itoa(request.FPS), "--auto-orient"}
	if request.Codec != "" {
		arguments = append(arguments, "--codec", request.Codec)
	}
	if request.FFmpeg != "" {
		arguments = append(arguments, "--ffmpeg", request.FFmpeg)
	}
	if request.DNGMode == "preview" {
		arguments = append(arguments, "--raw-preview")
	}
	if request.DNGMode == "develop" {
		if strings.TrimSpace(request.DNGLab) == "" {
			writeGUIError(w, http.StatusBadRequest, "DNGLab path is required for RAW development")
			return
		}
		arguments = append(arguments, "--raw-develop", "--dnglab", request.DNGLab)
	}
	if request.XMPBasic {
		arguments = append(arguments, "--apply-xmp-basic")
	}
	if request.XMPCrop {
		arguments = append(arguments, "--apply-xmp-crop")
	}
	if request.Deflicker {
		arguments = append(arguments, "--deflicker")
	}
	if request.Overwrite {
		arguments = append(arguments, "--overwrite")
	}
	writeGUICommandResult(w, arguments, runRender)
}

type guiCommandRunner func([]string, io.Writer, io.Writer) int

func writeGUICommandResult(w http.ResponseWriter, arguments []string, run guiCommandRunner) {
	var output, errorsBuffer bytes.Buffer
	code := run(arguments, &output, &errorsBuffer)
	message := strings.TrimSpace(output.String())
	if code != 0 {
		if message != "" {
			message += "\n"
		}
		message += strings.TrimSpace(errorsBuffer.String())
		writeGUIJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": message, "exitCode": code})
		return
	}
	if message == "" {
		message = strings.TrimSpace(errorsBuffer.String())
	}
	writeGUIJSON(w, http.StatusOK, map[string]any{"output": message})
}

func requireGUIJSONPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		writeGUIError(w, http.StatusMethodNotAllowed, "POST required")
		return false
	}
	if r.Header.Get("Origin") != "http://"+r.Host {
		writeGUIError(w, http.StatusForbidden, "same-origin request required")
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	if mediaType != "application/json" {
		writeGUIError(w, http.StatusUnsupportedMediaType, "application/json required")
		return false
	}
	return true
}

func decodeGUIJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, guiMaxRequestSize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeGUIError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeGUIError(w, http.StatusBadRequest, "request must contain one JSON object")
		return false
	}
	return true
}

func writeGUIError(w http.ResponseWriter, status int, message string) {
	writeGUIJSON(w, status, map[string]string{"error": message})
}

func writeGUIJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
