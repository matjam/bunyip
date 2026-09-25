package render

import (
	"fmt"

	"github.com/matjam/bunyip/internal/vk"
)

// UploadToBuffer copies data into the start of an existing device-local
// buffer made with NewDeviceBuffer, through the device's upload batch, so
// it costs no wait. It is the outside-a-frame counterpart of recording
// RecordBufferUpload into a frame, for a buffer being reused rather than
// made fresh. The caller makes sure no submitted work still reads the
// buffer: the batch runs ahead of the next frame, not after the last.
func (d *Device) UploadToBuffer(dst *Buffer, data []byte) error {
	size := vk.VkDeviceSize(len(data))
	if size == 0 {
		return errNoUpload
	}
	if size > dst.Size {
		return fmt.Errorf("render: %d bytes do not fit a buffer of %d", size, dst.Size)
	}
	staging, offset, mapped, err := d.StageUpload(size)
	if err != nil {
		return err
	}
	copy(mapped, data)
	cb, err := d.UploadCommands()
	if err != nil {
		return err
	}
	d.up.region = vk.VkBufferCopy{SrcOffset: offset, Size: size}
	vk.VkCmdCopyBuffer(cb, staging.Handle, dst.Handle, 1, &d.up.region)
	dst.NoteUpload()
	return nil
}
