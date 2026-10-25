package lrtl

import (
	"fmt"
	"math"
)

// The temperature locus and slopes are the DNG SDK's Wyszecki & Stiles table.
// Keeping these values here makes illuminant interpolation follow DNG's
// temperature/tint coordinate system rather than a CCT-only approximation.
var dngTemperatureLocus = [31][4]float64{
	{0, 0.18006, 0.26352, -0.24341}, {10, 0.18066, 0.26589, -0.25479},
	{20, 0.18133, 0.26846, -0.26876}, {30, 0.18208, 0.27119, -0.28539},
	{40, 0.18293, 0.27407, -0.30470}, {50, 0.18388, 0.27709, -0.32675},
	{60, 0.18494, 0.28021, -0.35156}, {70, 0.18611, 0.28342, -0.37915},
	{80, 0.18740, 0.28668, -0.40955}, {90, 0.18880, 0.28997, -0.44278},
	{100, 0.19032, 0.29326, -0.47888}, {125, 0.19462, 0.30141, -0.58204},
	{150, 0.19962, 0.30921, -0.70471}, {175, 0.20525, 0.31647, -0.84901},
	{200, 0.21142, 0.32312, -1.0182}, {225, 0.21807, 0.32909, -1.2168},
	{250, 0.22511, 0.33439, -1.4512}, {275, 0.23247, 0.33904, -1.7298},
	{300, 0.24010, 0.34308, -2.0637}, {325, 0.24702, 0.34655, -2.4681},
	{350, 0.25591, 0.34951, -2.9641}, {375, 0.26400, 0.35200, -3.5814},
	{400, 0.27218, 0.35407, -4.3633}, {425, 0.28039, 0.35577, -5.3762},
	{450, 0.28863, 0.35714, -6.7262}, {475, 0.29685, 0.35823, -8.5955},
	{500, 0.30505, 0.35907, -11.324}, {525, 0.31320, 0.35968, -15.628},
	{550, 0.32129, 0.36011, -23.325}, {575, 0.32931, 0.36038, -40.770},
	{600, 0.33724, 0.36051, -116.45},
}

func dngTemperatureTint(xy [2]float64) (temperature, tint float64, err error) {
	x, y := xy[0], xy[1]
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || x <= 0 || y <= 0 || x+y >= 1 {
		return 0, 0, fmt.Errorf("invalid DNG illuminant white point")
	}
	denominator := 1.5 - x + 6*y
	if denominator <= 0 {
		return 0, 0, fmt.Errorf("invalid DNG illuminant white point")
	}
	u, v := 2*x/denominator, 3*y/denominator
	lastDT, lastDU, lastDV := 0.0, 0.0, 0.0
	for index := 1; index <= 30; index++ {
		slope := dngTemperatureLocus[index][3]
		du, dv := 1/math.Sqrt(1+slope*slope), slope/math.Sqrt(1+slope*slope)
		uu, vv := u-dngTemperatureLocus[index][1], v-dngTemperatureLocus[index][2]
		dt := -uu*dv + vv*du
		if dt > 0 && index < 30 {
			lastDT, lastDU, lastDV = dt, du, dv
			continue
		}
		if dt > 0 {
			dt = 0
		}
		dt = -dt
		fraction := 0.0
		if index != 1 && lastDT+dt != 0 {
			fraction = dt / (lastDT + dt)
		}
		temperature = 1e6 / (dngTemperatureLocus[index-1][0]*fraction + dngTemperatureLocus[index][0]*(1-fraction))
		uu = u - (dngTemperatureLocus[index-1][1]*fraction + dngTemperatureLocus[index][1]*(1-fraction))
		vv = v - (dngTemperatureLocus[index-1][2]*fraction + dngTemperatureLocus[index][2]*(1-fraction))
		du = du*(1-fraction) + lastDU*fraction
		dv = dv*(1-fraction) + lastDV*fraction
		length := math.Hypot(du, dv)
		if length == 0 || math.IsNaN(length) || math.IsInf(length, 0) {
			return 0, 0, fmt.Errorf("invalid DNG illuminant temperature")
		}
		tint = (uu*du/length + vv*dv/length) * -3000
		if temperature <= 0 || math.IsNaN(temperature) || math.IsInf(temperature, 0) || math.IsNaN(tint) || math.IsInf(tint, 0) {
			return 0, 0, fmt.Errorf("invalid DNG illuminant temperature")
		}
		return temperature, tint, nil
	}
	return 0, 0, fmt.Errorf("could not estimate DNG illuminant temperature")
}

