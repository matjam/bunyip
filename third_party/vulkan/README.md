# Vulkan registry

`vk.xml` is the Khronos Vulkan API registry, copied unmodified from the
Vulkan SDK / `vulkan-headers` package (header version 350). It is the single
input to `cmd/vkgen`, which generates `internal/vk`.

The registry is licensed by The Khronos Group under Apache-2.0 OR MIT; the
licence text is in the file's own header comment.

To update: replace `vk.xml`, bump the header version in this file, run
`go generate ./internal/vk`, and review the diff of the generated code.
