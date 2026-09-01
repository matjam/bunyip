package render

import "github.com/matjam/bunyip/internal/vk"

// cstrings keeps NUL-terminated copies of Go strings alive for the duration
// of a Vulkan call that reads const char* arrays.
type cstrings struct {
	ptrs []*byte
	keep [][]byte
}

func newCStrings(ss []string) *cstrings {
	c := &cstrings{}
	for _, s := range ss {
		p, b := vk.CString(s)
		c.ptrs = append(c.ptrs, p)
		c.keep = append(c.keep, b)
	}
	return c
}

func (c *cstrings) count() uint32 { return uint32(len(c.ptrs)) }

func (c *cstrings) first() **byte {
	if len(c.ptrs) == 0 {
		return nil
	}
	return &c.ptrs[0]
}
