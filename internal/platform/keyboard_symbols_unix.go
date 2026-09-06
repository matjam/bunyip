//go:build !windows

package platform

import (
	"fmt"
	"github.com/ebitengine/purego"
)

func bindKeyboardFunction(lib uintptr, name string, target any) error {
	p, err := purego.Dlsym(lib, name)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnsupported, name, err)
	}
	purego.RegisterFunc(target, p)
	return nil
}