func dngTripleIlluminantWeights(white [2]float64, illuminants [3][2]float64) ([3]float64, error) {
	var weights [3]float64
	whiteTemp, whiteTint, err := dngTemperatureTint(white)
	if err != nil {
		return weights, err
	}
	whitePoint := [2]float64{math.Min(1500/whiteTemp, 1), whiteTint / 200}
	sum := 0.0
	for index, xy := range illuminants {
		temperature, tint, err := dngTemperatureTint(xy)
		if err != nil {
			return weights, fmt.Errorf("illuminant %d: %w", index+1, err)
		}
		point := [2]float64{math.Min(1500/temperature, 1), tint / 200}
		distance := (whitePoint[0]-point[0])*(whitePoint[0]-point[0]) + (whitePoint[1]-point[1])*(whitePoint[1]-point[1])
		weights[index] = 1 / (distance + 1e-8)
		sum += weights[index]
	}
	for index := range weights {
		weight := weights[index] / sum
		weight = weight * weight * (3 - 2*weight)
		weights[index] = min(1, max(0, (weight-.02)/.98))
	}
	sum = weights[0] + weights[1] + weights[2]
	if sum <= 0 || math.IsNaN(sum) || math.IsInf(sum, 0) {
		return [3]float64{}, fmt.Errorf("invalid DNG triple-illuminant weights")
	}
	weights[0] /= sum
	weights[1] /= sum
	weights[2] = math.Max(1-weights[0]-weights[1], 0)
	return weights, nil
}

func dngIlluminantWhiteXY(fields *dngFields, codeTag, dataTag uint16) ([2]float64, error) {
	code := fields.integer(codeTag, 0)
	if fields.err != nil {
		return [2]float64{}, fields.err
	}
	if code == 255 {
		return dngCustomIlluminantWhiteXY(fields, dataTag)
	}
	var white [2]float64
	switch code {
	case 1, 4, 9, 20:
		white = [2]float64{0.3324, 0.3474} // D55.
	case 2:
		white = [2]float64{0.37208, 0.37529} // F2.
	case 3, 17:
		white = [2]float64{0.4476, 0.4074} // Standard Light A.
	case 10, 21:
		white = [2]float64{0.3127, 0.3290} // D65.
	case 11, 22:
		white = [2]float64{0.2990, 0.3149} // D75.
	case 12:
		white = [2]float64{0.31310, 0.33727} // F1.
	case 13:
		white = [2]float64{0.34588, 0.35875} // F8.
	case 14:
		white = [2]float64{0.37417, 0.37281} // F9.
	case 15:
		white = [2]float64{0.40910, 0.39430} // F3.
	case 18:
		white = [2]float64{0.348483, 0.351747} // Standard Light B.
	case 19:
		white = [2]float64{0.310061, 0.316150} // Standard Light C.
	case 23:
		white = [2]float64{0.3457, 0.3585} // D50.
	case 24:
		white = dngXYFromTemperature(3200)
	default:
		return white, fmt.Errorf("unsupported DNG calibration illuminant %d", code)
	}
	return white, nil
}

