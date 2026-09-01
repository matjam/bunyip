package gfx

import "math"

func sin32(v float32) float32 { return float32(math.Sin(float64(v))) }
func cos32(v float32) float32 { return float32(math.Cos(float64(v))) }
