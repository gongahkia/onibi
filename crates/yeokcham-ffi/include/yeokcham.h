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
#define YEOKCHAM_CONTACT_INVITATION_BYTES UINT32_C(136)
#define YEOKCHAM_CONTACT_STATUS_PENDING UINT32_C(1)
#define YEOKCHAM_CONTACT_STATUS_VERIFIED UINT32_C(2)
#define YEOKCHAM_CONTACT_STATUS_REVOKED UINT32_C(3)
#define YEOKCHAM_CONTACT_VERIFICATION_NONE UINT32_C(0)
#define YEOKCHAM_CONTACT_VERIFICATION_QR UINT32_C(1)
#define YEOKCHAM_CONTACT_VERIFICATION_SAFETY_NUMBER UINT32_C(2)
#define YEOKCHAM_QR_VERIFICATION_PAYLOAD_BYTES UINT32_C(70)
#define YEOKCHAM_SAFETY_NUMBER_FINGERPRINT_BYTES UINT32_C(32)
#define YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS UINT32_C(4)
#define YEOKCHAM_DELIVERY_PROFILE_DIRECT UINT32_C(1)
#define YEOKCHAM_DELIVERY_PROFILE_TOR_MAILDROP UINT32_C(2)
#define YEOKCHAM_DELIVERY_PROFILE_LOCAL_MESH UINT32_C(3)
#define YEOKCHAM_LOCAL_MESH_TRANSPORT_LAN UINT32_C(1)
#define YEOKCHAM_LOCAL_MESH_TRANSPORT_WIFI_HOTSPOT UINT32_C(2)
#define YEOKCHAM_LOCAL_MESH_TRANSPORT_WIFI_DIRECT UINT32_C(3)
#define YEOKCHAM_LOCAL_MESH_TRANSPORT_BLUETOOTH UINT32_C(4)
#define YEOKCHAM_DIRECT_IP_DISCLOSURE_ACKNOWLEDGED UINT32_C(1)
#define YEOKCHAM_DIRECT_IP_DISCLOSURE_WARNING UINT32_C(1)
#define YEOKCHAM_MESSAGE_IDENTIFIER_BYTES UINT32_C(16)
#define YEOKCHAM_MAX_MESSAGE_ENVELOPE_BYTES UINT32_C(1048544)
#define YEOKCHAM_MAX_ATTACHMENT_TRANSFERS UINT32_C(1024)
#define YEOKCHAM_ATTACHMENT_IDENTIFIER_BYTES UINT32_C(16)
#define YEOKCHAM_MAX_ATTACHMENT_CHUNKS UINT32_C(1600)
#define YEOKCHAM_MAX_ATTACHMENT_CHUNKS_PER_CYCLE UINT32_C(64)
#define YEOKCHAM_MAX_ATTACHMENT_MANIFEST_BYTES UINT32_C(4194304)
#define YEOKCHAM_MAX_ATTACHMENT_CHUNK_BYTES UINT32_C(65640)
#define YEOKCHAM_ATTACHMENT_DELIVERY_COMPLETE UINT32_C(1)
#define YEOKCHAM_ATTACHMENT_DELIVERY_PENDING UINT32_C(2)
#define YEOKCHAM_ATTACHMENT_DELIVERY_RETRYING UINT32_C(3)
#define YEOKCHAM_MAX_CANCELLATIONS UINT32_C(1024)
#define YEOKCHAM_MAX_CANCELLATION_DEADLINE_MILLISECONDS UINT32_C(60000)

typedef struct yeokcham_client yeokcham_client_t; // library-owned opaque client; release with yeokcham_client_release
typedef struct yeokcham_client_config_builder yeokcham_client_config_builder_t; // library-owned configuration builder; release with yeokcham_client_config_builder_release
typedef struct yeokcham_buffer yeokcham_buffer_t; // library-owned opaque bytes; release with yeokcham_buffer_release
typedef struct yeokcham_event_subscription yeokcham_event_subscription_t; // library-owned opaque stream; release with yeokcham_event_subscription_release
typedef struct yeokcham_attachment_transfer yeokcham_attachment_transfer_t; // library-owned opaque transfer; release with yeokcham_attachment_transfer_release
typedef struct yeokcham_cancellation yeokcham_cancellation_t; // library-owned opaque cancellation; release with yeokcham_cancellation_release
typedef struct yeokcham_event {
    uint32_t version;
    uint64_t sequence;
    uint32_t kind;
    uint8_t message_identifier[YEOKCHAM_EVENT_MESSAGE_IDENTIFIER_BYTES];
} yeokcham_event_t;
typedef struct yeokcham_contact {
    uint8_t identity[YEOKCHAM_IDENTITY_PUBLIC_KEY_BYTES];
    uint32_t status;
    uint32_t verification;
} yeokcham_contact_t;
typedef struct yeokcham_delivery_profile_policy {
    uint32_t direct_allowed;
    uint32_t tor_maildrop_allowed;
    uint32_t local_mesh_transport_count;
    uint32_t local_mesh_transports[YEOKCHAM_MAX_LOCAL_MESH_TRANSPORTS];
} yeokcham_delivery_profile_policy_t;
typedef struct yeokcham_delivery_profile {
    uint32_t kind;
    uint32_t direct_ip_disclosure_warning;
} yeokcham_delivery_profile_t;
typedef struct yeokcham_byte_slice {
    const uint8_t *data;
    size_t length;
} yeokcham_byte_slice_t;
typedef struct yeokcham_attachment_delivery_cycle {
    uint32_t uploaded;
    uint32_t outcome;
    uint32_t next_pending_index;
} yeokcham_attachment_delivery_cycle_t;

