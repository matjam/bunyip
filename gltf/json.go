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
	BufferView    *int   `json:"bufferView"`
	ByteOffset    int    `json:"byteOffset"`
	ComponentType int    `json:"componentType"`
	Normalized    bool   `json:"normalized"`
	Count         int    `json:"count"`
	Type          string `json:"type"`
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
	Index int `json:"index"`
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
	NormalTexture   *jsonTextureRef `json:"normalTexture"`
	EmissiveTexture *jsonTextureRef `json:"emissiveTexture"`
	EmissiveFactor  []float32       `json:"emissiveFactor"`
}

type jsonMesh struct {
	Name       string          `json:"name"`
	Primitives []jsonPrimitive `json:"primitives"`
}

type jsonPrimitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    *int           `json:"indices"`
	Material   *int           `json:"material"`
	Mode       *int           `json:"mode"`
}

type jsonNode struct {
	Name        string    `json:"name"`
	Mesh        *int      `json:"mesh"`
	Children    []int     `json:"children"`
	Matrix      []float32 `json:"matrix"`
	Translation []float32 `json:"translation"`
	Rotation    []float32 `json:"rotation"`
	Scale       []float32 `json:"scale"`
}

type jsonScene struct {
	Nodes []int `json:"nodes"`
}
