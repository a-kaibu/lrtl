package lrtl

import "math"

// lowessSmooth applies a tricube-weighted local linear regression using a
// fixed number of nearest samples. The Holy Grail caller translates LRT's
// bandwidth setting and retry behavior into this sample count.
func lowessSmooth(values []float64, neighbors int) []float64 {
	result := append([]float64(nil), values...)
	if len(values) < 3 || neighbors < 2 {
		return result
	}
	if neighbors > len(values) {
		neighbors = len(values)
	}

	left, right := 0, neighbors-1
	for index := range values {
		if index > 0 && right+1 < len(values) {
			rightDistance := float64(right + 1 - index)
			leftDistance := float64(index - left)
			if rightDistance < leftDistance {
				left++
				right++
			}
		}

		radius := math.Max(float64(index-left), float64(right-index))
		if radius == 0 {
			continue
		}

		var weightSum, weightedX, weightedY float64
		var weightedXX, weightedXY float64
		for sample := left; sample <= right; sample++ {
			delta := float64(sample - index)
			weight := tricubeWeight(math.Abs(delta) / radius)
			weightSum += weight
			weightedX += weight * delta
			weightedY += weight * values[sample]
			weightedXX += weight * delta * delta
			weightedXY += weight * delta * values[sample]
		}
		if weightSum == 0 {
			continue
		}

		meanX := weightedX / weightSum
		meanY := weightedY / weightSum
		variance := weightedXX/weightSum - meanX*meanX
		if variance <= 1e-20 {
			result[index] = meanY
			continue
		}
		covariance := weightedXY/weightSum - meanX*meanY
		result[index] = meanY - (covariance/variance)*meanX
	}
	return result
}

func tricubeWeight(distance float64) float64 {
	if distance >= 1 {
		return 0
	}
	remaining := 1 - distance*distance*distance
	return remaining * remaining * remaining
}
