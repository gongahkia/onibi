#ifndef KELP_LLAMA_BRIDGE_H
#define KELP_LLAMA_BRIDGE_H

#include <stdint.h>

struct kelp_llama_result {
    int32_t ok;
    int32_t loaded;
    int32_t decoded_tokens;
    double elapsed_seconds;
    char text[2048];
    char error[256];
};

int32_t kelp_llama_generate(
    const char * model_path,
    const char * prompt,
    int32_t n_predict,
    int32_t n_threads,
    struct kelp_llama_result * result);

#endif
