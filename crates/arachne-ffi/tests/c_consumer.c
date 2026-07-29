#include "arachne.h"

int arachne_c_consumer_conformance(void) {
    uint8_t secret[] = {0xa5, 0x5a, 0x11};
    const uint8_t state_directory[] = "/tmp/arachne-ffi-c-consumer";
    uint8_t created_identity[ARACHNE_IDENTITY_PUBLIC_KEY_BYTES];
    uint8_t loaded_identity[ARACHNE_IDENTITY_PUBLIC_KEY_BYTES];
    arachne_client_t *client;
    arachne_client_config_builder_t *builder;
    size_t index;

    if (arachne_abi_negotiate(ARACHNE_ABI_VERSION) != ARACHNE_ABI_VERSION) {
        return 1;
    }
    if (arachne_abi_negotiate(ARACHNE_ABI_NEGOTIATION_REJECTED) != ARACHNE_ABI_NEGOTIATION_REJECTED) {
        return 2;
    }
    if (arachne_abi_negotiate(ARACHNE_ABI_VERSION + UINT32_C(1)) != ARACHNE_ABI_NEGOTIATION_REJECTED) {
        return 3;
    }
    client = arachne_client_create();
    if (client == NULL) {
        return 4;
    }
    builder = arachne_client_config_builder_create();
    if (builder == NULL) {
        return 5;
    }
    if (arachne_client_config_builder_set_state_directory(builder, state_directory, sizeof(state_directory) - UINT32_C(1)) != ARACHNE_STATUS_OK) {
        return 6;
    }
    if (arachne_client_config_builder_set_event_buffer_capacity(builder, UINT32_C(8)) != ARACHNE_STATUS_OK) {
        return 7;
    }
    if (arachne_client_config_builder_build(builder, client) != ARACHNE_STATUS_OK) {
        return 8;
    }
    if (arachne_client_config_builder_release(builder) != ARACHNE_STATUS_OK) {
        return 9;
    }
    if (arachne_client_complete_async(client, NULL, NULL) != ARACHNE_STATUS_INVALID_INPUT) {
        return 10;
    }
    if (arachne_secret_buffer_zeroize(secret, sizeof(secret)) != ARACHNE_STATUS_OK) {
        return 11;
    }
    if (secret[0] != 0 || secret[1] != 0 || secret[2] != 0) {
        return 12;
    }
    for (index = 0; index < ARACHNE_IDENTITY_PUBLIC_KEY_BYTES; ++index) {
        created_identity[index] = UINT8_C(0xa5);
    }
    if (arachne_client_identity_load(client, created_identity) != ARACHNE_STATUS_STATE) {
        return 15;
    }
    for (index = 0; index < ARACHNE_IDENTITY_PUBLIC_KEY_BYTES; ++index) {
        if (created_identity[index] != 0) {
            return 16;
        }
    }
    if (arachne_client_identity_create(client, created_identity) != ARACHNE_STATUS_OK) {
        return 17;
    }
    if (arachne_client_identity_load(client, loaded_identity) != ARACHNE_STATUS_OK) {
        return 18;
    }
    for (index = 0; index < ARACHNE_IDENTITY_PUBLIC_KEY_BYTES; ++index) {
        if (created_identity[index] != loaded_identity[index]) {
            return 19;
        }
    }
    if (arachne_client_release(client) != ARACHNE_STATUS_OK) {
        return 13;
    }
    if (arachne_client_release(client) != ARACHNE_STATUS_INVALID_INPUT) {
        return 14;
    }
    return 0;
}

