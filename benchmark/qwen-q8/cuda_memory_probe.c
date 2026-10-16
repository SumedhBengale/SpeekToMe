#include <dlfcn.h>
#include <stdio.h>
#include <stddef.h>

// Run only while the GPU is idle: compares runtime/context memory accounting.
int main(int argc, char **argv) {
    if (argc != 3) return 2;
    void *runtime = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
    if (!runtime) { fprintf(stderr, "%s\n", dlerror()); return 1; }
    int (*memory_info)(size_t *,size_t *) = dlsym(runtime,"cudaMemGetInfo");
    int (*runtime_version)(int *) = dlsym(runtime,"cudaRuntimeGetVersion");
    if (!memory_info || !runtime_version) return 1;
    size_t available = 0, total = 0;
    int version = 0; runtime_version(&version);
    int error = memory_info(&available,&total);
    printf("{\"event\":\"runtime_context\",\"version\":%d,\"error\":%d,\"free_mib\":%.2f,\"total_mib\":%.2f}\n",version,error,available/1048576.0,total/1048576.0);
    if (error) return 1;
    void *ggml = dlopen(argv[2],RTLD_NOW|RTLD_LOCAL);
    if (!ggml) { fprintf(stderr, "%s\n",dlerror()); return 1; }
    void *(*init)(int) = dlsym(ggml,"ggml_backend_cuda_init");
    if (!init || !init(0)) return 1;
    error = memory_info(&available,&total);
    printf("{\"event\":\"ggml_context\",\"version\":%d,\"error\":%d,\"free_mib\":%.2f,\"total_mib\":%.2f}\n",version,error,available/1048576.0,total/1048576.0);
    // This short-lived probe deliberately lets process teardown release CUDA.
    return error ? 1 : 0;
}
