#ifndef YEOKCHAM_V1_H
#define YEOKCHAM_V1_H

#include <stdint.h>
#include <stddef.h>

#define YEOKCHAM_ABI_VERSION_MAJOR UINT32_C(1)
#define YEOKCHAM_ABI_VERSION_MINOR UINT32_C(0)
#define YEOKCHAM_ABI_VERSION UINT32_C(1)
#define YEOKCHAM_ABI_NEGOTIATION_REJECTED UINT32_C(0)

typedef struct yeokcham_client yeokcham_client_t; // library-owned opaque client; release with yeokcham_client_release

typedef int32_t yeokcham_status_t;
typedef void (*yeokcham_completion_callback_t)(yeokcham_status_t status, void *context);

#define YEOKCHAM_STATUS_OK INT32_C(0)
#define YEOKCHAM_STATUS_INVALID_INPUT INT32_C(1)
#define YEOKCHAM_STATUS_UNSUPPORTED_VERSION INT32_C(2)
#define YEOKCHAM_STATUS_RESOURCE_LIMIT INT32_C(3)
#define YEOKCHAM_STATUS_STATE INT32_C(4)

yeokcham_client_t *yeokcham_client_create(void); // null when client capacity is exhausted
yeokcham_status_t yeokcham_client_release(yeokcham_client_t *client); // invalid input unless client is active
yeokcham_status_t yeokcham_client_complete_async(
    const yeokcham_client_t *client,
    yeokcham_completion_callback_t callback,
    void *context
); // callback receives the completion status
uint32_t yeokcham_abi_negotiate(uint32_t requested_version); // call first; returns requested token only on exact pre-release match, else YEOKCHAM_ABI_NEGOTIATION_REJECTED
yeokcham_status_t yeokcham_secret_buffer_zeroize(uint8_t *buffer, size_t length); // caller-owned writable bytes only

#endif
