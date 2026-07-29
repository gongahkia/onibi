#ifndef ARACHNE_V1_H
#define ARACHNE_V1_H

#include <stdint.h>
#include <stddef.h>

#define ARACHNE_ABI_VERSION_MAJOR UINT32_C(1)
#define ARACHNE_ABI_VERSION_MINOR UINT32_C(0)
#define ARACHNE_ABI_VERSION UINT32_C(1)
#define ARACHNE_ABI_NEGOTIATION_REJECTED UINT32_C(0)
#define ARACHNE_MAX_CALLBACK_WORKERS UINT32_C(4)
#define ARACHNE_MAX_BUFFERS UINT32_C(1024)
#define ARACHNE_MAX_ERROR_DETAIL_BYTES UINT32_C(64)
#define ARACHNE_MAX_EVENT_SUBSCRIPTIONS UINT32_C(1024)
#define ARACHNE_MAX_PENDING_COMPLETIONS UINT32_C(1024)
#define ARACHNE_EVENT_VERSION UINT32_C(1)
#define ARACHNE_EVENT_MESSAGE_IDENTIFIER_BYTES UINT32_C(16)
#define ARACHNE_EVENT_CLIENT_STARTED UINT32_C(1)
#define ARACHNE_EVENT_CLIENT_STOPPED UINT32_C(2)
#define ARACHNE_EVENT_MESSAGE_QUEUED UINT32_C(3)
#define ARACHNE_EVENT_MESSAGE_DELIVERED UINT32_C(4)
#define ARACHNE_EVENT_MESSAGE_DELIVERY_FAILED UINT32_C(5)
#define ARACHNE_IDENTITY_PUBLIC_KEY_BYTES UINT32_C(32)
#define ARACHNE_RECOVERY_ARCHIVE_BYTES UINT32_C(122)
#define ARACHNE_MAX_RECOVERY_PASSPHRASE_BYTES UINT32_C(1024)
#define ARACHNE_CONTACT_INVITATION_BYTES UINT32_C(136)
#define ARACHNE_CONTACT_STATUS_PENDING UINT32_C(1)
#define ARACHNE_CONTACT_STATUS_VERIFIED UINT32_C(2)
#define ARACHNE_CONTACT_STATUS_REVOKED UINT32_C(3)
#define ARACHNE_CONTACT_VERIFICATION_NONE UINT32_C(0)
#define ARACHNE_CONTACT_VERIFICATION_QR UINT32_C(1)
#define ARACHNE_CONTACT_VERIFICATION_SAFETY_NUMBER UINT32_C(2)
#define ARACHNE_QR_VERIFICATION_PAYLOAD_BYTES UINT32_C(70)
#define ARACHNE_SAFETY_NUMBER_FINGERPRINT_BYTES UINT32_C(32)
#define ARACHNE_MAX_LOCAL_MESH_TRANSPORTS UINT32_C(4)
#define ARACHNE_DELIVERY_PROFILE_DIRECT UINT32_C(1)
#define ARACHNE_DELIVERY_PROFILE_TOR_MAILDROP UINT32_C(2)
#define ARACHNE_DELIVERY_PROFILE_LOCAL_MESH UINT32_C(3)
#define ARACHNE_LOCAL_MESH_TRANSPORT_LAN UINT32_C(1)
#define ARACHNE_LOCAL_MESH_TRANSPORT_WIFI_HOTSPOT UINT32_C(2)
#define ARACHNE_LOCAL_MESH_TRANSPORT_WIFI_DIRECT UINT32_C(3)
#define ARACHNE_LOCAL_MESH_TRANSPORT_BLUETOOTH UINT32_C(4)
#define ARACHNE_DIRECT_IP_DISCLOSURE_ACKNOWLEDGED UINT32_C(1)
#define ARACHNE_DIRECT_IP_DISCLOSURE_WARNING UINT32_C(1)
#define ARACHNE_MESSAGE_IDENTIFIER_BYTES UINT32_C(16)
#define ARACHNE_MAX_MESSAGE_ENVELOPE_BYTES UINT32_C(1048544)
#define ARACHNE_MAX_ATTACHMENT_TRANSFERS UINT32_C(1024)
#define ARACHNE_ATTACHMENT_IDENTIFIER_BYTES UINT32_C(16)
#define ARACHNE_MAX_ATTACHMENT_CHUNKS UINT32_C(1600)
#define ARACHNE_MAX_ATTACHMENT_CHUNKS_PER_CYCLE UINT32_C(64)
#define ARACHNE_MAX_ATTACHMENT_MANIFEST_BYTES UINT32_C(4194304)
#define ARACHNE_MAX_ATTACHMENT_CHUNK_BYTES UINT32_C(65640)
#define ARACHNE_ATTACHMENT_DELIVERY_COMPLETE UINT32_C(1)
#define ARACHNE_ATTACHMENT_DELIVERY_PENDING UINT32_C(2)
#define ARACHNE_ATTACHMENT_DELIVERY_RETRYING UINT32_C(3)
#define ARACHNE_MAX_CANCELLATIONS UINT32_C(1024)
#define ARACHNE_MAX_CANCELLATION_DEADLINE_MILLISECONDS UINT32_C(60000)

