package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/maphash"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// A device builds every pipeline through one VkPipelineCache, and shares
// one VkShaderModule per distinct SPIR-V program between the pipelines
// built from it. The cache can be loaded from a file when the device is
// set up and written back when the device is destroyed, and from time to
// time while it runs, so the next start finds the driver's compiled
// programs instead of building them again.
//
// The cache is internally synchronised (it is created without
// VK_PIPELINE_CACHE_CREATE_EXTERNALLY_SYNCHRONIZED_BIT), so pipelines can
// be built on several goroutines at once through it; the module table has
// a mutex of its own.

// PipelineCacheOptions says where a device keeps its pipeline cache.
type PipelineCacheOptions struct {
	// Dir is the directory the cache file lives in. Empty keeps the cache
	// in memory for the life of the device.
	Dir string
	// Key tells apart caches built from different sets of programs, such
	// as the engine's shaders at one version and another. It is part of
	// the file's name.
	Key []byte
}

// pipelineCacheMagic starts a cache file; the rest of the header is a
// format version, the payload's length and its SHA-256.
const (
	pipelineCacheMagic   = "BYPC"
	pipelineCacheVersion = 1
	pipelineCacheHeader  = 4 + 4 + 8 + sha256.Size
	// pipelineCacheSaveEvery is the shortest time between two saves while
	// the device runs.
	pipelineCacheSaveEvery = 5 * time.Second
)

// pipelineState is a device's pipeline cache and shared shader modules.
type pipelineState struct {
	mu      sync.Mutex // guards everything below
	cache   vk.VkPipelineCache
	path    string // the file the cache is saved to, empty to keep it in memory
	seed    maphash.Seed
	modules map[uint64][]*shaderModule
	built   int       // pipelines built through the cache since the last save
	saved   time.Time // when the cache was last saved or loaded
	saving  sync.WaitGroup
	loaded  int // bytes of cache data loaded from the file

	workers chan struct{} // one token per pipeline being built on a worker
}

// shaderModule is one SPIR-V program's module, shared by the pipelines
// built from it and destroyed with the last of them.
type shaderModule struct {
	key    uint64
	spirv  []byte // a copy of the program, to tell apart two programs with one hash
	handle vk.VkShaderModule
	refs   int
}

// OpenPipelineCache creates the device's pipeline cache, filled from the
// file under opts.Dir when there is one for this device, driver and key.
// Call it before the first pipeline is built; a device that never calls
// it keeps a cache in memory, and a call once the cache exists does
// nothing. A file that is missing, unreadable, damaged or written for
// another device is ignored, and the cache starts empty.
func (d *Device) OpenPipelineCache(opts PipelineCacheOptions) error {
	ps := &d.pipes
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.cache != 0 {
		return nil
	}
	var data []byte
	if opts.Dir != "" {
		ps.path = filepath.Join(opts.Dir, d.pipelineCacheName(opts.Key))
		data = d.readPipelineCache(ps.path)
	}
	if err := d.createPipelineCache(data); err != nil && data != nil {
		// The driver refused the data; start from nothing.
		d.log.Warn("render: pipeline cache file ignored", "path", ps.path, "err", err)
		if err := d.createPipelineCache(nil); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	ps.loaded = len(data)
	ps.saved = time.Now()
	return nil
}

// createPipelineCache creates the cache with initial data. The caller
// holds ps.mu.
func (d *Device) createPipelineCache(data []byte) error {
	info := vk.VkPipelineCacheCreateInfo{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_CACHE_CREATE_INFO}
	if len(data) > 0 {
		info.InitialDataSize = uintptr(len(data))
		info.PInitialData = unsafe.Pointer(&data[0])
	}
	return vk.Check("vkCreatePipelineCache", vk.VkCreatePipelineCache(d.Handle, &info, nil, &d.pipes.cache))
}

// pipelineCacheName names the cache file after everything its contents
// depend on: the device, the driver and its version, the driver's own
// cache identifier, and the caller's key.
func (d *Device) pipelineCacheName(key []byte) string {
	p := &d.gpu.props
	h := sha256.New()
	var b [16]byte
	binary.LittleEndian.PutUint32(b[0:], p.VendorID)
	binary.LittleEndian.PutUint32(b[4:], p.DeviceID)
	binary.LittleEndian.PutUint32(b[8:], p.DriverVersion)
	binary.LittleEndian.PutUint32(b[12:], uint32(d.gpu.driverID))
	h.Write(b[:])
	h.Write(p.PipelineCacheUUID[:])
	device := hex.EncodeToString(h.Sum(nil)[:8])
	h.Write(key)
	return "pipelines-" + device + "-" + hex.EncodeToString(h.Sum(nil)[:8]) + ".vkcache"
}

// readPipelineCache reads and checks a cache file, returning nil when
// there is nothing usable in it.
func (d *Device) readPipelineCache(path string) []byte {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			d.log.Warn("render: pipeline cache unreadable", "path", path, "err", err)
		}
		return nil
	}
	data, err := unwrapPipelineCache(raw)
	if err == nil {
		err = d.checkPipelineCacheHeader(data)
	}
	if err != nil {
		d.log.Warn("render: pipeline cache file ignored", "path", path, "err", err)
		return nil
	}
	return data
}

