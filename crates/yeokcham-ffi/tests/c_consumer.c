#include "yeokcham.h"

int yeokcham_c_consumer_conformance(void) {
    uint8_t secret[] = {0xa5, 0x5a, 0x11};
    const uint8_t state_directory[] = "/tmp/yeokcham-ffi-c-consumer";
    yeokcham_client_t *client;
    yeokcham_client_config_builder_t *builder;

    if (yeokcham_abi_negotiate(YEOKCHAM_ABI_VERSION) != YEOKCHAM_ABI_VERSION) {
        return 1;
    }
    if (yeokcham_abi_negotiate(YEOKCHAM_ABI_NEGOTIATION_REJECTED) != YEOKCHAM_ABI_NEGOTIATION_REJECTED) {
        return 2;
    }
    if (yeokcham_abi_negotiate(YEOKCHAM_ABI_VERSION + UINT32_C(1)) != YEOKCHAM_ABI_NEGOTIATION_REJECTED) {
        return 3;
    }
    client = yeokcham_client_create();
    if (client == NULL) {
        return 4;
    }
    builder = yeokcham_client_config_builder_create();
    if (builder == NULL) {
        return 5;
    }
    if (yeokcham_client_config_builder_set_state_directory(builder, state_directory, sizeof(state_directory) - UINT32_C(1)) != YEOKCHAM_STATUS_OK) {
        return 6;
    }
    if (yeokcham_client_config_builder_set_event_buffer_capacity(builder, UINT32_C(8)) != YEOKCHAM_STATUS_OK) {
        return 7;
    }
    if (yeokcham_client_config_builder_build(builder, client) != YEOKCHAM_STATUS_OK) {
        return 8;
    }
    if (yeokcham_client_config_builder_release(builder) != YEOKCHAM_STATUS_OK) {
        return 9;
    }
    if (yeokcham_client_complete_async(client, NULL, NULL) != YEOKCHAM_STATUS_INVALID_INPUT) {
        return 10;
    }
    if (yeokcham_secret_buffer_zeroize(secret, sizeof(secret)) != YEOKCHAM_STATUS_OK) {
        return 11;
    }
    if (secret[0] != 0 || secret[1] != 0 || secret[2] != 0) {
        return 12;
    }
    if (yeokcham_client_release(client) != YEOKCHAM_STATUS_OK) {
        return 13;
    }
    if (yeokcham_client_release(client) != YEOKCHAM_STATUS_INVALID_INPUT) {
        return 14;
    }
    return 0;
}

int yeokcham_c_embedded_client_lifecycle(
    const uint8_t *state_directory,
    size_t state_directory_length
) {
    yeokcham_client_t *client;
    yeokcham_client_config_builder_t *builder;

    if (state_directory == NULL || state_directory_length == 0) {
        return 1;
    }
    client = yeokcham_client_create();
    if (client == NULL) {
        return 2;
    }
    builder = yeokcham_client_config_builder_create();
    if (builder == NULL) {
        (void)yeokcham_client_release(client);
        return 3;
    }
    if (yeokcham_client_config_builder_set_state_directory(builder, state_directory, state_directory_length) != YEOKCHAM_STATUS_OK) {
        (void)yeokcham_client_config_builder_release(builder);
        (void)yeokcham_client_release(client);
        return 4;
    }
    if (yeokcham_client_config_builder_set_event_buffer_capacity(builder, UINT32_C(8)) != YEOKCHAM_STATUS_OK) {
        (void)yeokcham_client_config_builder_release(builder);
        (void)yeokcham_client_release(client);
        return 5;
    }
    if (yeokcham_client_config_builder_build(builder, client) != YEOKCHAM_STATUS_OK) {
        (void)yeokcham_client_config_builder_release(builder);
        (void)yeokcham_client_release(client);
        return 6;
    }
    if (yeokcham_client_config_builder_release(builder) != YEOKCHAM_STATUS_OK) {
        (void)yeokcham_client_release(client);
        return 7;
    }
    if (yeokcham_client_start(client) != YEOKCHAM_STATUS_OK) {
        (void)yeokcham_client_release(client);
        return 8;
    }
    if (yeokcham_client_start(client) != YEOKCHAM_STATUS_STATE) {
        (void)yeokcham_client_stop(client);
        (void)yeokcham_client_release(client);
        return 9;
    }
    if (yeokcham_client_stop(client) != YEOKCHAM_STATUS_OK) {
        (void)yeokcham_client_release(client);
        return 10;
    }
    if (yeokcham_client_stop(client) != YEOKCHAM_STATUS_STATE) {
        (void)yeokcham_client_release(client);
        return 11;
    }
    if (yeokcham_client_release(client) != YEOKCHAM_STATUS_OK) {
        return 12;
    }
    return 0;
}
