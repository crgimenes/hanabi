package effect

import (
	"math"

	"github.com/crgimenes/hanabi/canvas"
)

// Value noise with an integer hash, lifted from crgimenes/loiterglass
// (noise.go, MIT) rather than rewritten: it is the coherent noise everyone
// otherwise imports a library for, and it carries no dependency at all.

// #nosec G115 -- wrapping is the point: this is a hash, and the sign of the
// coordinate carries no meaning once it is mixed.
func hash3(x, y, z int32) float32 {
	h := uint32(x)*374761393 + uint32(y)*668265263 + uint32(z)*2147483647
	h = (h ^ (h >> 13)) * 1274126177
	return float32(h^(h>>16)) / float32(math.MaxUint32)
}

func lerpf(a, b, t float32) float32 { return a + (b-a)*t }

func valueNoise3(x, y, z float32) float32 {
	xi := int32(math.Floor(float64(x)))
	yi := int32(math.Floor(float64(y)))
	zi := int32(math.Floor(float64(z)))
	fx, fy, fz := x-float32(xi), y-float32(yi), z-float32(zi)
	ux := fx * fx * (3 - 2*fx)
	uy := fy * fy * (3 - 2*fy)
	uz := fz * fz * (3 - 2*fz)

	c00 := lerpf(hash3(xi, yi, zi), hash3(xi+1, yi, zi), ux)
	c10 := lerpf(hash3(xi, yi+1, zi), hash3(xi+1, yi+1, zi), ux)
	c01 := lerpf(hash3(xi, yi, zi+1), hash3(xi+1, yi, zi+1), ux)
	c11 := lerpf(hash3(xi, yi+1, zi+1), hash3(xi+1, yi+1, zi+1), ux)

	return lerpf(lerpf(c00, c10, uy), lerpf(c01, c11, uy), uz)
}

// hsv turns a hue in turns (0..1) into a colour at full saturation. Only the
// hue moves in these effects, so value and saturation are not parameters.
func hsv(hue, value float64) canvas.Color {
	hue = hue - math.Floor(hue)
	s := hue * 6
	i := int(s)
	f := s - float64(i)
	p := 0.0
	q := 1 - f
	tt := f
	var r, g, b float64
	switch i {
	case 0:
		r, g, b = 1, tt, p
	case 1:
		r, g, b = q, 1, p
	case 2:
		r, g, b = p, 1, tt
	case 3:
		r, g, b = p, q, 1
	case 4:
		r, g, b = tt, p, 1
	default:
		r, g, b = 1, p, q
	}
	return canvas.RGB(chan8(r*value), chan8(g*value), chan8(b*value))
}

func chan8(v float64) uint8 {
	return uint8(min(max(v, 0), 1) * 255)
}