// unwrapPipelineCache checks the file's own header and checksum and
// returns the driver's data inside it.
func unwrapPipelineCache(raw []byte) ([]byte, error) {
	if len(raw) < pipelineCacheHeader || string(raw[:4]) != pipelineCacheMagic {
		return nil, errors.New("not a pipeline cache file")
	}
	if v := binary.LittleEndian.Uint32(raw[4:]); v != pipelineCacheVersion {
		return nil, fmt.Errorf("pipeline cache format %d, want %d", v, pipelineCacheVersion)
	}
	n := binary.LittleEndian.Uint64(raw[8:])
	data := raw[pipelineCacheHeader:]
	if uint64(len(data)) != n {
		return nil, fmt.Errorf("pipeline cache holds %d bytes, its header says %d", len(data), n)
	}
	if sum := sha256.Sum256(data); !bytes.Equal(sum[:], raw[16:pipelineCacheHeader]) {
		return nil, errors.New("pipeline cache checksum mismatch")
	}
	return data, nil
}

// wrapPipelineCache puts the file header in front of the driver's data.
func wrapPipelineCache(data []byte) []byte {
	out := make([]byte, pipelineCacheHeader+len(data))
	copy(out, pipelineCacheMagic)
	binary.LittleEndian.PutUint32(out[4:], pipelineCacheVersion)
	binary.LittleEndian.PutUint64(out[8:], uint64(len(data)))
	sum := sha256.Sum256(data)
	copy(out[16:], sum[:])
	copy(out[pipelineCacheHeader:], data)
	return out
}

// checkPipelineCacheHeader compares the driver's own header at the front
// of the data (VkPipelineCacheHeaderVersionOne) with this device. The
// driver checks it too; checking here keeps data written for another
// device away from it entirely.
func (d *Device) checkPipelineCacheHeader(data []byte) error {
	if len(data) < 32 {
		return errors.New("pipeline cache data too short")
	}
	p := &d.gpu.props
	size := binary.LittleEndian.Uint32(data[0:])
	version := binary.LittleEndian.Uint32(data[4:])
	switch {
	case size < 32 || int(size) > len(data):
		return fmt.Errorf("pipeline cache header of %d bytes", size)
	case version != uint32(vk.VK_PIPELINE_CACHE_HEADER_VERSION_ONE):
		return fmt.Errorf("pipeline cache header version %d", version)
	case binary.LittleEndian.Uint32(data[8:]) != p.VendorID || binary.LittleEndian.Uint32(data[12:]) != p.DeviceID:
		return errors.New("pipeline cache written for another device")
	case !bytes.Equal(data[16:32], p.PipelineCacheUUID[:]):
		return errors.New("pipeline cache written by another driver version")
	}
	return nil
}

// pipelineCache returns the cache pipelines are built through, creating
// an in-memory one when OpenPipelineCache was never called.
func (d *Device) pipelineCache() (vk.VkPipelineCache, error) {
	ps := &d.pipes
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.cache == 0 {
		if err := d.createPipelineCache(nil); err != nil {
			return 0, err
		}
		ps.saved = time.Now()
	}
	return ps.cache, nil
}

// builtPipeline counts a pipeline built through the cache, which may have
// grown it.
func (d *Device) builtPipeline() {
	d.pipes.mu.Lock()
	d.pipes.built++
	d.pipes.mu.Unlock()
}

// SavePipelineCacheSoon writes the pipeline cache to its file on a
// goroutine of its own when pipelines have been built since the last
// save and a few seconds have passed. Call it at a quiet point once a
// frame; when there is nothing to save it only checks a counter.
func (d *Device) SavePipelineCacheSoon() {
	ps := &d.pipes
	ps.mu.Lock()
	due := ps.path != "" && ps.built > 0 && time.Since(ps.saved) >= pipelineCacheSaveEvery
	ps.mu.Unlock()
	if !due {
		return
	}
	data, path := d.snapshotPipelineCache()
	if data == nil {
		return
	}
	ps.saving.Add(1)
	go func() {
		defer ps.saving.Done()
		if err := writeFileAtomic(path, wrapPipelineCache(data)); err != nil {
			d.log.Warn("render: pipeline cache not saved", "path", path, "err", err)
		}
	}()
}