typedef int32_t yeokcham_status_t;
typedef void (*yeokcham_completion_callback_t)(yeokcham_status_t status, void *context);
typedef yeokcham_status_t (*yeokcham_attachment_upload_callback_t)(const uint8_t *manifest, size_t manifest_length, const uint8_t *chunk, size_t chunk_length, void *context);

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
yeokcham_status_t yeokcham_client_contact_import(
    yeokcham_client_t *client,
    const uint8_t invitation[YEOKCHAM_CONTACT_INVITATION_BYTES],
    yeokcham_contact_t *contact
); // requires a running client with an identity; clears contact before any failure
yeokcham_status_t yeokcham_client_contact_get(
    yeokcham_client_t *client,
    const uint8_t identity[YEOKCHAM_IDENTITY_PUBLIC_KEY_BYTES],
    yeokcham_contact_t *contact
); // requires a running client with an identity; clears contact before any failure
yeokcham_status_t yeokcham_client_contact_revoke(
    yeokcham_client_t *client,
    const uint8_t identity[YEOKCHAM_IDENTITY_PUBLIC_KEY_BYTES],
    yeokcham_contact_t *contact
); // requires a running client with an identity; clears contact before any failure
yeokcham_status_t yeokcham_client_contact_verify_qr(
    yeokcham_client_t *client,
    const uint8_t payload[YEOKCHAM_QR_VERIFICATION_PAYLOAD_BYTES],
    yeokcham_contact_t *contact
); // requires a running client with an identity; clears contact before any failure
yeokcham_status_t yeokcham_client_contact_verify_safety_number(
    yeokcham_client_t *client,
    const uint8_t identity[YEOKCHAM_IDENTITY_PUBLIC_KEY_BYTES],
    const uint8_t fingerprint[YEOKCHAM_SAFETY_NUMBER_FINGERPRINT_BYTES],
    yeokcham_contact_t *contact
); // requires a running client with an identity; clears contact before any failure
yeokcham_status_t yeokcham_delivery_profile_select(
    const yeokcham_delivery_profile_policy_t *policy,
    uint32_t kind,
    uint32_t local_mesh_transport,
    uint32_t direct_ip_disclosure_acknowledged,
    yeokcham_delivery_profile_t *profile
); // direct requires acknowledgement; tor requires zero acknowledgement and transport; local mesh requires zero acknowledgement and allowed transport; clears profile before failure
yeokcham_status_t yeokcham_client_message_send(
    yeokcham_client_t *client,
    const uint8_t recipient[YEOKCHAM_IDENTITY_PUBLIC_KEY_BYTES],
    const uint8_t *envelope,
    size_t envelope_length,
    uint64_t created_at,
    uint32_t ttl_seconds,
    uint8_t message_identifier[YEOKCHAM_MESSAGE_IDENTIFIER_BYTES]
); // requires a running client with an identity and canonical bounded envelope; clears message_identifier before any failure
yeokcham_attachment_transfer_t *yeokcham_attachment_transfer_create(
    const uint8_t *manifest,
    size_t manifest_length,
    const yeokcham_byte_slice_t *chunks,
    size_t chunk_count,
    uint32_t maximum_chunks_per_cycle
); // null for invalid bounded transfer input or transfer capacity exhaustion
yeokcham_status_t yeokcham_attachment_transfer_run_cycle(
    yeokcham_attachment_transfer_t *transfer,
    yeokcham_attachment_upload_callback_t upload,
    void *context,
    yeokcham_attachment_delivery_cycle_t *cycle
); // callback must synchronously return ok only after upload succeeds; clears cycle before any failure
yeokcham_status_t yeokcham_attachment_transfer_release(yeokcham_attachment_transfer_t *transfer); // exactly one idle active release returns ok
yeokcham_cancellation_t *yeokcham_cancellation_create(void); // null when cancellation capacity is exhausted
yeokcham_status_t yeokcham_cancellation_cancel(yeokcham_cancellation_t *cancellation); // idempotently cancels one active handle
yeokcham_status_t yeokcham_cancellation_release(yeokcham_cancellation_t *cancellation); // exactly one active release returns ok
yeokcham_status_t yeokcham_event_subscription_wait(
    yeokcham_event_subscription_t *subscription,
    yeokcham_cancellation_t *cancellation,
    uint32_t deadline_milliseconds,
    yeokcham_event_t *event,
    uint8_t *has_event
); // blocks until one event, cancellation, or deadline; clears outputs before any failure or empty result
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
