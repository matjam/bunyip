package gltf

// The JSON model, limited to what the loader reads.

type jsonDoc struct {
	Buffers     []jsonBuffer     `json:"buffers"`
	BufferViews []jsonBufferView `json:"bufferViews"`
	Accessors   []jsonAccessor   `json:"accessors"`
	Images      []jsonImage      `json:"images"`
	Textures    []jsonTexture    `json:"textures"`
	Samplers    []jsonSampler    `json:"samplers"`
	Materials   []jsonMaterial   `json:"materials"`
	Meshes      []jsonMesh       `json:"meshes"`
	Nodes       []jsonNode       `json:"nodes"`
	Scenes      []jsonScene      `json:"scenes"`
	Scene       *int             `json:"scene"`
	Skins       []jsonSkin       `json:"skins"`
	Animations  []jsonAnimation  `json:"animations"`
}

type jsonSkin struct {
	Name                string `json:"name"`
	InverseBindMatrices *int   `json:"inverseBindMatrices"`
	Joints              []int  `json:"joints"`
	Skeleton            *int   `json:"skeleton"`
}

type jsonAnimation struct {
	Name     string `json:"name"`
	Channels []struct {
		Sampler int `json:"sampler"`
		Target  struct {
			Node *int   `json:"node"`
			Path string `json:"path"`
		} `json:"target"`
	} `json:"channels"`
	Samplers []struct {
		Input         int    `json:"input"`
		Output        int    `json:"output"`
		Interpolation string `json:"interpolation"`
	} `json:"samplers"`
}

type jsonBuffer struct {
	ByteLength int    `json:"byteLength"`
	URI        string `json:"uri"`
}

type jsonBufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	ByteStride int `json:"byteStride"`
}

type jsonAccessor struct {
	BufferView    *int        `json:"bufferView"`
	ByteOffset    int         `json:"byteOffset"`
	ComponentType int         `json:"componentType"`
	Normalized    bool        `json:"normalized"`
	Count         int         `json:"count"`
	Type          string      `json:"type"`
	Sparse        *jsonSparse `json:"sparse"`
}

// jsonSparse is the subset of an accessor's elements a file overrides,
// which is how Blender writes morph targets that touch few vertices.
type jsonSparse struct {
	Count   int `json:"count"`
	Indices struct {
		BufferView    int `json:"bufferView"`
		ByteOffset    int `json:"byteOffset"`
		ComponentType int `json:"componentType"`
	} `json:"indices"`
	Values struct {
		BufferView int `json:"bufferView"`
		ByteOffset int `json:"byteOffset"`
	} `json:"values"`
}

type jsonImage struct {
	URI        string `json:"uri"`
	MimeType   string `json:"mimeType"`
	BufferView *int   `json:"bufferView"`
}

type jsonTexture struct {
	Source  *int `json:"source"`
	Sampler *int `json:"sampler"`
}

type jsonSampler struct {
	MagFilter int `json:"magFilter"`
}

type jsonTextureRef struct {
	Index      int `json:"index"`
	TexCoord   int `json:"texCoord"`
	Extensions struct {
		Transform *struct {
			Offset   []float32 `json:"offset"`
			Rotation float32   `json:"rotation"`
			Scale    []float32 `json:"scale"`
		} `json:"KHR_texture_transform"`
	} `json:"extensions"`
}

