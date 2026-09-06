package render

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/matjam/bunyip/internal/vk"
)

// The generated Vulkan bindings are process-wide. Keep one compatible
// instance family alive, with a separate reference wrapper for each owner.
var instanceFamily struct {
	sync.Mutex
	owner *instanceOwner
}

type instanceOwner struct {
	handle     vk.VkInstance
	messenger  vk.VkDebugUtilsMessengerEXT
	debugUtils bool
	refs       int
	cfg        Config
	extensions []string
}

// NewInstance creates an instance or retains the compatible active instance.
// Validation and logger must match an existing family; requested surface
// extensions must already be enabled. The initial application name is kept.
// Each returned reference must be destroyed once; repeated Destroy is safe.
func NewInstance(cfg Config, surfaceExts []string) (*Instance, error) {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	instanceFamily.Lock()
	defer instanceFamily.Unlock()
	if owner := instanceFamily.owner; owner != nil {
		if owner.cfg.Validation != cfg.Validation || owner.cfg.Log != cfg.Log {
			return nil, errors.New("render: concurrent instance requires the active validation and logger configuration")
		}
		if err := owner.checkExtensions(surfaceExts); err != nil {
			return nil, err
		}
		return owner.retain(), nil
	}
	i, err := createInstance(cfg, surfaceExts)
	if err != nil {
		vk.ClearInstance()
		return nil, err
	}
	owner := &instanceOwner{handle: i.Handle, messenger: i.messenger, debugUtils: i.debugUtils, refs: 1, cfg: cfg, extensions: slices.Clone(i.extensions)}
	i.owner = owner
	instanceFamily.owner = owner
	return i, nil
}

func (o *instanceOwner) checkExtensions(required []string) error {
	for _, ext := range required {
		if !slices.Contains(o.extensions, ext) {
			return fmt.Errorf("render: active instance does not enable required surface extension %s", ext)
		}
	}
	return nil
}
func (o *instanceOwner) retain() *Instance {
	o.refs++
	return &Instance{Handle: o.handle, log: o.cfg.Log, debugUtils: o.debugUtils, owner: o}
}
func (i *Instance) retain(required []string) (*Instance, error) {
	instanceFamily.Lock()
	defer instanceFamily.Unlock()
	if i == nil || i.Handle == 0 || i.owner == nil || instanceFamily.owner != i.owner {
		return nil, errors.New("render: instance is closed")
	}
	if err := i.owner.checkExtensions(required); err != nil {
		return nil, err
	}
	return i.owner.retain(), nil
}

// Destroy releases this reference, destroying the native instance only after
// the last output has released its reference. Its devices must already be gone.
func (i *Instance) Destroy() {
	if i == nil {
		return
	}
	instanceFamily.Lock()
	defer instanceFamily.Unlock()
	if i.Handle == 0 {
		return
	}
	owner := i.owner
	if owner == nil {
		i.destroyInstance()
		return
	}
	i.Handle = 0
	i.owner = nil
	owner.refs--
	if owner.refs != 0 {
		return
	}
	raw := Instance{Handle: owner.handle, messenger: owner.messenger}
	raw.destroyInstance()
	instanceFamily.owner = nil
	vk.ClearInstance()
}
