import SwiftUI
import OnibiCore

struct ConnectionSetupView: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var viewModel: MobileMonitorViewModel

    @State private var baseURLString = ""
    @State private var token = ""
    @State private var localError: String?

    var body: some View {
        Form {
            Section("Mac Host") {
                TextField("https://your-mac.tailnet.ts.net", text: $baseURLString)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.URL)

                SecureField("Pairing token", text: $token)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }

            Section {
                Button("Save Connection") {
                    save()
                }
                .frame(maxWidth: .infinity, alignment: .center)
            }

            Section("How to Pair") {
                Text("1. Open Onibi on your Mac.")
                Text("2. Go to Settings > Mobile Access.")
                Text("3. Enable the gateway, copy the Tailnet URL, and copy the pairing token.")
                Text("4. Paste both values here to start monitoring.")
            }

            if let localError {
                Section("Error") {
                    Text(localError)
                        .foregroundStyle(.red)
                }
            }
        }
        .navigationTitle("Connect Host")
    }

    private func save() {
        do {
            try viewModel.saveConfiguration(
                baseURLString: baseURLString.trimmingCharacters(in: .whitespacesAndNewlines),
                token: token.trimmingCharacters(in: .whitespacesAndNewlines)
            )
            localError = nil
            dismiss()
        } catch {
            localError = error.localizedDescription
        }
    }
}