func dngCustomIlluminantWhiteXY(fields *dngFields, tag uint16) ([2]float64, error) {
	entry, found := fields.entries[tag]
	if !found {
		return [2]float64{}, fmt.Errorf("DNG calibration illuminant %d requires IlluminantData", tag)
	}
	if entry.typeID != 7 || entry.count < 2 {
		return [2]float64{}, fmt.Errorf("invalid DNG IlluminantData tag %d", tag)
	}
	scanner := entry.source
	if scanner == nil {
		scanner = fields.scanner
	}
	length := uint64(entry.count)
	data := entry.value
	if length > 4 {
		offset := uint64(scanner.order.Uint32(entry.value))
		if !tiffRange(offset, length, uint64(len(scanner.data))) {
			return [2]float64{}, fmt.Errorf("DNG IlluminantData tag %d extends beyond file", tag)
		}
		data = scanner.data[offset : offset+length]
	}
	if len(data) < 2 || scanner.order.Uint16(data[:2]) != 0 {
		return [2]float64{}, fmt.Errorf("unsupported DNG IlluminantData encoding in tag %d", tag)
	}
	if len(data) < 18 {
		return [2]float64{}, fmt.Errorf("DNG IlluminantData tag %d is truncated", tag)
	}
	xNumerator, xDenominator := scanner.order.Uint32(data[2:6]), scanner.order.Uint32(data[6:10])
	yNumerator, yDenominator := scanner.order.Uint32(data[10:14]), scanner.order.Uint32(data[14:18])
	if xDenominator == 0 || yDenominator == 0 {
		return [2]float64{}, fmt.Errorf("invalid rational in DNG IlluminantData tag %d", tag)
	}
	white := [2]float64{float64(xNumerator) / float64(xDenominator), float64(yNumerator) / float64(yDenominator)}
	if _, _, err := dngTemperatureTint(white); err != nil {
		return [2]float64{}, fmt.Errorf("invalid DNG IlluminantData tag %d: %w", tag, err)
	}
	return white, nil
}

func dngXYFromTemperature(temperature float64) [2]float64 {
	r := 1e6 / temperature
	for index := 0; index <= 29; index++ {
		if r >= dngTemperatureLocus[index+1][0] && index != 29 {
			continue
		}
		fraction := (dngTemperatureLocus[index+1][0] - r) / (dngTemperatureLocus[index+1][0] - dngTemperatureLocus[index][0])
		u := dngTemperatureLocus[index][1]*fraction + dngTemperatureLocus[index+1][1]*(1-fraction)
		v := dngTemperatureLocus[index][2]*fraction + dngTemperatureLocus[index+1][2]*(1-fraction)
		denominator := u - 4*v + 2
		return [2]float64{1.5 * u / denominator, v / denominator}
	}
	return [2]float64{0.3457, 0.3585}
}

func mixDNGMatrices3(matrices [3]dngMatrix, weights [3]float64) (result dngMatrix) {
	for row := range result {
		for column := range result[row] {
			for index := 0; index < 3; index++ {
				result[row][column] += weights[index] * matrices[index][row][column]
			}
		}
	}
	return result
}

func dngTripleProfilePresent(fields *dngFields) bool {
	for _, tag := range []uint16{52529, 52530, 52531, 52532, 52537, 52538} {
		if _, found := fields.entries[tag]; found {
			return true
		}
	}
	return false
}