typedef struct arachne_client arachne_client_t; // library-owned opaque client; release with arachne_client_release
typedef struct arachne_client_config_builder arachne_client_config_builder_t; // library-owned configuration builder; release with arachne_client_config_builder_release
typedef struct arachne_buffer arachne_buffer_t; // library-owned opaque bytes; release with arachne_buffer_release
typedef struct arachne_event_subscription arachne_event_subscription_t; // library-owned opaque stream; release with arachne_event_subscription_release
typedef struct arachne_attachment_transfer arachne_attachment_transfer_t; // library-owned opaque transfer; release with arachne_attachment_transfer_release
typedef struct arachne_cancellation arachne_cancellation_t; // library-owned opaque cancellation; release with arachne_cancellation_release
typedef struct arachne_event {
    uint32_t version;
    uint64_t sequence;
    uint32_t kind;
    uint8_t message_identifier[ARACHNE_EVENT_MESSAGE_IDENTIFIER_BYTES];
} arachne_event_t;
typedef struct arachne_contact {
    uint8_t identity[ARACHNE_IDENTITY_PUBLIC_KEY_BYTES];
    uint32_t status;
    uint32_t verification;
} arachne_contact_t;
typedef struct arachne_delivery_profile_policy {
    uint32_t direct_allowed;
    uint32_t tor_maildrop_allowed;
    uint32_t local_mesh_transport_count;
    uint32_t local_mesh_transports[ARACHNE_MAX_LOCAL_MESH_TRANSPORTS];
} arachne_delivery_profile_policy_t;
typedef struct arachne_delivery_profile {
    uint32_t kind;
    uint32_t direct_ip_disclosure_warning;
} arachne_delivery_profile_t;
typedef struct arachne_byte_slice {
    const uint8_t *data;
    size_t length;
} arachne_byte_slice_t;
typedef struct arachne_attachment_delivery_cycle {
    uint32_t uploaded;
    uint32_t outcome;
    uint32_t next_pending_index;
} arachne_attachment_delivery_cycle_t;

typedef int32_t arachne_status_t;
typedef void (*arachne_completion_callback_t)(arachne_status_t status, void *context);
typedef arachne_status_t (*arachne_attachment_upload_callback_t)(const uint8_t *manifest, size_t manifest_length, const uint8_t *chunk, size_t chunk_length, void *context);

#define ARACHNE_STATUS_OK INT32_C(0)
#define ARACHNE_STATUS_INVALID_INPUT INT32_C(1)
#define ARACHNE_STATUS_UNSUPPORTED_VERSION INT32_C(2)
#define ARACHNE_STATUS_RESOURCE_LIMIT INT32_C(3)
#define ARACHNE_STATUS_STATE INT32_C(4)

