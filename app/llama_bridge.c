#include "kelp_llama_bridge.h"

#include "llama.h"

#include <stdbool.h>
#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>

static void set_error(struct kelp_llama_result * result, const char * message) {
    if (result == NULL) return;
    snprintf(result->error, sizeof(result->error), "%s", message);
}

static void append_text(char * out, size_t out_len, size_t * used, const char * piece, size_t piece_len) {
    if (out_len == 0 || *used >= out_len - 1) return;
    const size_t remaining = out_len - 1 - *used;
    const size_t copied = piece_len < remaining ? piece_len : remaining;
    memcpy(out + *used, piece, copied);
    *used += copied;
    out[*used] = '\0';
}

static void quiet_llama_log(enum ggml_log_level level, const char * text, void * user_data) {
    (void) level;
    (void) text;
    (void) user_data;
}

static int64_t peak_rss_bytes(void) {
    struct rusage usage;
    if (getrusage(RUSAGE_SELF, &usage) != 0) return 0;
#if defined(__APPLE__)
    return (int64_t) usage.ru_maxrss;
#else
    return (int64_t) usage.ru_maxrss * 1024;
#endif
}

int32_t kelp_llama_generate(
    const char * model_path,
    const char * prompt,
    int32_t n_predict,
    int32_t n_threads,
    struct kelp_llama_result * result) {
    if (result == NULL) return 1;
    memset(result, 0, sizeof(*result));
    if (model_path == NULL || model_path[0] == '\0') {
        set_error(result, "missing model path");
        return 1;
    }
    if (prompt == NULL || prompt[0] == '\0') {
        set_error(result, "missing prompt");
        return 1;
    }
    if (n_predict < 0) n_predict = 0;
    if (n_predict > 512) n_predict = 512;
    if (n_threads < 1) n_threads = 1;

    llama_backend_init();
    struct llama_model * model = NULL;
    struct llama_context * ctx = NULL;
    struct llama_sampler * sampler = NULL;
    llama_token * prompt_tokens = NULL;
    ggml_log_callback old_log_callback = NULL;
    void * old_log_user_data = NULL;
    int32_t rc = 1;
    const int64_t start_us = llama_time_us();

    llama_log_get(&old_log_callback, &old_log_user_data);
    llama_log_set(quiet_llama_log, NULL);
    struct llama_model_params model_params = llama_model_default_params();
    model_params.n_gpu_layers = 0;
    model = llama_model_load_from_file(model_path, model_params);
    if (model == NULL) {
        set_error(result, "llama_model_load_from_file failed");
        goto cleanup;
    }
    result->loaded = 1;

    const struct llama_vocab * vocab = llama_model_get_vocab(model);
    const int32_t prompt_len = (int32_t) strlen(prompt);
    const int32_t needed = -llama_tokenize(vocab, prompt, prompt_len, NULL, 0, true, true);
    if (needed <= 0) {
        set_error(result, "llama_tokenize sizing failed");
        goto cleanup;
    }
    prompt_tokens = (llama_token *) malloc((size_t) needed * sizeof(llama_token));
    if (prompt_tokens == NULL) {
        set_error(result, "token allocation failed");
        goto cleanup;
    }
    if (llama_tokenize(vocab, prompt, prompt_len, prompt_tokens, needed, true, true) < 0) {
        set_error(result, "llama_tokenize failed");
        goto cleanup;
    }

    struct llama_context_params ctx_params = llama_context_default_params();
    ctx_params.n_ctx = (uint32_t) needed + (uint32_t) n_predict + 8;
    ctx_params.n_batch = (uint32_t) needed;
    ctx_params.n_threads = n_threads;
    ctx_params.n_threads_batch = n_threads;
    ctx_params.no_perf = true;
    ctx = llama_init_from_model(model, ctx_params);
    if (ctx == NULL) {
        set_error(result, "llama_init_from_model failed");
        goto cleanup;
    }

    struct llama_sampler_chain_params sampler_params = llama_sampler_chain_default_params();
    sampler_params.no_perf = true;
    sampler = llama_sampler_chain_init(sampler_params);
    if (sampler == NULL) {
        set_error(result, "llama_sampler_chain_init failed");
        goto cleanup;
    }
    llama_sampler_chain_add(sampler, llama_sampler_init_greedy());

    struct llama_batch batch = llama_batch_get_one(prompt_tokens, needed);
    if (llama_model_has_encoder(model)) {
        if (llama_encode(ctx, batch) != 0) {
            set_error(result, "llama_encode failed");
            goto cleanup;
        }
        llama_token decoder_start_token_id = llama_model_decoder_start_token(model);
        if (decoder_start_token_id == LLAMA_TOKEN_NULL) decoder_start_token_id = llama_vocab_bos(vocab);
        batch = llama_batch_get_one(&decoder_start_token_id, 1);
    }

    size_t used = 0;
    int32_t decoded = 0;
    for (int32_t pos = 0; pos + batch.n_tokens < needed + n_predict;) {
        if (llama_decode(ctx, batch) != 0) {
            set_error(result, "llama_decode failed");
            goto cleanup;
        }
        pos += batch.n_tokens;
        llama_token token = llama_sampler_sample(sampler, ctx, -1);
        if (token == LLAMA_TOKEN_NULL || llama_vocab_is_eog(vocab, token)) break;
        char piece[256];
        const int32_t piece_len = llama_token_to_piece(vocab, token, piece, (int32_t) sizeof(piece), 0, true);
        if (piece_len < 0) {
            set_error(result, "llama_token_to_piece failed");
            goto cleanup;
        }
        append_text(result->text, sizeof(result->text), &used, piece, (size_t) piece_len);
        decoded += 1;
        batch = llama_batch_get_one(&token, 1);
    }

    result->ok = 1;
    result->decoded_tokens = decoded;
    result->elapsed_seconds = (double) (llama_time_us() - start_us) / 1000000.0;
    rc = 0;

cleanup:
    result->peak_rss_bytes = peak_rss_bytes();
    if (prompt_tokens != NULL) free(prompt_tokens);
    if (sampler != NULL) llama_sampler_free(sampler);
    if (ctx != NULL) llama_free(ctx);
    if (model != NULL) llama_model_free(model);
    llama_log_set(old_log_callback, old_log_user_data);
    llama_backend_free();
    if (rc != 0 && result->error[0] == '\0') set_error(result, "llama generation failed");
    return rc;
}
