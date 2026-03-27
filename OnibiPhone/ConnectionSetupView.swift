import SwiftUI
import OnibiCore

struct ConnectionSetupView: View {
    @Environment(\.dismiss) private var dismiss

    let viewModel: MobileMonitorViewModel

    @State private var baseURLString: String
    @State private var token: String
    @State private var localError: String?

    init(viewModel: MobileMonitorViewModel) {
        self.viewModel = viewModel
        let draft = viewModel.connectionDraft
        _baseURLString = State(initialValue: draft?.baseURLString ?? "")
        _token = State(initialValue: draft?.token ?? "")
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                ConnectionHeroCard(hasExistingConfiguration: viewModel.hasConfiguration)
                connectionFieldsCard
                pairingStepsCard

                if let localError {
                    ConnectionErrorCard(message: localError)
                }

                if viewModel.hasConfiguration {
                    DisconnectCard(disconnect: disconnect)
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 18)
            .padding(.bottom, 32)
        }
        .background(PhoneBackground())
        .navigationTitle("Connection")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Button("Close") {
                    dismiss()
                }
            }
        }
        .onChange(of: baseURLString) { _, _ in
            localError = nil
        }
        .onChange(of: token) { _, _ in
            localError = nil
        }
    }

    private var connectionFieldsCard: some View {
        PhoneCard {
            VStack(alignment: .leading, spacing: 18) {
                PhoneSectionHeader(
                    title: "Mobile Gateway",
                    subtitle: "Paste the Tailscale URL and pairing token from the Mac app."
                )

                VStack(alignment: .leading, spacing: 16) {
                    ConnectionField(
                        title: "Mac Host",
                        prompt: "https://your-mac.tailnet.ts.net",
                        symbolName: "desktopcomputer",
                        text: $baseURLString,
                        isSecure: false
                    )

                    ConnectionField(
                        title: "Pairing Token",
                        prompt: "Paste the token from macOS",
                        symbolName: "key.horizontal",
                        text: $token,
                        isSecure: true
                    )
                }

                Button(saveButtonTitle, action: save)
                    .buttonStyle(PhonePrimaryButtonStyle())
                    .disabled(!canSave)
            }
        }
    }

    private var pairingStepsCard: some View {
        PhoneCard {
            VStack(alignment: .leading, spacing: 18) {
                PhoneSectionHeader(
                    title: "How Pairing Works",
                    subtitle: "You only need to do this once per iPhone."
                )

                VStack(alignment: .leading, spacing: 14) {
                    PairingStepRow(
                        number: 1,
                        title: "Open the Mac app",
                        message: "Launch Onibi from the macOS menu bar."
                    )
                    PairingStepRow(
                        number: 2,
                        title: "Enable Mobile Access",
                        message: "Go to Settings > Mobile Access and switch the gateway on."
                    )
                    PairingStepRow(
                        number: 3,
                        title: "Copy both values",
                        message: "Grab the Tailnet URL and the generated pairing token."
                    )
                    PairingStepRow(
                        number: 4,
                        title: "Save on iPhone",
                        message: "Paste the values here and Onibi will begin syncing immediately."
                    )
                }
            }
        }
    }

    private var saveButtonTitle: String {
        viewModel.hasConfiguration ? "Update Connection" : "Save Connection"
    }

    private var canSave: Bool {
        !baseURLString.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty &&
        !token.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func save() {
        do {
            try viewModel.saveConfiguration(
                baseURLString: baseURLString.trimmingCharacters(in: .whitespacesAndNewlines),
                token: token.trimmingCharacters(in: .whitespacesAndNewlines)
            )
            viewModel.startPolling()
            dismiss()
        } catch {
            localError = error.localizedDescription
        }
    }

    private func disconnect() {
        viewModel.clearConfiguration()
        dismiss()
    }
}

private struct ConnectionHeroCard: View {
    let hasExistingConfiguration: Bool

    var body: some View {
        PhoneCard {
            VStack(alignment: .leading, spacing: 16) {
                PhoneBadge(
                    title: hasExistingConfiguration ? "Saved Host" : "New Pairing",
                    symbolName: "flame.fill",
                    tint: PhonePalette.ember
                )

                Text("Bring your Mac’s Ghostty sessions onto the phone without losing the privacy of a local Tailscale tunnel.")
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(PhonePalette.charcoal)
                    .fixedSize(horizontal: false, vertical: true)

                Text("Onibi only needs the host URL and pairing token generated by the macOS app.")
                    .font(.subheadline)
                    .foregroundStyle(PhonePalette.smoke)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}

private struct ConnectionField: View {
    let title: String
    let prompt: String
    let symbolName: String
    @Binding var text: String
    let isSecure: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Label(title, systemImage: symbolName)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(PhonePalette.charcoal)

            Group {
                if isSecure {
                    SecureField(prompt, text: $text)
                } else {
                    TextField(prompt, text: $text)
                        .keyboardType(.URL)
                }
            }
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .font(.body.monospaced())
            .foregroundStyle(PhonePalette.charcoal)
            .padding(.horizontal, 16)
            .padding(.vertical, 14)
            .background(Color.white.opacity(0.58), in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        }
    }
}

private struct PairingStepRow: View {
    let number: Int
    let title: String
    let message: String

    var body: some View {
        HStack(alignment: .top, spacing: 14) {
            Text(String(number))
                .font(.subheadline.weight(.bold))
                .foregroundStyle(.white)
                .frame(width: 28, height: 28)
                .background(PhonePalette.ember, in: Circle())

            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .font(.headline)
                    .foregroundStyle(PhonePalette.charcoal)

                Text(message)
                    .font(.subheadline)
                    .foregroundStyle(PhonePalette.smoke)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}

private struct ConnectionErrorCard: View {
    let message: String

    var body: some View {
        PhoneCard {
            VStack(alignment: .leading, spacing: 10) {
                Label("Couldn’t save connection", systemImage: "exclamationmark.triangle.fill")
                    .font(.headline)
                    .foregroundStyle(PhonePalette.rose)

                Text(message)
                    .font(.subheadline)
                    .foregroundStyle(PhonePalette.smoke)
            }
        }
    }
}

private struct DisconnectCard: View {
    let disconnect: () -> Void

    var body: some View {
        PhoneCard {
            VStack(alignment: .leading, spacing: 16) {
                Text("Need to reset this phone?")
                    .font(.headline)
                    .foregroundStyle(PhonePalette.charcoal)

                Text("Removing the saved connection clears the host URL, token, and cached session data from this device.")
                    .font(.subheadline)
                    .foregroundStyle(PhonePalette.smoke)
                    .fixedSize(horizontal: false, vertical: true)

                Button(role: .destructive, action: disconnect) {
                    Text("Disconnect This iPhone")
                }
                .buttonStyle(PhoneSecondaryButtonStyle())
            }
        }
    }
}

#Preview("Connection Setup") {
    NavigationStack {
        ConnectionSetupView(viewModel: MobileMonitorViewModel())
    }
}
