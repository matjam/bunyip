package main

import (
	"fmt"
	"strings"
)

// commandLevel says which proc-address function resolves a command: global
// (no instance yet), instance, or device.
func (m *model) commandLevel(c *cmd) string {
	if c.Name == "vkGetDeviceProcAddr" {
		return "instance"
	}
	if len(c.Params) == 0 {
		return "global"
	}
	switch c.Params[0].Type {
	case "VkInstance", "VkPhysicalDevice":
		return "instance"
	case "VkDevice", "VkQueue", "VkCommandBuffer":
		return "device"
	}
	return "global"
}

func (m *model) resolveCmd(name string) *cmd {
	c := m.Cmds[name]
	for c.Alias != "" {
		c = m.Cmds[c.Alias]
	}
	return c
}

func (m *model) signature(c *cmd) (string, error) {
	var params []string
	for _, p := range c.Params {
		gt, err := m.goType(p)
		if err != nil {
			return "", fmt.Errorf("%s: %w", p.Name, err)
		}
		if len(p.Arrays) > 0 && p.Ptr == 0 {
			gt = "*" + gt // C array parameters decay to pointers
		}
		params = append(params, goParamName(p.Name)+" "+gt)
	}
	ret := ""
	if c.Ret != "void" {
		gt, err := m.goType(decl{Type: c.Ret})
		if err != nil {
			return "", fmt.Errorf("return: %w", err)
		}
		ret = " " + gt
	}
	return "func(" + strings.Join(params, ", ") + ")" + ret, nil
}

func goCmdName(name string) string {
	return "Vk" + strings.TrimPrefix(name, "vk")
}

func (m *model) emitCommands(sel *selection, pkg string) (*file, error) {
	f := newFile("zz_commands.go", pkg, m.HeaderVersion, "unsafe")
	f.p("// Vulkan commands as function variables. Each is nil until the matching\n")
	f.p("// load step has run and the driver reports the entry point.\n\n")
	f.p("var _ unsafe.Pointer\n\n")
	f.p("var (\n")
	for _, name := range m.sortedCmds(sel) {
		if name == "vkGetInstanceProcAddr" {
			continue
		}
		sig, err := m.signature(m.resolveCmd(name))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		f.p("\t%s %s\n", goCmdName(name), sig)
	}
	f.p(")\n")
	return f, nil
}

func (m *model) emitLoader(sel *selection, pkg string) *file {
	f := newFile("zz_loader.go", pkg, m.HeaderVersion)
	f.p("// Binding of command variables to driver entry points, by level.\n\n")
	levels := map[string][]string{}
	for _, name := range m.sortedCmds(sel) {
		if name == "vkGetInstanceProcAddr" {
			continue
		}
		lvl := m.commandLevel(m.resolveCmd(name))
		levels[lvl] = append(levels[lvl], name)
	}
	for _, lvl := range []string{"global", "instance", "device"} {
		f.p("func bind%sCommands(resolve func(string) uintptr) {\n", strings.ToUpper(lvl[:1])+lvl[1:])
		for _, name := range levels[lvl] {
			f.p("\tbind(&%s, resolve(%q))\n", goCmdName(name), name)
		}
		f.p("}\n\n")
	}
	return f
}
