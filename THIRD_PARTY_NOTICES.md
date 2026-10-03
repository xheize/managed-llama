# Third-party components

Managed Llama source is covered by LICENSE. Dependencies retain their original
licenses and copyrights. `go.mod` and `go.sum` identify the Go dependencies.

Release builds collect the license and notice files of the Go module dependencies
and the Go toolchain into `licenses/` using `scripts/collect-licenses.ps1`.
The installer includes this directory and the project license.

llama.cpp is supplied separately for source builds. Installer builds include the
explicitly supplied llama.cpp license and any `licenses/` directory in the runtime
distribution. The packager must supply notices for all bundled runtime DLLs,
including any VC++ or CUDA redistributables. Do not assume the llama.cpp license
covers other runtime components.

GGUF models and NVIDIA drivers are not included. Their respective licenses apply.
