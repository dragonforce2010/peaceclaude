package remote

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// qrSpec encodes ISO-18004 EC Level M parameters for Versions 1..10.
type qrSpec struct {
	version    int
	size       int
	totalBytes int
	dataBytes  int
	ecPerBlock int
	g1Blocks   int
	g1Data     int
	g2Blocks   int
	g2Data     int
	remBits    int
	alignPos   []int
}

var qrSpecsM = []qrSpec{
	{version: 1, size: 21, totalBytes: 26, dataBytes: 16, ecPerBlock: 10, g1Blocks: 1, g1Data: 16, remBits: 0, alignPos: nil},
	{version: 2, size: 25, totalBytes: 44, dataBytes: 28, ecPerBlock: 16, g1Blocks: 1, g1Data: 28, remBits: 7, alignPos: []int{6, 18}},
	{version: 3, size: 29, totalBytes: 70, dataBytes: 44, ecPerBlock: 26, g1Blocks: 1, g1Data: 44, remBits: 7, alignPos: []int{6, 22}},
	{version: 4, size: 33, totalBytes: 100, dataBytes: 64, ecPerBlock: 18, g1Blocks: 2, g1Data: 32, remBits: 7, alignPos: []int{6, 26}},
	{version: 5, size: 37, totalBytes: 134, dataBytes: 86, ecPerBlock: 24, g1Blocks: 2, g1Data: 43, remBits: 7, alignPos: []int{6, 30}},
	{version: 6, size: 41, totalBytes: 172, dataBytes: 108, ecPerBlock: 16, g1Blocks: 4, g1Data: 27, remBits: 7, alignPos: []int{6, 34}},
	{version: 7, size: 45, totalBytes: 196, dataBytes: 124, ecPerBlock: 18, g1Blocks: 4, g1Data: 31, remBits: 0, alignPos: []int{6, 22, 38}},
	{version: 8, size: 49, totalBytes: 242, dataBytes: 154, ecPerBlock: 22, g1Blocks: 2, g1Data: 38, g2Blocks: 2, g2Data: 39, remBits: 0, alignPos: []int{6, 24, 42}},
	{version: 9, size: 53, totalBytes: 292, dataBytes: 182, ecPerBlock: 22, g1Blocks: 3, g1Data: 36, g2Blocks: 2, g2Data: 37, remBits: 0, alignPos: []int{6, 26, 46}},
	{version: 10, size: 57, totalBytes: 346, dataBytes: 216, ecPerBlock: 26, g1Blocks: 4, g1Data: 43, g2Blocks: 1, g2Data: 44, remBits: 3, alignPos: []int{6, 28, 50}},
}

var gfExp [512]byte
var gfLog [256]byte

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11d
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

func rsGenerator(ecCount int) []byte {
	poly := []byte{1}
	for i := 0; i < ecCount; i++ {
		next := make([]byte, len(poly)+1)
		root := gfExp[i]
		for j, coef := range poly {
			next[j] ^= coef
			next[j+1] ^= gfMul(coef, root)
		}
		poly = next
	}
	return poly
}

func rsRemainder(data []byte, gen []byte) []byte {
	ecCount := len(gen) - 1
	rem := make([]byte, len(data)+ecCount)
	copy(rem, data)
	for i := 0; i < len(data); i++ {
		lead := rem[i]
		if lead == 0 {
			continue
		}
		for j := 0; j < len(gen); j++ {
			rem[i+j] ^= gfMul(gen[j], lead)
		}
	}
	return rem[len(data):]
}

