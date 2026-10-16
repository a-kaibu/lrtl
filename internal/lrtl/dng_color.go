package lrtl

import (
	"fmt"
	"math"
)

type dngMatrix [3][3]float64

var dngIdentityMatrix = dngMatrix{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
var dngWhiteD50 = [3]float64{0.96422, 1, 0.82521}
var dngXYZD50ToSRGB = dngMatrix{
	{3.1338561, -1.6168667, -0.4906146},
	{-0.9787684, 1.9161415, 0.0334540},
	{0.0719453, -0.2289914, 1.4052427},
}

func (matrix dngMatrix) vector(value [3]float64) (result [3]float64) {
	for row := range result {
		for column := range value {
			result[row] += matrix[row][column] * value[column]
		}
	}
	return result
}

func (matrix dngMatrix) multiply(right dngMatrix) (result dngMatrix) {
	for row := range result {
		for column := range result[row] {
			for index := 0; index < 3; index++ {
				result[row][column] += matrix[row][index] * right[index][column]
			}
		}
	}
	return result
}

func (matrix dngMatrix) inverse() (dngMatrix, error) {
	a, b, c := matrix[0][0], matrix[0][1], matrix[0][2]
	d, e, f := matrix[1][0], matrix[1][1], matrix[1][2]
	g, h, i := matrix[2][0], matrix[2][1], matrix[2][2]
	determinant := a*(e*i-f*h) - b*(d*i-f*g) + c*(d*h-e*g)
	if math.IsNaN(determinant) || math.IsInf(determinant, 0) || math.Abs(determinant) < 1e-12 {
		return dngMatrix{}, fmt.Errorf("singular or invalid DNG color matrix")
	}
	result := dngMatrix{{e*i - f*h, c*h - b*i, b*f - c*e}, {f*g - d*i, a*i - c*g, c*d - a*f}, {d*h - e*g, b*g - a*h, a*e - b*d}}
	for row := range result {
		for column := range result[row] {
			result[row][column] /= determinant
		}
	}
	return result, nil
}

func dngDiagonal(vector [3]float64) dngMatrix {
	return dngMatrix{{vector[0], 0, 0}, {0, vector[1], 0}, {0, 0, vector[2]}}
}

func mixDNGMatrix(first, second dngMatrix, weight float64) (result dngMatrix) {
	for row := range result {
		for column := range result[row] {
			result[row][column] = (1-weight)*first[row][column] + weight*second[row][column]
		}
	}
	return result
}

func (fields *dngFields) matrix(tag uint16, fallback dngMatrix) (dngMatrix, bool) {
	values := fields.values(tag)
	if len(values) == 0 {
		return fallback, false
	}
	if len(values) != 9 {
		fields.err = fmt.Errorf("DNG color matrix tag %d must contain nine coefficients", tag)
		return fallback, false
	}
	var result dngMatrix
	for index, value := range values {
		result[index/3][index%3] = value
	}
	return result, true
}

func dngIlluminantTemperature(code int) float64 {
	switch code {
	case 1, 4, 9, 20:
		return 5500
	case 2, 14:
		return 4150
	case 3:
		return 2850
	case 10, 21:
		return 6500
	case 11, 22:
		return 7500
	case 12:
		return 6430
	case 13, 23:
		return 5000
	case 15:
		return 3450
	case 17:
		return 2856
	case 18:
		return 4874
	case 19:
		return 6774
	case 24:
		return 3200
	}
	return 0
}

func dngWhiteTemperature(xyz [3]float64) float64 {
	sum := xyz[0] + xyz[1] + xyz[2]
	if sum <= 0 {
		return 6500
	}
	x, y := xyz[0]/sum, xyz[1]/sum
	if math.Abs(y-0.1858) < 1e-9 {
		return 6500
	}
	n := (x - 0.332) / (y - 0.1858)
	return min(50000, max(2000, -449*n*n*n+3525*n*n-6823.3*n+5520.33))
}

func dngChromaticAdaptation(source, destination [3]float64) (dngMatrix, error) {
	bradford := dngMatrix{{0.8951, 0.2664, -0.1614}, {-0.7502, 1.7135, 0.0367}, {0.0389, -0.0685, 1.0296}}
	inverse, err := bradford.inverse()
	if err != nil {
		return dngMatrix{}, err
	}
	before, after := bradford.vector(source), bradford.vector(destination)
	var gains [3]float64
	for channel := range gains {
		if before[channel] <= 0 || after[channel] <= 0 {
			return dngMatrix{}, fmt.Errorf("invalid DNG white point")
		}
		gains[channel] = after[channel] / before[channel]
	}
	return inverse.multiply(dngDiagonal(gains)).multiply(bradford), nil
}

func dngCameraToSpace(fields *dngFields, xyzToRGB dngMatrix) (dngMatrix, error) {
	color1, hasColor1 := fields.matrix(50721, dngIdentityMatrix)
	color2, hasColor2 := fields.matrix(50722, color1)
	calibration1, _ := fields.matrix(50723, dngIdentityMatrix)
	calibration2, _ := fields.matrix(50724, calibration1)
	forward1, hasForward1 := fields.matrix(50964, dngIdentityMatrix)
	forward2, hasForward2 := fields.matrix(50965, forward1)
	if !hasColor1 && hasColor2 {
		color1 = color2
	}
	if !hasForward1 && hasForward2 {
		forward1 = forward2
	}
	if !hasColor1 && !hasColor2 && !hasForward1 && !hasForward2 {
		return dngMatrix{}, fmt.Errorf("Linear DNG has no supported camera color matrix")
	}
	analog := [3]float64{1, 1, 1}
	if values := fields.values(50727); len(values) != 0 {
		if len(values) != 3 {
			return dngMatrix{}, fmt.Errorf("DNG AnalogBalance must have three values")
		}
		copy(analog[:], values)
	}
	for _, value := range analog {
		if value <= 0 {
			return dngMatrix{}, fmt.Errorf("DNG AnalogBalance must be positive")
		}
	}
	neutralValues := fields.values(50728)
	whiteXY := fields.values(50729)
	if len(neutralValues) != 3 && len(whiteXY) != 2 {
		return dngMatrix{}, fmt.Errorf("Linear DNG requires AsShotNeutral or AsShotWhiteXY")
	}
	var neutral [3]float64
	if len(neutralValues) == 3 {
		copy(neutral[:], neutralValues)
	} else {
		if whiteXY[0] <= 0 || whiteXY[1] <= 0 || whiteXY[0]+whiteXY[1] >= 1 || (!hasColor1 && !hasColor2) {
			return dngMatrix{}, fmt.Errorf("invalid DNG AsShotWhiteXY")
		}
		xyz := [3]float64{whiteXY[0] / whiteXY[1], 1, (1 - whiteXY[0] - whiteXY[1]) / whiteXY[1]}
		neutral = dngDiagonal(analog).multiply(calibration2).multiply(color2).vector(xyz)
	}
	for _, value := range neutral {
		if value <= 0 {
			return dngMatrix{}, fmt.Errorf("DNG AsShotNeutral must be positive")
		}
	}
	firstTemperature := dngIlluminantTemperature(fields.integer(50778, 0))
	secondTemperature := dngIlluminantTemperature(fields.integer(50779, 0))
	if fields.err != nil {
		return dngMatrix{}, fields.err
	}
	weight := 0.0
	if hasColor2 || hasForward2 {
		weight = 1
	}
	if hasColor1 && hasColor2 && firstTemperature > 0 && secondTemperature > 0 && firstTemperature != secondTemperature {
		// Estimate the scene white from the camera neutral and interpolate in
		// reciprocal temperature. McCamy CCT is an independent approximation;
		// Adobe's complete profile rendering is not reproduced here.
		weight = 0.5
		for iteration := 0; iteration < 8; iteration++ {
			calibration := mixDNGMatrix(calibration1, calibration2, weight)
			matrix := dngDiagonal(analog).multiply(calibration).multiply(mixDNGMatrix(color1, color2, weight))
			inverse, err := matrix.inverse()
			if err != nil {
				return dngMatrix{}, err
			}
			temperature := dngWhiteTemperature(inverse.vector(neutral))
			weight = min(1, max(0, (1/temperature-1/firstTemperature)/(1/secondTemperature-1/firstTemperature)))
		}
	}
	nativeToReference, err := dngDiagonal(analog).multiply(mixDNGMatrix(calibration1, calibration2, weight)).inverse()
	if err != nil {
		return dngMatrix{}, err
	}
	if hasForward1 || hasForward2 {
		referenceNeutral := nativeToReference.vector(neutral)
		var whiteBalance [3]float64
		for channel, value := range referenceNeutral {
			if value <= 0 {
				return dngMatrix{}, fmt.Errorf("invalid calibrated DNG neutral")
			}
			whiteBalance[channel] = 1 / value
		}
		forward := mixDNGMatrix(forward1, forward2, weight)
		return xyzToRGB.multiply(forward).multiply(dngDiagonal(whiteBalance)).multiply(nativeToReference), nil
	}
	xyzFromCamera, err := dngDiagonal(analog).multiply(mixDNGMatrix(calibration1, calibration2, weight)).multiply(mixDNGMatrix(color1, color2, weight)).inverse()
	if err != nil {
		return dngMatrix{}, err
	}
	white := xyzFromCamera.vector(neutral)
	if white[1] <= 0 {
		return dngMatrix{}, fmt.Errorf("invalid DNG scene white")
	}
	for row := range xyzFromCamera {
		for column := range xyzFromCamera[row] {
			xyzFromCamera[row][column] /= white[1]
		}
	}
	white = [3]float64{white[0] / white[1], 1, white[2] / white[1]}
	adaptation, err := dngChromaticAdaptation(white, dngWhiteD50)
	if err != nil {
		return dngMatrix{}, err
	}
	return xyzToRGB.multiply(adaptation).multiply(xyzFromCamera), nil
}