// SavePipelineCache writes the pipeline cache to its file now if
// pipelines have been built since it was last saved. It does nothing for
// a cache kept in memory.
func (d *Device) SavePipelineCache() error {
	ps := &d.pipes
	ps.saving.Wait()
	ps.mu.Lock()
	due := ps.path != "" && ps.built > 0 && ps.cache != 0
	ps.mu.Unlock()
	if !due {
		return nil
	}
	data, path := d.snapshotPipelineCache()
	if data == nil {
		return nil
	}
	return writeFileAtomic(path, wrapPipelineCache(data))
}

// snapshotPipelineCache copies the cache's data out of the driver and
// marks it saved.
func (d *Device) snapshotPipelineCache() ([]byte, string) {
	ps := &d.pipes
	ps.mu.Lock()
	defer ps.mu.Unlock()
	var size uintptr
	if vk.VkGetPipelineCacheData(d.Handle, ps.cache, &size, nil) != vk.VK_SUCCESS || size == 0 {
		return nil, ""
	}
	data := make([]byte, size)
	res := vk.VkGetPipelineCacheData(d.Handle, ps.cache, &size, unsafe.Pointer(&data[0]))
	if res != vk.VK_SUCCESS && res != vk.VK_INCOMPLETE {
		return nil, ""
	}
	ps.built, ps.saved = 0, time.Now()
	return data[:size], ps.path
}

// writeFileAtomic writes data to a temporary file beside path and renames
// it over path, so a reader never sees a half-written cache.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp)
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	removeStalePipelineCaches(path)
	return nil
}

// removeStalePipelineCaches deletes the cache files the same device wrote
// under another key, which a new engine version leaves behind.
func removeStalePipelineCaches(path string) {
	dir, name := filepath.Split(path)
	parts := strings.Split(name, "-")
	if len(parts) != 3 {
		return
	}
	prefix := parts[0] + "-" + parts[1] + "-"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		if n != name && strings.HasPrefix(n, prefix) && strings.HasSuffix(n, ".vkcache") {
			os.Remove(filepath.Join(dir, n))
		}
	}
}

// shaderModule returns the module for a SPIR-V program, creating it the
// first time the program is seen, and takes a reference to it.
func (d *Device) shaderModule(spirv []byte) (*shaderModule, error) {
	if len(spirv) == 0 || len(spirv)%4 != 0 {
		return nil, fmt.Errorf("render: SPIR-V of %d bytes is not a whole number of words", len(spirv))
	}
	ps := &d.pipes
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.modules == nil {
		ps.modules = map[uint64][]*shaderModule{}
		ps.seed = maphash.MakeSeed()
	}
	key := maphash.Bytes(ps.seed, spirv)
	for _, m := range ps.modules[key] {
		if bytes.Equal(m.spirv, spirv) {
			m.refs++
			return m, nil
		}
	}
	m := &shaderModule{key: key, spirv: bytes.Clone(spirv), refs: 1}
	info := vk.VkShaderModuleCreateInfo{
		SType:    vk.VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
		CodeSize: uintptr(len(m.spirv)),
		PCode:    (*uint32)(unsafe.Pointer(&m.spirv[0])),
	}
	if err := vk.Check("vkCreateShaderModule", vk.VkCreateShaderModule(d.Handle, &info, nil, &m.handle)); err != nil {
		return nil, err
	}
	ps.modules[key] = append(ps.modules[key], m)
	return m, nil
}

// releaseModule drops a reference to a module and destroys it with the
// last one. A pipeline no longer needs its modules once it is built; the
// references only keep a module for the next variant built from it.
func (d *Device) releaseModule(m *shaderModule) {
	if m == nil {
		return
	}
	ps := &d.pipes
	ps.mu.Lock()
	defer ps.mu.Unlock()
	m.refs--
	if m.refs > 0 {
		return
	}
	list := ps.modules[m.key]
	for i, e := range list {
		if e == m {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(ps.modules, m.key)
	} else {
		ps.modules[m.key] = list
	}
	vk.VkDestroyShaderModule(d.Handle, m.handle, nil)
}

// destroyPipelineState saves the cache to its file, then frees it and any
// module a pipeline still holds. The device must be idle and no pipeline
// may be building.
func (d *Device) destroyPipelineState() {
	ps := &d.pipes
	if err := d.SavePipelineCache(); err != nil {
		d.log.Warn("render: pipeline cache not saved", "path", ps.path, "err", err)
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for _, list := range ps.modules {
		for _, m := range list {
			vk.VkDestroyShaderModule(d.Handle, m.handle, nil)
		}
	}
	ps.modules = nil
	if ps.cache != 0 {
		vk.VkDestroyPipelineCache(d.Handle, ps.cache, nil)
		ps.cache = 0
	}
}
