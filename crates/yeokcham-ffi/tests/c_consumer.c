#include "yeokcham.h"

int yeokcham_c_consumer_conformance(void) {
    uint8_t secret[] = {0xa5, 0x5a, 0x11};
    yeokcham_client_t *client;

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
    if (yeokcham_client_complete_async(client, NULL, NULL) != YEOKCHAM_STATUS_INVALID_INPUT) {
        return 5;
    }
    if (yeokcham_secret_buffer_zeroize(secret, sizeof(secret)) != YEOKCHAM_STATUS_OK) {
        return 6;
    }
    if (secret[0] != 0 || secret[1] != 0 || secret[2] != 0) {
        return 7;
    }
    if (yeokcham_client_release(client) != YEOKCHAM_STATUS_OK) {
        return 8;
    }
    if (yeokcham_client_release(client) != YEOKCHAM_STATUS_INVALID_INPUT) {
        return 9;
    }
    return 0;
}
