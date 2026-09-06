package shaders

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed prelude_sprite.wgsl
var spritePreludeWGSL string

const spriteEntryWGSL = `
@fragment fn main(@location(0) uv: vec2f, @location(1) color: vec4f, @location(2) pos: vec2f) -> @location(0) vec4f {
    spritePosition = pos;
    return fragment(uv, color);
}
`

func composeSprite(source string) (string, int, error) {
	if !hookPresent(source, "fragment") {
		return "", 0, fmt.Errorf("sprite source must define fn fragment(uv: vec2f, color: vec4f) -> vec4f")
	}
	prefix := spritePreludeWGSL + "\n"
	return prefix + source + "\n" + spriteEntryWGSL, strings.Count(prefix, "\n"), nil
}
