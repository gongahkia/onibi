#ifndef YEOKCHAM_V1_H
#define YEOKCHAM_V1_H

#include <stdint.h>
#include <stddef.h>

#define YEOKCHAM_ABI_VERSION_MAJOR UINT32_C(1)
#define YEOKCHAM_ABI_VERSION_MINOR UINT32_C(0)
#define YEOKCHAM_ABI_VERSION UINT32_C(1)
#define YEOKCHAM_ABI_NEGOTIATION_REJECTED UINT32_C(0)
#define YEOKCHAM_MAX_CALLBACK_WORKERS UINT32_C(4)
#define YEOKCHAM_MAX_BUFFERS UINT32_C(1024)
#define YEOKCHAM_MAX_ERROR_DETAIL_BYTES UINT32_C(64)
#define YEOKCHAM_MAX_EVENT_SUBSCRIPTIONS UINT32_C(1024)
#define YEOKCHAM_MAX_PENDING_COMPLETIONS UINT32_C(1024)
#define YEOKCHAM_EVENT_VERSION UINT32_C(1)
#define YEOKCHAM_EVENT_MESSAGE_IDENTIFIER_BYTES UINT32_C(16)
#define YEOKCHAM_EVENT_CLIENT_STARTED UINT32_C(1)
#define YEOKCHAM_EVENT_CLIENT_STOPPED UINT32_C(2)
#define YEOKCHAM_EVENT_MESSAGE_QUEUED UINT32_C(3)
#define YEOKCHAM_EVENT_MESSAGE_DELIVERED UINT32_C(4)
#define YEOKCHAM_EVENT_MESSAGE_DELIVERY_FAILED UINT32_C(5)
#define YEOKCHAM_IDENTITY_PUBLIC_KEY_BYTES UINT32_C(32)

typedef struct yeokcham_client yeokcham_client_t; // library-owned opaque client; release with yeokcham_client_release
typedef struct yeokcham_client_config_builder yeokcham_client_config_builder_t; // library-owned configuration builder; release with yeokcham_client_config_builder_release
typedef struct yeokcham_buffer yeokcham_buffer_t; // library-owned opaque bytes; release with yeokcham_buffer_release
typedef struct yeokcham_event_subscription yeokcham_event_subscription_t; // library-owned opaque stream; release with yeokcham_event_subscription_release
typedef struct yeokcham_event {
    uint32_t version;
    uint64_t sequence;
    uint32_t kind;
    uint8_t message_identifier[YEOKCHAM_EVENT_MESSAGE_IDENTIFIER_BYTES];
} yeokcham_event_t;

typedef int32_t yeokcham_status_t;
typedef void (*yeokcham_completion_callback_t)(yeokcham_status_t status, void *context);

#define YEOKCHAM_STATUS_OK INT32_C(0)
#define YEOKCHAM_STATUS_INVALID_INPUT INT32_C(1)
#define YEOKCHAM_STATUS_UNSUPPORTED_VERSION INT32_C(2)
#define YEOKCHAM_STATUS_RESOURCE_LIMIT INT32_C(3)
#define YEOKCHAM_STATUS_STATE INT32_C(4)

yeokcham_client_t *yeokcham_client_create(void); // null when client capacity is exhausted
yeokcham_status_t yeokcham_client_release(yeokcham_client_t *client); // invalid input unless client is active
yeokcham_status_t yeokcham_client_start(yeokcham_client_t *client); // requires a validated attached configuration
yeokcham_status_t yeokcham_client_stop(yeokcham_client_t *client); // requires a running client
yeokcham_status_t yeokcham_client_identity_create(
    yeokcham_client_t *client,
    uint8_t public_key[YEOKCHAM_IDENTITY_PUBLIC_KEY_BYTES]
); // creates one handle-scoped identity and clears public_key before any failure
yeokcham_status_t yeokcham_client_identity_load(
    yeokcham_client_t *client,
    uint8_t public_key[YEOKCHAM_IDENTITY_PUBLIC_KEY_BYTES]
); // loads the handle-scoped identity and clears public_key before any failure
yeokcham_status_t yeokcham_client_copy_last_error_detail(
    const yeokcham_client_t *client,
    uint8_t *buffer,
    size_t buffer_capacity,
    size_t *detail_length
); // caller-owned writable buffer; query length with null buffer and zero capacity
yeokcham_status_t yeokcham_client_take_last_error_detail(
    yeokcham_client_t *client,
    yeokcham_buffer_t **detail
); // transfers the last detail into a library-owned buffer; null on failure
const uint8_t *yeokcham_buffer_data(const yeokcham_buffer_t *buffer); // valid until release; caller synchronizes release with use
size_t yeokcham_buffer_length(const yeokcham_buffer_t *buffer); // zero for invalid buffers
yeokcham_status_t yeokcham_buffer_release(yeokcham_buffer_t *buffer); // exactly one active release returns ok
yeokcham_event_subscription_t *yeokcham_client_subscribe_events(const yeokcham_client_t *client); // null unless client is running or capacity is exhausted
yeokcham_status_t yeokcham_event_subscription_poll(
    yeokcham_event_subscription_t *subscription,
    yeokcham_event_t *event,
    uint8_t *has_event
); // never blocks; with valid outputs, clears them before returning a non-ok status or an empty poll
yeokcham_status_t yeokcham_event_subscription_release(yeokcham_event_subscription_t *subscription); // exactly one active release returns ok
yeokcham_client_config_builder_t *yeokcham_client_config_builder_create(void); // null when builder capacity is exhausted
yeokcham_status_t yeokcham_client_config_builder_release(yeokcham_client_config_builder_t *builder); // invalid input unless builder is active
yeokcham_status_t yeokcham_client_config_builder_set_state_directory(
    yeokcham_client_config_builder_t *builder,
    const uint8_t *state_directory,
    size_t state_directory_length
); // state directory bytes must be nonempty UTF-8 without NUL
yeokcham_status_t yeokcham_client_config_builder_set_event_buffer_capacity(
    yeokcham_client_config_builder_t *builder,
    uint32_t event_buffer_capacity
); // zero is rejected when building
yeokcham_status_t yeokcham_client_config_builder_build(
    const yeokcham_client_config_builder_t *builder,
    const yeokcham_client_t *client
); // validates and attaches an embedded client configuration
yeokcham_status_t yeokcham_client_complete_async(
    const yeokcham_client_t *client,
    yeokcham_completion_callback_t callback,
    void *context
); // callback may reenter the ABI and release the submitted client
uint32_t yeokcham_abi_negotiate(uint32_t requested_version); // call first; returns requested token only on exact pre-release match, else YEOKCHAM_ABI_NEGOTIATION_REJECTED
yeokcham_status_t yeokcham_secret_buffer_zeroize(uint8_t *buffer, size_t length); // caller-owned writable bytes only

#endif