// EncodeQRMatrix encodes payload into a Byte-mode EC-Level-M QR code boolean matrix.
func EncodeQRMatrix(payload string) ([][]bool, error) {
	raw := []byte(payload)
	if len(raw) == 0 {
		return nil, errors.New("QR payload is empty")
	}
	var spec *qrSpec
	for i := range qrSpecsM {
		s := &qrSpecsM[i]
		ccBits := 8
		if s.version >= 10 {
			ccBits = 16
		}
		needBits := 4 + ccBits + len(raw)*8
		if needBits <= s.dataBytes*8 {
			spec = s
			break
		}
	}
	if spec == nil {
		return nil, fmt.Errorf("QR payload too long (%d bytes, max 213)", len(raw))
	}

	bits := make([]byte, 0, spec.dataBytes*8)
	appendBits := func(val, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, byte((val>>i)&1))
		}
	}
	appendBits(0b0100, 4) // Byte mode
	ccBits := 8
	if spec.version >= 10 {
		ccBits = 16
	}
	appendBits(len(raw), ccBits)
	for _, b := range raw {
		appendBits(int(b), 8)
	}
	capBits := spec.dataBytes * 8
	term := capBits - len(bits)
	if term > 4 {
		term = 4
	}
	appendBits(0, term)
	for len(bits)%8 != 0 {
		bits = append(bits, 0)
	}
	padBytes := [2]byte{0xec, 0x11}
	for padIdx := 0; len(bits) < capBits; padIdx++ {
		appendBits(int(padBytes[padIdx%2]), 8)
	}

	dataWords := make([]byte, spec.dataBytes)
	for i := 0; i < spec.dataBytes; i++ {
		var b byte
		for k := 0; k < 8; k++ {
			b = (b << 1) | bits[i*8+k]
		}
		dataWords[i] = b
	}

	// Split blocks & compute ECC
	gen := rsGenerator(spec.ecPerBlock)
	totalBlocks := spec.g1Blocks + spec.g2Blocks
	dataBlocks := make([][]byte, totalBlocks)
	ecBlocks := make([][]byte, totalBlocks)
	offset := 0
	for i := 0; i < totalBlocks; i++ {
		blen := spec.g1Data
		if i >= spec.g1Blocks {
			blen = spec.g2Data
		}
		blk := dataWords[offset : offset+blen]
		offset += blen
		dataBlocks[i] = blk
		ecBlocks[i] = rsRemainder(blk, gen)
	}

	// Interleave data codewords, then EC codewords
	finalWords := make([]byte, 0, spec.totalBytes)
	maxDataLen := spec.g1Data
	if spec.g2Data > maxDataLen {
		maxDataLen = spec.g2Data
	}
	for col := 0; col < maxDataLen; col++ {
		for i := 0; i < totalBlocks; i++ {
			if col < len(dataBlocks[i]) {
				finalWords = append(finalWords, dataBlocks[i][col])
			}
		}
	}
	for col := 0; col < spec.ecPerBlock; col++ {
		for i := 0; i < totalBlocks; i++ {
			finalWords = append(finalWords, ecBlocks[i][col])
		}
	}

	n := spec.size
	modules := make([][]bool, n)
	isFunc := make([][]bool, n)
	for i := 0; i < n; i++ {
		modules[i] = make([]bool, n)
		isFunc[i] = make([]bool, n)
	}

	setFunc := func(r, c int, dark bool) {
		if r < 0 || r >= n || c < 0 || c >= n {
			return
		}
		modules[r][c] = dark
		isFunc[r][c] = true
	}

	drawFinder := func(tr, tc int) {
		for dr := -1; dr <= 7; dr++ {
			for dc := -1; dc <= 7; dc++ {
				r, c := tr+dr, tc+dc
				if r < 0 || r >= n || c < 0 || c >= n {
					continue
				}
				dark := false
				if dr >= 0 && dr <= 6 && dc >= 0 && dc <= 6 {
					dist := int(math.Max(math.Abs(float64(dr-3)), math.Abs(float64(dc-3))))
					dark = dist != 2
				}
				setFunc(r, c, dark)
			}
		}
	}

	drawFinder(0, 0)
	drawFinder(0, n-7)
	drawFinder(n-7, 0)

	// Timing patterns
	for i := 8; i < n-8; i++ {
		setFunc(6, i, i%2 == 0)
		setFunc(i, 6, i%2 == 0)
	}
	// Dark module
	setFunc(n-8, 8, true)

	// Alignment patterns (only skip cells that overlap the 3 corner finders, NOT the timing line)
	inFinder := func(r, c int) bool {
		return (r <= 8 && c <= 8) || (r <= 8 && c >= n-8) || (r >= n-8 && c <= 8)
	}
	for _, r := range spec.alignPos {
		for _, c := range spec.alignPos {
			if inFinder(r, c) {
				continue
			}
			for dr := -2; dr <= 2; dr++ {
				for dc := -2; dc <= 2; dc++ {
					dist := int(math.Max(math.Abs(float64(dr)), math.Abs(float64(dc))))
					setFunc(r+dr, c+dc, dist != 1)
				}
			}
		}
	}

	// Reserve format info cells
	for i := 0; i < 9; i++ {
		if i != 6 {
			setFunc(8, i, false)
			setFunc(i, 8, false)
		}
	}
	for i := 0; i < 8; i++ {
		setFunc(8, n-1-i, false)
		if i < 7 {
			setFunc(n-1-i, 8, false)
		}
	}

	// Version info for >= 7
	if spec.version >= 7 {
		v := spec.version
		rem := v
		for i := 0; i < 12; i++ {
			rem = (rem << 1) ^ ((rem >> 11) * 0x1f25)
		}
		vbits := (v << 12) | rem
		for i := 0; i < 18; i++ {
			dark := ((vbits >> i) & 1) != 0
			a := n - 11 + (i % 3)
			b := i / 3
			setFunc(a, b, dark)
			setFunc(b, a, dark)
		}
	}

	// Place data + EC bits in zigzag columns
	bitIdx := 0
	totalBits := len(finalWords) * 8
	upward := true
	for right := n - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for step := 0; step < n; step++ {
			r := step
			if upward {
				r = n - 1 - step
			}
			for dx := 0; dx < 2; dx++ {
				c := right - dx
				if isFunc[r][c] {
					continue
				}
				var bit bool
				if bitIdx < totalBits {
					bit = ((finalWords[bitIdx>>3] >> (7 - (bitIdx & 7))) & 1) != 0
					bitIdx++
				}
				modules[r][c] = bit
			}
		}
		upward = !upward
	}

	// Evaluate all 8 masks and apply lowest penalty
	bestMask := 0
	bestPenalty := 1 << 30
	bestGrid := modules
	for mask := 0; mask < 8; mask++ {
		cand := applyQRMask(modules, isFunc, mask)
		writeQRFormatBits(cand, mask)
		p := scoreQRPenalty(cand)
		if p < bestPenalty {
			bestPenalty = p
			bestMask = mask
			bestGrid = cand
		}
	}
	_ = bestMask
	return bestGrid, nil
}