arachne_client_t *arachne_client_create(void); // null when client capacity is exhausted
arachne_status_t arachne_client_release(arachne_client_t *client); // invalid input unless client is active
arachne_status_t arachne_client_start(arachne_client_t *client); // requires a validated attached configuration
arachne_status_t arachne_client_stop(arachne_client_t *client); // requires a running client
arachne_status_t arachne_client_identity_create(
    arachne_client_t *client,
    uint8_t public_key[ARACHNE_IDENTITY_PUBLIC_KEY_BYTES]
); // creates one handle-scoped identity and clears public_key before any failure
arachne_status_t arachne_client_identity_load(
    arachne_client_t *client,
    uint8_t public_key[ARACHNE_IDENTITY_PUBLIC_KEY_BYTES]
); // loads the handle-scoped identity and clears public_key before any failure
arachne_status_t arachne_client_identity_export_recovery(
    arachne_client_t *client,
    const uint8_t *passphrase,
    size_t passphrase_length,
    arachne_buffer_t **archive
); // requires a nonempty bounded passphrase and handle identity; nulls archive before any failure; release the opaque encrypted archive buffer
arachne_status_t arachne_client_identity_import_recovery(
    arachne_client_t *client,
    const uint8_t *archive,
    size_t archive_length,
    const uint8_t *passphrase,
    size_t passphrase_length,
    uint8_t public_key[ARACHNE_IDENTITY_PUBLIC_KEY_BYTES]
); // requires exactly ARACHNE_RECOVERY_ARCHIVE_BYTES and a nonempty bounded passphrase; clears public_key before any failure
arachne_status_t arachne_client_contact_import(
    arachne_client_t *client,
    const uint8_t invitation[ARACHNE_CONTACT_INVITATION_BYTES],
    arachne_contact_t *contact
); // requires a running client with an identity; clears contact before any failure
arachne_status_t arachne_client_contact_get(
    arachne_client_t *client,
    const uint8_t identity[ARACHNE_IDENTITY_PUBLIC_KEY_BYTES],
    arachne_contact_t *contact
); // requires a running client with an identity; clears contact before any failure
arachne_status_t arachne_client_contact_revoke(
    arachne_client_t *client,
    const uint8_t identity[ARACHNE_IDENTITY_PUBLIC_KEY_BYTES],
    arachne_contact_t *contact
); // requires a running client with an identity; clears contact before any failure
arachne_status_t arachne_client_contact_verify_qr(
    arachne_client_t *client,
    const uint8_t payload[ARACHNE_QR_VERIFICATION_PAYLOAD_BYTES],
    arachne_contact_t *contact
); // requires a running client with an identity; clears contact before any failure
arachne_status_t arachne_client_contact_verify_safety_number(
    arachne_client_t *client,
    const uint8_t identity[ARACHNE_IDENTITY_PUBLIC_KEY_BYTES],
    const uint8_t fingerprint[ARACHNE_SAFETY_NUMBER_FINGERPRINT_BYTES],
    arachne_contact_t *contact
); // requires a running client with an identity; clears contact before any failure
arachne_status_t arachne_delivery_profile_select(
    const arachne_delivery_profile_policy_t *policy,
    uint32_t kind,
    uint32_t local_mesh_transport,
    uint32_t direct_ip_disclosure_acknowledged,
    arachne_delivery_profile_t *profile
); // direct requires acknowledgement; tor requires zero acknowledgement and transport; local mesh requires zero acknowledgement and allowed transport; clears profile before failure
arachne_status_t arachne_client_message_send(
    arachne_client_t *client,
    const uint8_t recipient[ARACHNE_IDENTITY_PUBLIC_KEY_BYTES],
    const uint8_t *envelope,
    size_t envelope_length,
    uint64_t created_at,
    uint32_t ttl_seconds,
    uint8_t message_identifier[ARACHNE_MESSAGE_IDENTIFIER_BYTES]
); // requires a running client with an identity and canonical bounded envelope; clears message_identifier before any failure
arachne_attachment_transfer_t *arachne_attachment_transfer_create(
    const uint8_t *manifest,
    size_t manifest_length,
    const arachne_byte_slice_t *chunks,
    size_t chunk_count,
    uint32_t maximum_chunks_per_cycle
); // null for invalid bounded transfer input or transfer capacity exhaustion
arachne_status_t arachne_attachment_transfer_run_cycle(
    arachne_attachment_transfer_t *transfer,
    arachne_attachment_upload_callback_t upload,
    void *context,
    arachne_attachment_delivery_cycle_t *cycle
); // callback must synchronously return ok only after upload succeeds; clears cycle before any failure
arachne_status_t arachne_attachment_transfer_release(arachne_attachment_transfer_t *transfer); // exactly one idle active release returns ok
arachne_cancellation_t *arachne_cancellation_create(void); // null when cancellation capacity is exhausted
arachne_status_t arachne_cancellation_cancel(arachne_cancellation_t *cancellation); // idempotently cancels one active handle
arachne_status_t arachne_cancellation_release(arachne_cancellation_t *cancellation); // exactly one active release returns ok
arachne_status_t arachne_event_subscription_wait(
    arachne_event_subscription_t *subscription,
    arachne_cancellation_t *cancellation,
    uint32_t deadline_milliseconds,
    arachne_event_t *event,
    uint8_t *has_event
); // blocks until one event, cancellation, or deadline; clears outputs before any failure or empty result
arachne_status_t arachne_client_copy_last_error_detail(
    const arachne_client_t *client,
    uint8_t *buffer,
    size_t buffer_capacity,
    size_t *detail_length
); // caller-owned writable buffer; query length with null buffer and zero capacity
arachne_status_t arachne_client_take_last_error_detail(
    arachne_client_t *client,
    arachne_buffer_t **detail
); // transfers the last detail into a library-owned buffer; null on failure
const uint8_t *arachne_buffer_data(const arachne_buffer_t *buffer); // valid until release; caller synchronizes release with use
size_t arachne_buffer_length(const arachne_buffer_t *buffer); // zero for invalid buffers
arachne_status_t arachne_buffer_release(arachne_buffer_t *buffer); // exactly one active release returns ok
arachne_event_subscription_t *arachne_client_subscribe_events(const arachne_client_t *client); // null unless client is running or capacity is exhausted
arachne_status_t arachne_event_subscription_poll(
    arachne_event_subscription_t *subscription,
    arachne_event_t *event,
    uint8_t *has_event
); // never blocks; with valid outputs, clears them before returning a non-ok status or an empty poll
arachne_status_t arachne_event_subscription_release(arachne_event_subscription_t *subscription); // exactly one active release returns ok
arachne_client_config_builder_t *arachne_client_config_builder_create(void); // null when builder capacity is exhausted
arachne_status_t arachne_client_config_builder_release(arachne_client_config_builder_t *builder); // invalid input unless builder is active
arachne_status_t arachne_client_config_builder_set_state_directory(
    arachne_client_config_builder_t *builder,
    const uint8_t *state_directory,
    size_t state_directory_length
); // state directory bytes must be nonempty UTF-8 without NUL
arachne_status_t arachne_client_config_builder_set_event_buffer_capacity(
    arachne_client_config_builder_t *builder,
    uint32_t event_buffer_capacity
); // zero is rejected when building
arachne_status_t arachne_client_config_builder_build(
    const arachne_client_config_builder_t *builder,
    const arachne_client_t *client
); // validates and attaches an embedded client configuration
arachne_status_t arachne_client_complete_async(
    const arachne_client_t *client,
    arachne_completion_callback_t callback,
    void *context
); // callback may reenter the ABI and release the submitted client
uint32_t arachne_abi_negotiate(uint32_t requested_version); // call first; returns requested token only on exact pre-release match, else ARACHNE_ABI_NEGOTIATION_REJECTED
arachne_status_t arachne_secret_buffer_zeroize(uint8_t *buffer, size_t length); // caller-owned writable bytes only

#endif
