package gfx

import "fmt"

// Ownership is checked before recording handles or modifying another output's
// resource state. Destruction after a valid draw is separate: its already queued
// resources remain usable until the owning frame retires them.
func (g *Graphics) textureOwnerError(t *Texture) error {
	if t != nil && t.g != g {
		return fmt.Errorf("gfx: texture belongs to another Graphics")
	}
	return nil
}

func (g *Graphics) requireTextureOwner(t *Texture) {
	if err := g.textureOwnerError(t); err != nil {
		panic(err)
	}
}

func (g *Graphics) shaderOwnerError(s *Shader) error {
	if s == nil {
		return nil
	}
	if s.g != g {
		return fmt.Errorf("gfx: shader belongs to another Graphics")
	}
	for _, t := range s.images {
		if err := g.textureOwnerError(t); err != nil {
			return err
		}
	}
	return nil
}

func (g *Graphics) requireShaderOwner(s *Shader) {
	if err := g.shaderOwnerError(s); err != nil {
		panic(err)
	}
}

func (g *Graphics) materialOwnerError(m Material) error {
	if err := g.shaderOwnerError(m.Shader); err != nil {
		return err
	}
	for _, t := range [...]*Texture{m.Texture, m.MetalRoughTexture, m.NormalTexture, m.EmissiveTexture, m.OcclusionTexture, m.ThicknessTexture, m.TransmissionTexture, m.SpecularTexture, m.IridescenceTexture, m.AnisotropyTexture, m.FurTexture} {
		if err := g.textureOwnerError(t); err != nil {
			return err
		}
	}
	return nil
}

func (g *Graphics) meshOwnerError(m *Mesh, mat Material) error {
	if m != nil && m.g != g {
		return fmt.Errorf("gfx: mesh belongs to another Graphics")
	}
	return g.materialOwnerError(mat)
}

func (g *Graphics) requireMeshOwner(m *Mesh, mat Material) {
	if err := g.meshOwnerError(m, mat); err != nil {
		panic(err)
	}
}

func (g *Graphics) modelOwnerError(m *Model, materials bool) error {
	if m == nil {
		return nil
	}
	// A caller may assemble a CPU-only Model wrapper from local ModelParts.
	// Loaded models also own morph storage, which must remain on its device.
	if m.g != nil && m.g != g {
		return fmt.Errorf("gfx: model belongs to another Graphics")
	}
	for _, p := range m.Parts {
		mat := Material{}
		if materials {
			mat = p.Material
		}
		if err := g.meshOwnerError(p.Mesh, mat); err != nil {
			return err
		}
	}
	return nil
}

func (g *Graphics) requireModelOwner(m *Model, materials bool) {
	if err := g.modelOwnerError(m, materials); err != nil {
		panic(err)
	}
}

func (g *Graphics) requireEnvironmentOwner(env *Environment) {
	if env != nil && env.g != g {
		panic("gfx: environment belongs to another Graphics")
	}
}

func (g *Graphics) checkDrawFont(f *Font) bool {
	if f == nil || f.destroyed || f.g != g {
		g.recordDrawError(fmt.Errorf("gfx: drawing text requires live fonts owned by this Graphics"))
		return false
	}
	return true
}