int arachne_c_embedded_client_lifecycle(
    const uint8_t *state_directory,
    size_t state_directory_length
) {
    arachne_client_t *client;
    arachne_client_config_builder_t *builder;
    arachne_event_subscription_t *subscription;
    arachne_event_t event;
    uint8_t has_event;
    size_t index;

    if (state_directory == NULL || state_directory_length == 0) {
        return 1;
    }
    client = arachne_client_create();
    if (client == NULL) {
        return 2;
    }
    builder = arachne_client_config_builder_create();
    if (builder == NULL) {
        (void)arachne_client_release(client);
        return 3;
    }
    if (arachne_client_config_builder_set_state_directory(builder, state_directory, state_directory_length) != ARACHNE_STATUS_OK) {
        (void)arachne_client_config_builder_release(builder);
        (void)arachne_client_release(client);
        return 4;
    }
    if (arachne_client_config_builder_set_event_buffer_capacity(builder, UINT32_C(8)) != ARACHNE_STATUS_OK) {
        (void)arachne_client_config_builder_release(builder);
        (void)arachne_client_release(client);
        return 5;
    }
    if (arachne_client_config_builder_build(builder, client) != ARACHNE_STATUS_OK) {
        (void)arachne_client_config_builder_release(builder);
        (void)arachne_client_release(client);
        return 6;
    }
    if (arachne_client_config_builder_release(builder) != ARACHNE_STATUS_OK) {
        (void)arachne_client_release(client);
        return 7;
    }
    if (arachne_client_start(client) != ARACHNE_STATUS_OK) {
        (void)arachne_client_release(client);
        return 8;
    }
    subscription = arachne_client_subscribe_events(client);
    if (subscription == NULL) {
        (void)arachne_client_stop(client);
        (void)arachne_client_release(client);
        return 9;
    }
    has_event = UINT8_C(1);
    if (arachne_event_subscription_poll(subscription, &event, &has_event) != ARACHNE_STATUS_OK || has_event != 0 || event.version != 0 || event.sequence != 0 || event.kind != 0) {
        (void)arachne_event_subscription_release(subscription);
        (void)arachne_client_stop(client);
        (void)arachne_client_release(client);
        return 10;
    }
    if (arachne_client_start(client) != ARACHNE_STATUS_STATE) {
        (void)arachne_event_subscription_release(subscription);
        (void)arachne_client_stop(client);
        (void)arachne_client_release(client);
        return 11;
    }
    if (arachne_client_stop(client) != ARACHNE_STATUS_OK) {
        (void)arachne_event_subscription_release(subscription);
        (void)arachne_client_release(client);
        return 12;
    }
    if (arachne_event_subscription_poll(subscription, &event, &has_event) != ARACHNE_STATUS_OK || has_event != UINT8_C(1) || event.version != ARACHNE_EVENT_VERSION || event.sequence != UINT64_C(2) || event.kind != ARACHNE_EVENT_CLIENT_STOPPED) {
        (void)arachne_event_subscription_release(subscription);
        (void)arachne_client_release(client);
        return 13;
    }
    for (index = 0; index < ARACHNE_EVENT_MESSAGE_IDENTIFIER_BYTES; ++index) {
        if (event.message_identifier[index] != 0) {
            (void)arachne_event_subscription_release(subscription);
            (void)arachne_client_release(client);
            return 14;
        }
    }
    if (arachne_event_subscription_poll(subscription, &event, &has_event) != ARACHNE_STATUS_STATE || has_event != 0 || event.version != 0 || event.sequence != 0 || event.kind != 0) {
        (void)arachne_event_subscription_release(subscription);
        (void)arachne_client_release(client);
        return 15;
    }
    if (arachne_event_subscription_release(subscription) != ARACHNE_STATUS_OK) {
        (void)arachne_client_release(client);
        return 16;
    }
    if (arachne_event_subscription_release(subscription) != ARACHNE_STATUS_INVALID_INPUT) {
        (void)arachne_client_release(client);
        return 17;
    }
    if (arachne_client_stop(client) != ARACHNE_STATUS_STATE) {
        (void)arachne_client_release(client);
        return 18;
    }
    if (arachne_client_release(client) != ARACHNE_STATUS_OK) {
        return 19;
    }
    return 0;
}