func dngCameraToSpaceTriple(fields *dngFields, xyzToRGB dngMatrix) (dngMatrix, [3]float64, error) {
	var zeroWeights [3]float64
	color1, hasColor1 := fields.matrix(50721, dngIdentityMatrix)
	color2, hasColor2 := fields.matrix(50722, color1)
	color3, hasColor3 := fields.matrix(52531, color2)
	calibration1, _ := fields.matrix(50723, dngIdentityMatrix)
	calibration2, _ := fields.matrix(50724, calibration1)
	calibration3, _ := fields.matrix(52530, calibration2)
	_, hasReduction1 := fields.matrix(50725, dngIdentityMatrix)
	_, hasReduction2 := fields.matrix(50726, dngIdentityMatrix)
	_, hasReduction3 := fields.matrix(52538, dngIdentityMatrix)
	forward1, hasForward1 := fields.matrix(50964, dngIdentityMatrix)
	forward2, hasForward2 := fields.matrix(50965, forward1)
	forward3, hasForward3 := fields.matrix(52532, forward2)
	if fields.err != nil {
		return dngMatrix{}, zeroWeights, fields.err
	}
	if !hasColor1 || !hasColor2 || !hasColor3 {
		return dngMatrix{}, zeroWeights, fmt.Errorf("triple-illuminant DNG requires ColorMatrix1, ColorMatrix2, and ColorMatrix3")
	}
	forwardCount := 0
	for _, present := range []bool{hasForward1, hasForward2, hasForward3} {
		if present {
			forwardCount++
		}
	}
	if forwardCount != 0 && forwardCount != 3 {
		return dngMatrix{}, zeroWeights, fmt.Errorf("triple-illuminant DNG requires all three ForwardMatrix tags or none")
	}
	reductionCount := 0
	for _, present := range []bool{hasReduction1, hasReduction2, hasReduction3} {
		if present {
			reductionCount++
		}
	}
	if reductionCount != 0 && reductionCount != 3 {
		return dngMatrix{}, zeroWeights, fmt.Errorf("triple-illuminant DNG requires all three ReductionMatrix tags or none")
	}
	hueMapCount := 0
	for _, tag := range []uint16{50938, 50939, 52537} {
		if _, found := fields.entries[tag]; found {
			hueMapCount++
		}
	}
	if hueMapCount != 0 && hueMapCount != 3 {
		return dngMatrix{}, zeroWeights, fmt.Errorf("triple-illuminant DNG requires all three hue/saturation tables or none")
	}

	codes := [3]uint16{50778, 50779, 52529}
	dataTags := [3]uint16{52533, 52534, 52535}
	var illuminants [3][2]float64
	for index := range illuminants {
		illuminants[index], fields.err = dngIlluminantWhiteXY(fields, codes[index], dataTags[index])
		if fields.err != nil {
			return dngMatrix{}, zeroWeights, fields.err
		}
		for earlier := 0; earlier < index; earlier++ {
			if illuminants[index] == illuminants[earlier] {
				return dngMatrix{}, zeroWeights, fmt.Errorf("triple-illuminant DNG calibration whites must be distinct")
			}
		}
	}

	calibrationSignature := fields.text(50931)
	profileSignature := fields.text(50932)
	if fields.err != nil {
		return dngMatrix{}, zeroWeights, fields.err
	}
	if calibrationSignature != profileSignature {
		calibration1, calibration2, calibration3 = dngIdentityMatrix, dngIdentityMatrix, dngIdentityMatrix
	}

	analog := [3]float64{1, 1, 1}
	if values := fields.values(50727); len(values) != 0 {
		if len(values) != 3 {
			return dngMatrix{}, zeroWeights, fmt.Errorf("DNG AnalogBalance must have three values")
		}
		copy(analog[:], values)
	}
	for _, value := range analog {
		if value <= 0 {
			return dngMatrix{}, zeroWeights, fmt.Errorf("DNG AnalogBalance must be positive")
		}
	}
	neutralValues := fields.values(50728)
	whiteXY := fields.values(50729)
	if fields.err != nil {
		return dngMatrix{}, zeroWeights, fields.err
	}
	if len(neutralValues) != 3 && len(whiteXY) != 2 {
		return dngMatrix{}, zeroWeights, fmt.Errorf("Linear DNG requires AsShotNeutral or AsShotWhiteXY")
	}
	var neutral [3]float64
	for index := range neutralValues {
		if index < len(neutral) {
			neutral[index] = neutralValues[index]
		}
	}
	for _, value := range neutral {
		if len(neutralValues) == 3 && value <= 0 {
			return dngMatrix{}, zeroWeights, fmt.Errorf("DNG AsShotNeutral must be positive")
		}
	}

	colors := [3]dngMatrix{color1, color2, color3}
	calibrations := [3]dngMatrix{calibration1, calibration2, calibration3}
	forwards := [3]dngMatrix{forward1, forward2, forward3}
	weights := [3]float64{1.0 / 3, 1.0 / 3, 1.0 / 3}
	if len(neutralValues) != 3 {
		if whiteXY[0] <= 0 || whiteXY[1] <= 0 || whiteXY[0]+whiteXY[1] >= 1 {
			return dngMatrix{}, zeroWeights, fmt.Errorf("invalid DNG AsShotWhiteXY")
		}
		weights, fields.err = dngTripleIlluminantWeights([2]float64{whiteXY[0], whiteXY[1]}, illuminants)
		if fields.err != nil {
			return dngMatrix{}, zeroWeights, fields.err
		}
		xyz := [3]float64{whiteXY[0] / whiteXY[1], 1, (1 - whiteXY[0] - whiteXY[1]) / whiteXY[1]}
		cameraFromXYZ := dngDiagonal(analog).multiply(mixDNGMatrices3(calibrations, weights)).multiply(mixDNGMatrices3(colors, weights))
		neutral = cameraFromXYZ.vector(xyz)
	} else {
		for iteration := 0; iteration < 32; iteration++ {
			cameraFromXYZ := dngDiagonal(analog).multiply(mixDNGMatrices3(calibrations, weights)).multiply(mixDNGMatrices3(colors, weights))
			xyzFromCamera, err := cameraFromXYZ.inverse()
			if err != nil {
				return dngMatrix{}, zeroWeights, err
			}
			white := xyzFromCamera.vector(neutral)
			sum := white[0] + white[1] + white[2]
			if sum <= 0 || white[1] <= 0 || math.IsNaN(sum) || math.IsInf(sum, 0) {
				return dngMatrix{}, zeroWeights, fmt.Errorf("invalid DNG scene white")
			}
			xy := [2]float64{white[0] / sum, white[1] / sum}
			next, err := dngTripleIlluminantWeights(xy, illuminants)
			if err != nil {
				return dngMatrix{}, zeroWeights, err
			}
			delta := 0.0
			for index := range weights {
				delta = math.Max(delta, math.Abs(weights[index]-next[index]))
			}
			weights = next
			if delta < 1e-10 {
				break
			}
		}
	}
	for _, value := range neutral {
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return dngMatrix{}, zeroWeights, fmt.Errorf("DNG AsShotNeutral must be positive")
		}
	}
	if fields.err != nil {
		return dngMatrix{}, zeroWeights, fields.err
	}

	calibration := mixDNGMatrices3(calibrations, weights)
	colorMatrix := mixDNGMatrices3(colors, weights)
	nativeToReference, err := dngDiagonal(analog).multiply(calibration).inverse()
	if err != nil {
		return dngMatrix{}, zeroWeights, err
	}
	if forwardCount == 3 {
		referenceNeutral := nativeToReference.vector(neutral)
		var whiteBalance [3]float64
		for channel, value := range referenceNeutral {
			if value <= 0 {
				return dngMatrix{}, zeroWeights, fmt.Errorf("invalid calibrated DNG neutral")
			}
			whiteBalance[channel] = 1 / value
		}
		return xyzToRGB.multiply(mixDNGMatrices3(forwards, weights)).multiply(dngDiagonal(whiteBalance)).multiply(nativeToReference), weights, nil
	}

	xyzFromCamera, err := dngDiagonal(analog).multiply(calibration).multiply(colorMatrix).inverse()
	if err != nil {
		return dngMatrix{}, zeroWeights, err
	}
	white := xyzFromCamera.vector(neutral)
	if white[1] <= 0 {
		return dngMatrix{}, zeroWeights, fmt.Errorf("invalid DNG scene white")
	}
	for row := range xyzFromCamera {
		for column := range xyzFromCamera[row] {
			xyzFromCamera[row][column] /= white[1]
		}
	}
	white = [3]float64{white[0] / white[1], 1, white[2] / white[1]}
	adaptation, err := dngChromaticAdaptation(white, dngWhiteD50)
	if err != nil {
		return dngMatrix{}, zeroWeights, err
	}
	return xyzToRGB.multiply(adaptation).multiply(xyzFromCamera), weights, nil
}
