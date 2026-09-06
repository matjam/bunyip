package shaders

import (
	"encoding/binary"
	"fmt"

	"github.com/gogpu/naga"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/spirv"
)

func compileModule(module *ir.Module) ([]byte, error) {
	data, err := naga.GenerateSPIRV(module, spirv.Options{Version: spirv.Version1_3, Validation: true})
	if err != nil {
		return nil, fmt.Errorf("generate SPIR-V: %w", err)
	}
	return zeroPrivateGlobals(data)
}

// zeroPrivateGlobals supplies WGSL's default initialization, which Naga 0.19
// omits from private OpVariable declarations. A null constant initializes the
// entire scalar or composite value before each invocation. Explicit WGSL
// initializers are rejected before lowering; an initializer already emitted by
// the backend is preserved so this repair is safe across compiler upgrades.
func zeroPrivateGlobals(data []byte) ([]byte, error) {
	if err := validateCompiledSPIRV(data); err != nil {
		return nil, err
	}
	words := make([]uint32, len(data)/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(data[4*i:])
	}
	pointees := map[uint32]uint32{}
	nulls := map[uint32]uint32{}
	bound := words[3]
	out := append([]uint32{}, words[:5]...)
	for i := 5; i < len(words); {
		n, op := int(words[i]>>16), words[i]&0xffff
		inst := words[i : i+n]
		switch op {
		case 32: // OpTypePointer: result ID, storage class, pointee type.
			if n != 4 {
				return nil, fmt.Errorf("SPIR-V OpTypePointer has %d words", n)
			}
			pointees[inst[1]] = inst[3]
		case 46: // OpConstantNull: result type, result ID.
			if n != 3 {
				return nil, fmt.Errorf("SPIR-V OpConstantNull has %d words", n)
			}
			nulls[inst[1]] = inst[2]
		case 59: // OpVariable: result type, result ID, storage, optional initializer.
			if n != 4 && n != 5 {
				return nil, fmt.Errorf("SPIR-V OpVariable has %d words", n)
			}
			if n == 4 && inst[3] == 6 { // Private, without initializer.
				typ := pointees[inst[1]]
				if typ == 0 {
					return nil, fmt.Errorf("SPIR-V private variable has unknown pointer type %d", inst[1])
				}
				id := nulls[typ]
				if id == 0 {
					if bound == ^uint32(0) {
						return nil, fmt.Errorf("SPIR-V ID bound overflow")
					}
					id, bound = bound, bound+1
					nulls[typ] = id
					out = append(out, 3<<16|46, typ, id)
				}
				out = append(out, 5<<16|59, inst[1], inst[2], inst[3], id)
				i += n
				continue
			}
		}
		out = append(out, inst...)
		i += n
	}
	out[3] = bound
	result := make([]byte, 4*len(out))
	for i, word := range out {
		binary.LittleEndian.PutUint32(result[4*i:], word)
	}
	return result, nil
}