type jsonMaterial struct {
	Name string `json:"name"`
	PBR  *struct {
		BaseColorFactor          []float32       `json:"baseColorFactor"`
		BaseColorTexture         *jsonTextureRef `json:"baseColorTexture"`
		MetallicFactor           *float32        `json:"metallicFactor"`
		RoughnessFactor          *float32        `json:"roughnessFactor"`
		MetallicRoughnessTexture *jsonTextureRef `json:"metallicRoughnessTexture"`
	} `json:"pbrMetallicRoughness"`
	NormalTexture    *jsonTextureRef `json:"normalTexture"`
	EmissiveTexture  *jsonTextureRef `json:"emissiveTexture"`
	EmissiveFactor   []float32       `json:"emissiveFactor"`
	OcclusionTexture *struct {
		Index    int      `json:"index"`
		TexCoord int      `json:"texCoord"`
		Strength *float32 `json:"strength"`
	} `json:"occlusionTexture"`
	AlphaMode   string   `json:"alphaMode"`
	AlphaCutoff *float32 `json:"alphaCutoff"`
	DoubleSided bool     `json:"doubleSided"`
	Extensions  struct {
		Unlit            *struct{} `json:"KHR_materials_unlit"`
		EmissiveStrength *struct {
			Strength float32 `json:"emissiveStrength"`
		} `json:"KHR_materials_emissive_strength"`
		Clearcoat *struct {
			Factor          *float32 `json:"clearcoatFactor"`
			RoughnessFactor *float32 `json:"clearcoatRoughnessFactor"`
		} `json:"KHR_materials_clearcoat"`
		Sheen *struct {
			ColorFactor     []float32 `json:"sheenColorFactor"`
			RoughnessFactor *float32  `json:"sheenRoughnessFactor"`
		} `json:"KHR_materials_sheen"`
		Transmission *struct {
			Factor  *float32        `json:"transmissionFactor"`
			Texture *jsonTextureRef `json:"transmissionTexture"`
		} `json:"KHR_materials_transmission"`
		IOR *struct {
			IOR *float32 `json:"ior"`
		} `json:"KHR_materials_ior"`
		Volume *struct {
			Thickness           *float32        `json:"thicknessFactor"`
			ThicknessTexture    *jsonTextureRef `json:"thicknessTexture"`
			AttenuationDistance *float32        `json:"attenuationDistance"`
			AttenuationColor    []float32       `json:"attenuationColor"`
		} `json:"KHR_materials_volume"`
		Specular *struct {
			Factor       *float32        `json:"specularFactor"`
			Texture      *jsonTextureRef `json:"specularTexture"`
			ColorFactor  []float32       `json:"specularColorFactor"`
			ColorTexture *jsonTextureRef `json:"specularColorTexture"`
		} `json:"KHR_materials_specular"`
		Iridescence *struct {
			Factor           *float32        `json:"iridescenceFactor"`
			Texture          *jsonTextureRef `json:"iridescenceTexture"`
			IOR              *float32        `json:"iridescenceIor"`
			ThicknessMinimum *float32        `json:"iridescenceThicknessMinimum"`
			ThicknessMaximum *float32        `json:"iridescenceThicknessMaximum"`
			ThicknessTexture *jsonTextureRef `json:"iridescenceThicknessTexture"`
		} `json:"KHR_materials_iridescence"`
		Anisotropy *struct {
			Strength *float32        `json:"anisotropyStrength"`
			Rotation *float32        `json:"anisotropyRotation"`
			Texture  *jsonTextureRef `json:"anisotropyTexture"`
		} `json:"KHR_materials_anisotropy"`
		SpecGloss *struct {
			DiffuseFactor    []float32       `json:"diffuseFactor"`
			DiffuseTexture   *jsonTextureRef `json:"diffuseTexture"`
			SpecularFactor   []float32       `json:"specularFactor"`
			GlossinessFactor *float32        `json:"glossinessFactor"`
			SpecGlossTexture *jsonTextureRef `json:"specularGlossinessTexture"`
		} `json:"KHR_materials_pbrSpecularGlossiness"`
	} `json:"extensions"`
}

type jsonMesh struct {
	Name       string          `json:"name"`
	Primitives []jsonPrimitive `json:"primitives"`
	Weights    []float32       `json:"weights"`
	Extras     struct {
		TargetNames []string `json:"targetNames"`
	} `json:"extras"`
}

type jsonPrimitive struct {
	Attributes map[string]int   `json:"attributes"`
	Indices    *int             `json:"indices"`
	Material   *int             `json:"material"`
	Mode       *int             `json:"mode"`
	Targets    []map[string]int `json:"targets"`
}

type jsonNode struct {
	Name        string    `json:"name"`
	Mesh        *int      `json:"mesh"`
	Skin        *int      `json:"skin"`
	Children    []int     `json:"children"`
	Matrix      []float32 `json:"matrix"`
	Translation []float32 `json:"translation"`
	Rotation    []float32 `json:"rotation"`
	Scale       []float32 `json:"scale"`
	Weights     []float32 `json:"weights"`
}

type jsonScene struct {
	Nodes []int `json:"nodes"`
}
