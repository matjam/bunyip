// Package vk is a generated, cgo-free binding to the Vulkan API.
//
// Types, constants and commands keep their C names so that the Vulkan
// specification can be read against this package directly. Commands are
// package-level function variables that are nil until the matching load step
// has run: Load binds the global commands, LoadInstance the instance-level
// ones, and LoadDevice the device-level ones. The driver is opened at runtime
// with purego, so the package builds with CGO_ENABLED=0 on every platform.
//
// Every generated struct carries a layout test; run the package tests after
// regenerating.
package vk

//go:generate go run ../../cmd/vkgen -registry ../../third_party/vulkan/vk.xml -out .
