package detect

import "math"

// MotionScore compares two raw RGB frames and returns a normalized score
// between 0.0 (identical) and 1.0 (completely different).
// Both frames must be the same length (width * height * 3 for RGB24).
func MotionScore(prev, curr []byte) float64 {
	if len(prev) != len(curr) || len(prev) == 0 {
		return 0
	}

	var totalDiff uint64
	pixels := len(prev) / 3

	for i := 0; i < len(prev); i += 3 {
		// Average the RGB channels for a simple luminance approximation
		prevLum := int(prev[i]) + int(prev[i+1]) + int(prev[i+2])
		currLum := int(curr[i]) + int(curr[i+1]) + int(curr[i+2])
		diff := prevLum - currLum
		if diff < 0 {
			diff = -diff
		}
		totalDiff += uint64(diff)
	}

	// Normalize: max diff per pixel is 255*3 = 765
	maxDiff := float64(pixels) * 765.0
	score := float64(totalDiff) / maxDiff

	return math.Min(score, 1.0)
}

// MotionRegion represents a contiguous area of detected motion.
type MotionRegion struct {
	Box   [4]int  // x1, y1, x2, y2
	Area  int
	Score float64 // fraction of pixels above threshold within the region
}

// MotionDetector performs contour-based motion detection using a running
// background model with adaptive alpha blending.
type MotionDetector struct {
	threshold uint8
	minArea   int
	bgAlpha   float64
	bg        []float64 // background model (grayscale, one value per pixel)
}

// NewMotionDetector creates a MotionDetector.
// threshold: pixel difference threshold for binary mask (0-255).
// minArea: minimum number of pixels for a motion region to be reported.
// bgAlpha: blending factor for background update (0.0 = static, 1.0 = instant).
func NewMotionDetector(threshold uint8, minArea int, bgAlpha float64) *MotionDetector {
	return &MotionDetector{
		threshold: threshold,
		minArea:   minArea,
		bgAlpha:   bgAlpha,
	}
}

// Detect processes an RGB24 frame and returns bounding boxes of motion regions.
func (m *MotionDetector) Detect(frame []byte, width, height int) []MotionRegion {
	pixels := width * height
	if len(frame) != pixels*3 {
		return nil
	}

	// Convert to grayscale
	gray := make([]uint8, pixels)
	for i := 0; i < pixels; i++ {
		off := i * 3
		// Fast luminance approximation: (R + G + G + B) >> 2
		gray[i] = uint8((int(frame[off]) + int(frame[off+1])*2 + int(frame[off+2])) >> 2)
	}

	// Initialize background model on first frame
	if m.bg == nil {
		m.bg = make([]float64, pixels)
		for i, v := range gray {
			m.bg[i] = float64(v)
		}
		return nil
	}

	// Apply 3x3 box blur to reduce noise
	blurred := boxBlur3x3(gray, width, height)

	// Compute absolute difference against background and threshold to binary
	binary := make([]uint8, pixels)
	var totalFG int
	for i := 0; i < pixels; i++ {
		diff := float64(blurred[i]) - m.bg[i]
		if diff < 0 {
			diff = -diff
		}
		if diff > float64(m.threshold) {
			binary[i] = 1
			totalFG++
		}
	}

	// Update background model with alpha blending
	for i := 0; i < pixels; i++ {
		m.bg[i] = m.bg[i]*(1-m.bgAlpha) + float64(blurred[i])*m.bgAlpha
	}

	if totalFG == 0 {
		return nil
	}

	// Connected component labeling to find contiguous motion regions
	regions := connectedComponents(binary, width, height, m.minArea)

	// Compute score for each region
	for i := range regions {
		r := &regions[i]
		regionPixels := (r.Box[2] - r.Box[0]) * (r.Box[3] - r.Box[1])
		if regionPixels > 0 {
			r.Score = float64(r.Area) / float64(regionPixels)
		}
	}

	return regions
}

// boxBlur3x3 applies a simple 3x3 box blur to a grayscale image.
func boxBlur3x3(src []uint8, w, h int) []uint8 {
	dst := make([]uint8, w*h)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sum int
			var count int
			for dy := -1; dy <= 1; dy++ {
				ny := y + dy
				if ny < 0 || ny >= h {
					continue
				}
				for dx := -1; dx <= 1; dx++ {
					nx := x + dx
					if nx < 0 || nx >= w {
						continue
					}
					sum += int(src[ny*w+nx])
					count++
				}
			}
			dst[y*w+x] = uint8(sum / count)
		}
	}
	return dst
}

// connectedComponents performs two-pass connected component labeling on a
// binary image and returns bounding boxes of components with area >= minArea.
func connectedComponents(binary []uint8, w, h, minArea int) []MotionRegion {
	labels := make([]int, w*h)
	parent := []int{0} // index 0 unused, labels start at 1
	nextLabel := 1

	// find returns the root of the union-find tree
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}

	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			if ra > rb {
				ra, rb = rb, ra
			}
			parent[rb] = ra
		}
	}

	// First pass: assign provisional labels
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := y*w + x
			if binary[idx] == 0 {
				continue
			}

			var neighbors []int
			if x > 0 && labels[idx-1] > 0 {
				neighbors = append(neighbors, labels[idx-1])
			}
			if y > 0 && labels[idx-w] > 0 {
				neighbors = append(neighbors, labels[idx-w])
			}

			if len(neighbors) == 0 {
				labels[idx] = nextLabel
				parent = append(parent, nextLabel)
				nextLabel++
			} else {
				minLabel := neighbors[0]
				for _, n := range neighbors[1:] {
					if find(n) < find(minLabel) {
						minLabel = n
					}
				}
				labels[idx] = find(minLabel)
				for _, n := range neighbors {
					union(minLabel, n)
				}
			}
		}
	}

	// Second pass: resolve labels and collect bounding boxes
	type bbox struct {
		x1, y1, x2, y2, area int
	}
	boxes := map[int]*bbox{}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := y*w + x
			if labels[idx] == 0 {
				continue
			}
			root := find(labels[idx])
			labels[idx] = root

			b, ok := boxes[root]
			if !ok {
				b = &bbox{x1: x, y1: y, x2: x + 1, y2: y + 1}
				boxes[root] = b
			}
			if x < b.x1 {
				b.x1 = x
			}
			if y < b.y1 {
				b.y1 = y
			}
			if x+1 > b.x2 {
				b.x2 = x + 1
			}
			if y+1 > b.y2 {
				b.y2 = y + 1
			}
			b.area++
		}
	}

	// Filter by minimum area
	var regions []MotionRegion
	for _, b := range boxes {
		if b.area >= minArea {
			regions = append(regions, MotionRegion{
				Box:  [4]int{b.x1, b.y1, b.x2, b.y2},
				Area: b.area,
			})
		}
	}
	return regions
}