func maskBit(mask, r, c int) bool {
	switch mask {
	case 0:
		return (r+c)%2 == 0
	case 1:
		return r%2 == 0
	case 2:
		return c%3 == 0
	case 3:
		return (r+c)%3 == 0
	case 4:
		return (r/2+c/3)%2 == 0
	case 5:
		return (r*c)%2+(r*c)%3 == 0
	case 6:
		return ((r*c)%2+(r*c)%3)%2 == 0
	default:
		return ((r+c)%2+(r*c)%3)%2 == 0
	}
}

func applyQRMask(src, isFunc [][]bool, mask int) [][]bool {
	n := len(src)
	out := make([][]bool, n)
	for r := 0; r < n; r++ {
		out[r] = make([]bool, n)
		for c := 0; c < n; c++ {
			if isFunc[r][c] {
				out[r][c] = src[r][c]
			} else if maskBit(mask, r, c) {
				out[r][c] = !src[r][c]
			} else {
				out[r][c] = src[r][c]
			}
		}
	}
	return out
}

func writeQRFormatBits(grid [][]bool, mask int) {
	n := len(grid)
	// Level M = 00
	data := (0b00 << 3) | mask
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := ((data << 10) | rem) ^ 0x5412

	for i := 0; i <= 5; i++ {
		grid[8][i] = ((bits >> i) & 1) != 0
	}
	grid[8][7] = ((bits >> 6) & 1) != 0
	grid[8][8] = ((bits >> 7) & 1) != 0
	grid[7][8] = ((bits >> 8) & 1) != 0
	for i := 9; i < 15; i++ {
		grid[14-i][8] = ((bits >> i) & 1) != 0
	}

	for i := 0; i < 8; i++ {
		grid[8][n-1-i] = ((bits >> i) & 1) != 0
	}
	for i := 8; i < 15; i++ {
		grid[n-15+i][8] = ((bits >> i) & 1) != 0
	}
	grid[n-8][8] = true
}