int arachne_c_delivery_profile_operations(void) {
    arachne_delivery_profile_policy_t policy = {
        UINT32_C(1),
        UINT32_C(1),
        ARACHNE_MAX_LOCAL_MESH_TRANSPORTS,
        {
            ARACHNE_LOCAL_MESH_TRANSPORT_LAN,
            ARACHNE_LOCAL_MESH_TRANSPORT_WIFI_HOTSPOT,
            ARACHNE_LOCAL_MESH_TRANSPORT_WIFI_DIRECT,
            ARACHNE_LOCAL_MESH_TRANSPORT_BLUETOOTH
        }
    };
    arachne_delivery_profile_t profile = { UINT32_C(99), UINT32_C(99) };

    if (arachne_delivery_profile_select(&policy, ARACHNE_DELIVERY_PROFILE_DIRECT, UINT32_C(0), ARACHNE_DIRECT_IP_DISCLOSURE_ACKNOWLEDGED, &profile) != ARACHNE_STATUS_OK) {
        return 1;
    }
    if (profile.kind != ARACHNE_DELIVERY_PROFILE_DIRECT || profile.direct_ip_disclosure_warning != ARACHNE_DIRECT_IP_DISCLOSURE_WARNING) {
        return 2;
    }
    if (arachne_delivery_profile_select(&policy, ARACHNE_DELIVERY_PROFILE_LOCAL_MESH, ARACHNE_LOCAL_MESH_TRANSPORT_BLUETOOTH, UINT32_C(0), &profile) != ARACHNE_STATUS_OK) {
        return 3;
    }
    if (profile.kind != ARACHNE_DELIVERY_PROFILE_LOCAL_MESH || profile.direct_ip_disclosure_warning != 0) {
        return 4;
    }
    if (arachne_delivery_profile_select(&policy, ARACHNE_DELIVERY_PROFILE_DIRECT, UINT32_C(0), UINT32_C(0), &profile) != ARACHNE_STATUS_INVALID_INPUT) {
        return 5;
    }
    if (profile.kind != 0 || profile.direct_ip_disclosure_warning != 0) {
        return 6;
    }
    return 0;
}

int arachne_c_recovery_operations(void) {
    const uint8_t passphrase[] = "C recovery passphrase";
    uint8_t source_identity[ARACHNE_IDENTITY_PUBLIC_KEY_BYTES];
    uint8_t recovered_identity[ARACHNE_IDENTITY_PUBLIC_KEY_BYTES];
    arachne_client_t *source = arachne_client_create();
    arachne_client_t *target = arachne_client_create();
    arachne_buffer_t *archive = NULL;
    size_t index;
    int result = 1;

    if (source == NULL || target == NULL) {
        goto cleanup;
    }
    if (arachne_client_identity_create(source, source_identity) != ARACHNE_STATUS_OK) {
        result = 2;
        goto cleanup;
    }
    if (arachne_client_identity_export_recovery(source, passphrase, sizeof(passphrase) - UINT32_C(1), &archive) != ARACHNE_STATUS_OK || archive == NULL) {
        result = 3;
        goto cleanup;
    }
    if (arachne_buffer_data(archive) == NULL || arachne_buffer_length(archive) != ARACHNE_RECOVERY_ARCHIVE_BYTES) {
        result = 4;
        goto cleanup;
    }
    if (arachne_client_identity_import_recovery(target, arachne_buffer_data(archive), arachne_buffer_length(archive), passphrase, sizeof(passphrase) - UINT32_C(1), recovered_identity) != ARACHNE_STATUS_OK) {
        result = 5;
        goto cleanup;
    }
    for (index = 0; index < ARACHNE_IDENTITY_PUBLIC_KEY_BYTES; ++index) {
        if (source_identity[index] != recovered_identity[index]) {
            result = 6;
            goto cleanup;
        }
    }
    result = 0;

cleanup:
    if (archive != NULL && arachne_buffer_release(archive) != ARACHNE_STATUS_OK && result == 0) {
        result = 7;
    }
    if (source != NULL && arachne_client_release(source) != ARACHNE_STATUS_OK && result == 0) {
        result = 8;
    }
    if (target != NULL && arachne_client_release(target) != ARACHNE_STATUS_OK && result == 0) {
        result = 9;
    }
    return result;
}
