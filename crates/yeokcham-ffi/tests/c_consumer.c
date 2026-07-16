#include "yeokcham.h"

int yeokcham_c_consumer_conformance(void) {
    uint8_t secret[] = {0xa5, 0x5a, 0x11};
    yeokcham_handle_t *handle;

    if (yeokcham_abi_negotiate(YEOKCHAM_ABI_VERSION) != YEOKCHAM_ABI_VERSION) {
        return 1;
    }
    if (yeokcham_abi_negotiate(YEOKCHAM_ABI_NEGOTIATION_REJECTED) != YEOKCHAM_ABI_NEGOTIATION_REJECTED) {
        return 2;
    }
    if (yeokcham_abi_negotiate(YEOKCHAM_ABI_VERSION + UINT32_C(1)) != YEOKCHAM_ABI_NEGOTIATION_REJECTED) {
        return 3;
    }
    handle = yeokcham_handle_create();
    if (handle == NULL) {
        return 4;
    }
    if (yeokcham_handle_complete_async(handle, NULL, NULL) != YEOKCHAM_STATUS_INVALID_INPUT) {
        return 5;
    }
    if (yeokcham_secret_buffer_zeroize(secret, sizeof(secret)) != YEOKCHAM_STATUS_OK) {
        return 6;
    }
    if (secret[0] != 0 || secret[1] != 0 || secret[2] != 0) {
        return 7;
    }
    if (yeokcham_handle_release(handle) != YEOKCHAM_STATUS_OK) {
        return 8;
    }
    if (yeokcham_handle_release(handle) != YEOKCHAM_STATUS_INVALID_INPUT) {
        return 9;
    }
    return 0;
}