func scoreQRPenalty(g [][]bool) int {
	n := len(g)
	penalty := 0
	// Rule 1: runs of 5+ same color
	for r := 0; r < n; r++ {
		run := 1
		for c := 1; c < n; c++ {
			if g[r][c] == g[r][c-1] {
				run++
			} else {
				if run >= 5 {
					penalty += run - 2
				}
				run = 1
			}
		}
		if run >= 5 {
			penalty += run - 2
		}
	}
	for c := 0; c < n; c++ {
		run := 1
		for r := 1; r < n; r++ {
			if g[r][c] == g[r-1][c] {
				run++
			} else {
				if run >= 5 {
					penalty += run - 2
				}
				run = 1
			}
		}
		if run >= 5 {
			penalty += run - 2
		}
	}
	// Rule 2: 2x2 blocks
	for r := 0; r < n-1; r++ {
		for c := 0; c < n-1; c++ {
			v := g[r][c]
			if g[r][c+1] == v && g[r+1][c] == v && g[r+1][c+1] == v {
				penalty += 3
			}
		}
	}
	// Rule 3: finder-like patterns 1:1:3:1:1 with 4 lights
	matchFinderLike := func(line []bool) int {
		pts := 0
		for i := 0; i <= len(line)-11; i++ {
			if line[i] && !line[i+1] && line[i+2] && line[i+3] && line[i+4] && !line[i+5] && line[i+6] &&
				!line[i+7] && !line[i+8] && !line[i+9] && !line[i+10] {
				pts += 40
			}
			if !line[i] && !line[i+1] && !line[i+2] && !line[i+3] && line[i+4] &&
				!line[i+5] && line[i+6] && line[i+7] && line[i+8] && !line[i+9] && line[i+10] {
				pts += 40
			}
		}
		return pts
	}
	for r := 0; r < n; r++ {
		penalty += matchFinderLike(g[r])
	}
	col := make([]bool, n)
	for c := 0; c < n; c++ {
		for r := 0; r < n; r++ {
			col[r] = g[r][c]
		}
		penalty += matchFinderLike(col)
	}
	// Rule 4: dark ratio vs 50%
	dark := 0
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if g[r][c] {
				dark++
			}
		}
	}
	pct := (dark * 100) / (n * n)
	dev := pct - 50
	if dev < 0 {
		dev = -dev
	}
	penalty += (dev / 5) * 10
	return penalty
}

// RenderQRSVG renders a clean crisp SVG QR code with a quiet zone of 4 modules.
func RenderQRSVG(payload string) (string, error) {
	matrix, err := EncodeQRMatrix(payload)
	if err != nil {
		return "", err
	}
	const quiet = 4
	n := len(matrix)
	vb := n + quiet*2
	var path strings.Builder
	for r := 0; r < n; r++ {
		c := 0
		for c < n {
			if !matrix[r][c] {
				c++
				continue
			}
			start := c
			for c < n && matrix[r][c] {
				c++
			}
			fmt.Fprintf(&path, "M%d %dh%dv1h-%dz", start+quiet, r+quiet, c-start, c-start)
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="100%%" height="100%%" fill="#ffffff"/><path fill="#111418" d="%s"/></svg>`,
		vb, vb, path.String()), nil
}
