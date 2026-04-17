import SwiftUI
import CoreImage.CIFilterBuiltins

/// Settings tabs
enum SettingsTab: String, CaseIterable {
    case general = "General"
    case notifications = "Notifications"
    case appearance = "Appearance"
    case logs = "Logs"
    case mobile = "Mobile Access"
    case filters = "Filters"
    case about = "About"
    
    var icon: String {
        switch self {
        case .general: return "gear"
        case .notifications: return "bell"
        case .appearance: return "paintbrush"
        case .logs: return "doc.text"
        case .mobile: return "iphone"
        case .filters: return "line.3.horizontal.decrease"
        case .about: return "info.circle"
        }
    }
}

/// Main settings view
struct SettingsView: View {
    @ObservedObject private var viewModel = SettingsViewModel.shared
    @State private var selectedTab: SettingsTab = .general
    
    var body: some View {
        NavigationSplitView {
            List(SettingsTab.allCases.filter { shouldShowTab($0) }, id: \.self, selection: $selectedTab) { tab in
                Label(tab.rawValue, systemImage: tab.icon)
                    .tag(tab)
            }
            .listStyle(.sidebar)
            .frame(minWidth: 150)
        } detail: {
            Group {
                switch selectedTab {
                case .general:
                    GeneralSettingsTab(viewModel: viewModel)
                case .notifications:
                    NotificationsSettingsTab(viewModel: viewModel)
                case .appearance:
                    AppearanceSettingsTab(viewModel: viewModel)
                case .logs:
                    LogsSettingsTab(viewModel: viewModel)
                case .mobile:
                    MobileAccessSettingsTab(viewModel: viewModel)
                case .filters:
                    FiltersSettingsTab(viewModel: viewModel)
                case .about:
                    AboutSettingsTab()
                }
            }
            .frame(minWidth: 400, minHeight: 350)
        }
        .frame(minWidth: 600, minHeight: 400)
    }
    
    private func shouldShowTab(_ tab: SettingsTab) -> Bool {
        if viewModel.settings.userPersona == .casual {
            return tab != .filters
        }
        return true
    }
}

// MARK: - General Settings Tab

struct GeneralSettingsTab: View {
    @ObservedObject var viewModel: SettingsViewModel
    @ObservedObject private var shellHookInstaller = ShellHookInstaller.shared
    @State private var showResetConfirmation = false
    @State private var importExportAlertMessage = ""
    @State private var showImportExportAlert = false
    
    var body: some View {
        Form {
            Section("User Persona") {
                Picker("Experience Level", selection: $viewModel.settings.userPersona) {
                    ForEach(UserPersona.allCases, id: \.self) { persona in
                        Text(persona.displayName).tag(persona)
                    }
                }
                .pickerStyle(.segmented)
                
                Text(viewModel.settings.userPersona.description)
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
            
            Section("Startup") {
                Toggle("Launch at login", isOn: $viewModel.settings.autoStartOnLogin)
                Toggle("Show in Dock", isOn: $viewModel.settings.showInDock)
            }
            
            Section("Behavior") {
                Toggle("Play notification sounds", isOn: $viewModel.settings.playNotificationSounds)
            }
            
            Section("Data") {
                HStack {
                    Text("Configuration directory")
                    Spacer()
                    Text(OnibiConfig.appDataDirectory)
                        .font(.caption)
                        .foregroundColor(.secondary)
                    Button("Reveal") {
                        NSWorkspace.shared.selectFile(nil, inFileViewerRootedAtPath: OnibiConfig.appDataDirectory)
                    }
                }
            }
            
            Section("Settings File") {
                HStack {
                    Button("Import...") {
                        importSettingsFromFile()
                    }
                    
                    Button("Export...") {
                        exportSettingsToFile()
                    }
                    
                    Spacer()
                }
                
                Text("Import replaces current settings. Export saves current settings as JSON.")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

            Section("Shell Integration") {
                HStack {
                    Text("Detected shell")
                    Spacer()
                    Text(targetShell.rawValue)
                        .font(.caption)
                        .foregroundColor(.secondary)
                }

                HStack {
                    Text("Hook status")
                    Spacer()
                    Text(targetShellStatusLabel)
                        .font(.caption)
                        .foregroundColor(targetShellStatusColor)
                }

                HStack {
                    Button("Install / Update Hooks") {
                        installOrUpdateShellHooks()
                    }
                    Button("Uninstall Hooks") {
                        uninstallShellHooks()
                    }
                    Button("Refresh Status") {
                        shellHookInstaller.checkAllShellStatuses()
                    }
                    Spacer()
                }

                Text("After changes, open a new terminal tab/window or run `exec zsh -l`.")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
        }
        .formStyle(.grouped)
        .navigationTitle("General")
        .toolbar {
            resetButton
        }
        .alert("Settings", isPresented: $showImportExportAlert) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(importExportAlertMessage)
        }
    }

    private var resetButton: some View {
        Button("Reset to Defaults") {
            showResetConfirmation = true
        }
        .alert("Reset Settings?", isPresented: $showResetConfirmation) {
            Button("Cancel", role: .cancel) {}
            Button("Reset", role: .destructive) {
                viewModel.resetToDefaults()
            }
        } message: {
            Text("This will restore all settings to their default values. This action cannot be undone.")
        }
    }

    private var targetShell: ShellHookInstaller.Shell {
        shellHookInstaller.detectedShell ?? .zsh
    }

    private var targetShellStatus: ShellHookInstaller.InstallationStatus {
        shellHookInstaller.shellStatuses[targetShell] ?? .notInstalled
    }

    private var targetShellStatusLabel: String {
        switch targetShellStatus {
        case .installed:
            return "Installed"
        case .notInstalled:
            return "Not Installed"
        case .error(let message):
            return "Error: \(message)"
        }
    }

    private var targetShellStatusColor: Color {
        switch targetShellStatus {
        case .installed:
            return .green
        case .notInstalled:
            return .secondary
        case .error:
            return .red
        }
    }
    
    private func importSettingsFromFile() {
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [.json]
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = false
        panel.message = "Choose a JSON settings file to import."
        
        guard panel.runModal() == .OK, let url = panel.url else { return }
        
        do {
            try viewModel.importSettings(from: url)
            importExportAlertMessage = "Imported settings from \(url.lastPathComponent)."
            showImportExportAlert = true
        } catch {
            importExportAlertMessage = "Failed to import settings: \(error.localizedDescription)"
            showImportExportAlert = true
        }
    }
    
    private func exportSettingsToFile() {
        let panel = NSSavePanel()
        panel.allowedContentTypes = [.json]
        panel.nameFieldStringValue = "onibi-settings.json"
        panel.canCreateDirectories = true
        panel.message = "Choose where to save your settings file."
        
        guard panel.runModal() == .OK, let url = panel.url else { return }
        
        do {
            try viewModel.exportSettings(to: url)
            importExportAlertMessage = "Exported settings to \(url.lastPathComponent)."
            showImportExportAlert = true
        } catch {
            importExportAlertMessage = "Failed to export settings: \(error.localizedDescription)"
            showImportExportAlert = true
        }
    }

    private func installOrUpdateShellHooks() {
        do {
            try shellHookInstaller.installOrUpdate(for: targetShell)
            shellHookInstaller.checkAllShellStatuses()
            importExportAlertMessage = "Installed shell hooks for \(targetShell.rawValue). Open a new terminal tab/window."
            showImportExportAlert = true
        } catch {
            importExportAlertMessage = "Failed to install shell hooks: \(error.localizedDescription)"
            showImportExportAlert = true
        }
    }

    private func uninstallShellHooks() {
        do {
            try shellHookInstaller.uninstall(from: targetShell)
            shellHookInstaller.checkAllShellStatuses()
            importExportAlertMessage = "Removed shell hooks from \(targetShell.rawValue)."
            showImportExportAlert = true
        } catch {
            importExportAlertMessage = "Failed to uninstall shell hooks: \(error.localizedDescription)"
            showImportExportAlert = true
        }
    }
}

// MARK: - Notifications Settings Tab

struct NotificationsSettingsTab: View {
    @ObservedObject var viewModel: SettingsViewModel
    @State private var showSoundPicker = false
    @State private var testingNotification = false
    
    var body: some View {
        Form {
            Section("Notification Types") {
                notificationToggle("System Notifications", 
                                   isOn: $viewModel.settings.notifications.enableSystem,
                                   type: .system)
                notificationToggle("Task Completions", 
                                   isOn: $viewModel.settings.notifications.enableTaskCompletion,
                                   type: .taskCompletion)
                notificationToggle("AI Output", 
                                   isOn: $viewModel.settings.notifications.enableAIOutput,
                                   type: .aiOutput)
                notificationToggle("Dev Workflows", 
                                   isOn: $viewModel.settings.notifications.enableDevWorkflow,
                                   type: .devWorkflow)
                notificationToggle("Automation", 
                                   isOn: $viewModel.settings.notifications.enableAutomation,
                                   type: .automation)
            }
            
            Section("Display") {
                Toggle("Show badge on menubar icon", isOn: $viewModel.settings.notifications.showBadge)
                
                Picker("Auto-expire after", selection: $viewModel.settings.notifications.autoExpireMinutes) {
                    Text("Never").tag(nil as Int?)
                    Text("5 minutes").tag(5 as Int?)
                    Text("15 minutes").tag(15 as Int?)
                    Text("30 minutes").tag(30 as Int?)
                    Text("1 hour").tag(60 as Int?)
                }
                
                if viewModel.settings.userPersona == .powerUser {
                    HStack {
                        Text("Throttle interval")
                        Spacer()
                        Slider(value: $viewModel.settings.notifications.throttleInterval, in: 0...5, step: 0.1) {
                            Text("Interval")
                        }
                        .frame(width: 150)
                        
                        Text(String(format: "%.1fs", viewModel.settings.notifications.throttleInterval))
                            .monospacedDigit()
                            .frame(width: 40, alignment: .trailing)
                            .foregroundColor(.secondary)
                    }
                    .help("Minimum time between notifications of the same type")
                }
            }
            
            Section("Sound") {
                Picker("Notification sound", selection: $viewModel.settings.notifications.soundName) {
                    Text("None").tag(nil as String?)
                    Text("Default").tag("default" as String?)
                    Text("Ping").tag("Ping" as String?)
                    Text("Pop").tag("Pop" as String?)
                    Text("Glass").tag("Glass" as String?)
                }
                
                Button("Preview Sound") {
                    previewSound()
                }
                .disabled(viewModel.settings.notifications.soundName == nil)
            }
            
            Section("Test") {
                Button(action: sendTestNotification) {
                    HStack {
                        if testingNotification {
                            ProgressView()
                                .scaleEffect(0.7)
                        }
                        Text("Send Test Notification")
                    }
                }
                .disabled(testingNotification)
            }
        }
        .formStyle(.grouped)
        .navigationTitle("Notifications")
    }
    
    private func notificationToggle(_ label: String, isOn: Binding<Bool>, type: NotificationType) -> some View {
        HStack {
            Image(systemName: type.iconName)
                .foregroundColor(colorForType(type))
                .frame(width: 20)
            Toggle(label, isOn: isOn)
        }
    }
    
    private func colorForType(_ type: NotificationType) -> Color {
        switch type {
        case .system: return .gray
        case .taskCompletion: return .green
        case .aiOutput: return .purple
        case .devWorkflow: return .orange
        case .automation: return .blue
        case .terminalNotification: return .teal
        }
    }
    
    private func previewSound() {
        if let soundName = viewModel.settings.notifications.soundName {
            NSSound(named: NSSound.Name(soundName))?.play()
        }
    }
    
    private func sendTestNotification() {
        testingNotification = true
        let notification = AppNotification(
            type: .system,
            title: "Test Notification",
            message: "This is a test notification from Onibi"
        )
        EventBus.shared.publish(notification)
        
        DispatchQueue.main.asyncAfter(deadline: .now() + 1) {
            testingNotification = false
        }
    }
}

// MARK: - Appearance Settings Tab

struct AppearanceSettingsTab: View {
    @ObservedObject var viewModel: SettingsViewModel
    
    private let iconOptions = ["terminal", "terminal.fill", "chevron.left.forwardslash.chevron.right"]
    
    var body: some View {
        Form {
            Section("Theme") {
                Toggle("Sync with Ghostty", isOn: $viewModel.settings.syncThemeWithGhostty)
                    .onChange(of: viewModel.settings.syncThemeWithGhostty) { _, newValue in
                        if newValue {
                            viewModel.syncGhosttyTheme()
                        }
                    }
                
                if !viewModel.settings.syncThemeWithGhostty {
                    Picker("Appearance", selection: $viewModel.settings.theme) {
                        ForEach(Theme.allCases, id: \.self) { theme in
                            Text(theme.displayName).tag(theme)
                        }
                    }
                    .pickerStyle(.segmented)
                    .onChange(of: viewModel.settings.theme) { _, newValue in
                        applyTheme(newValue)
                    }
                } else {
                    Text("Theme is synchronized with Ghostty configuration")
                        .font(.caption)
                        .foregroundColor(.secondary)
                    
                    if let custom = viewModel.settings.customTheme {
                        HStack {
                            Text("Background")
                            Spacer()
                            ColorPreview(hex: custom.backgroundColor)
                        }
                        HStack {
                            Text("Foreground")
                            Spacer()
                            ColorPreview(hex: custom.foregroundColor)
                        }
                    } else {
                        HStack {
                            Spacer()
                            ProgressView()
                                .scaleEffect(0.5)
                            Spacer()
                        }
                        .onAppear {
                            // Trigger sync if missing
                            viewModel.syncGhosttyTheme()
                        }
                    }
                }
            }
            
            Section("Menubar Icon") {
                HStack {
                    Text("Icon style")
                    Spacer()
                    HStack(spacing: 16) {
                        ForEach(iconOptions, id: \.self) { iconName in
                            iconOption(iconName, selected: viewModel.settings.menubarIconStyle == iconName)
                                .onTapGesture {
                                    viewModel.settings.menubarIconStyle = iconName
                                    // Notify MenuBarController to update icon
                                    NotificationCenter.default.post(name: .menubarIconChanged, object: iconName)
                                }
                        }
                    }
                }
            }
        }
        .formStyle(.grouped)
        .navigationTitle("Appearance")
        .onAppear {
            // Apply current theme on appear
            applyTheme(viewModel.settings.theme)
        }
    }
    
    private func iconOption(_ name: String, selected: Bool) -> some View {
        Image(systemName: name)
            .font(.title2)
            .padding(8)
            .background(selected ? Color.accentColor.opacity(0.2) : Color.clear)
            .cornerRadius(8)
            .overlay(
                RoundedRectangle(cornerRadius: 8)
                    .stroke(selected ? Color.accentColor : Color.clear, lineWidth: 2)
            )
    }
    
    private func applyTheme(_ theme: Theme) {
        switch theme {
        case .light:
            NSApp.appearance = NSAppearance(named: .aqua)
        case .dark:
            NSApp.appearance = NSAppearance(named: .darkAqua)
        case .system:
            NSApp.appearance = nil
        }
    }
}

// MARK: - Logs Settings Tab

struct LogsSettingsTab: View {
    @ObservedObject var viewModel: SettingsViewModel
    @State private var showClearConfirmation = false
    @State private var storageSize: String = "Calculating..."
    @State private var hasCalculatedStorage = false
    
    var body: some View {
        Form {
            Section("Performance Profile") {
                Picker("Log Volume", selection: $viewModel.settings.logVolumeProfile) {
                    ForEach(LogVolumeProfile.allCases, id: \.self) { profile in
                        Text(profile.displayName).tag(profile)
                    }
                }
                .pickerStyle(.menu)
                .onChange(of: viewModel.settings.logVolumeProfile) { _, newValue in
                    viewModel.settings.maxLogFileSizeMB = newValue.maxFileSizeMB
                }
                
                Text(viewModel.settings.logVolumeProfile.description)
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
            
            if viewModel.settings.userPersona == .powerUser {
                Section("Retention") {
                    Picker("Keep logs for", selection: $viewModel.settings.logRetentionDays) {
                        Text("1 day").tag(1)
                        Text("3 days").tag(3)
                        Text("7 days").tag(7)
                        Text("14 days").tag(14)
                        Text("30 days").tag(30)
                    }
                    
                    Slider(value: Binding(
                        get: { Double(viewModel.settings.logRetentionDays) },
                        set: { viewModel.settings.logRetentionDays = Int($0) }
                    ), in: 1...30, step: 1) {
                        Text("Retention: \(viewModel.settings.logRetentionDays) days")
                    }
                }
            }
            
            Section("Storage") {
                if viewModel.settings.userPersona == .powerUser {
                    HStack {
                        Text("Maximum storage")
                        Spacer()
                        Picker("", selection: $viewModel.settings.maxStorageMB) {
                            Text("50 MB").tag(50)
                            Text("100 MB").tag(100)
                            Text("250 MB").tag(250)
                            Text("500 MB").tag(500)
                        }
                        .frame(width: 120)
                    }
                }
                
                HStack {
                    Text("Current usage")
                    Spacer()
                    Text(storageSize)
                        .foregroundColor(.secondary)
                }
                .onAppear {
                    guard !hasCalculatedStorage else { return }
                    hasCalculatedStorage = true
                    calculateStorageSize()
                }
            }
            
            Section("Management") {
                Button("Clear All Logs", role: .destructive) {
                    showClearConfirmation = true
                }
                .alert("Clear All Logs?", isPresented: $showClearConfirmation) {
                    Button("Cancel", role: .cancel) {}
                    Button("Clear", role: .destructive) {
                        clearAllLogs()
                    }
                } message: {
                    Text("This action cannot be undone.")
                }
            }
        }
        .formStyle(.grouped)
        .navigationTitle("Logs")
    }
    
    private func calculateStorageSize() {
        DispatchQueue.global(qos: .utility).async {
            let logsPath = OnibiConfig.appDataDirectory + "/logs.json"
            do {
                let attrs = try FileManager.default.attributesOfItem(atPath: logsPath)
                let size = attrs[.size] as? Int64 ?? 0
                let sizeStr = ByteCountFormatter.string(fromByteCount: size, countStyle: .file)
                DispatchQueue.main.async {
                    storageSize = sizeStr
                }
            } catch {
                DiagnosticsStore.shared.record(
                    component: "SettingsView",
                    level: .debug,
                    message: "failed to calculate logs storage size",
                    metadata: [
                        "path": logsPath,
                        "reason": error.localizedDescription
                    ]
                )
                DispatchQueue.main.async {
                    storageSize = "0 bytes"
                }
            }
        }
    }
    
    private func clearAllLogs() {
        let logsPath = OnibiConfig.appDataDirectory + "/logs.json"
        do {
            if FileManager.default.fileExists(atPath: logsPath) {
                try FileManager.default.removeItem(atPath: logsPath)
            }
        } catch {
            ErrorReporter.shared.report(error, context: "SettingsView.clearAllLogs", severity: .warning)
            DiagnosticsStore.shared.record(
                component: "SettingsView",
                level: .warning,
                message: "failed to clear logs from settings",
                metadata: [
                    "path": logsPath,
                    "reason": error.localizedDescription
                ]
            )
        }
        hasCalculatedStorage = false
        calculateStorageSize()
    }
}

// MARK: - Mobile Access Settings Tab

struct MobileAccessSettingsTab: View {
    @ObservedObject var viewModel: SettingsViewModel
    @ObservedObject private var gatewayService = MobileGatewayService.shared
    @ObservedObject private var proxyListener = LocalSessionProxyListener.shared
    @State private var revealToken = false
    @State private var showPairingQR = false

    private var pairingEndpointForQR: String {
        // Prefer a tunnel (HTTPS, routable) > Tailscale > first LAN IP > loopback
        let tunnel = viewModel.settings.mobileAccessTunnelURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if !tunnel.isEmpty { return tunnel }
        if let tailnet = gatewayService.tailscaleStatus.baseURLString { return tailnet }
        if let lan = gatewayService.advertisedURLs.first(where: { !$0.contains("127.0.0.1") }) {
            return lan
        }
        return gatewayService.localURLString
    }

    private var pairingPayloadForQR: String {
        let payload: [String: String] = [
            "type": "onibi_pairing",
            "baseURL": pairingEndpointForQR,
            "token": gatewayService.pairingToken
        ]

        guard let data = try? JSONSerialization.data(withJSONObject: payload, options: [.sortedKeys]),
              let text = String(data: data, encoding: .utf8) else {
            return ""
        }
        return text
    }

    /// onibi://pair?base=<urlencoded>&token=<urlencoded> — the web app accepts this via clipboard/paste.
    private var pairingDeepLink: String {
        var components = URLComponents()
        components.scheme = "onibi"
        components.host = "pair"
        components.queryItems = [
            URLQueryItem(name: "base", value: pairingEndpointForQR),
            URLQueryItem(name: "token", value: gatewayService.pairingToken)
        ]
        return components.url?.absoluteString ?? ""
    }

    var body: some View {
        Form {
            Section("Gateway") {
                Toggle("Enable mobile gateway", isOn: $viewModel.settings.mobileAccessEnabled)

                Stepper(value: $viewModel.settings.mobileAccessPort, in: 1024...65535) {
                    HStack {
                        Text("Gateway port")
                        Spacer()
                        Text(String(viewModel.settings.mobileAccessPort))
                            .foregroundColor(.secondary)
                    }
                }

                HStack {
                    Text("Gateway status")
                    Spacer()
                    Label(
                        gatewayService.isRunning ? "Running" : "Stopped",
                        systemImage: gatewayService.isRunning ? "checkmark.circle.fill" : "xmark.circle.fill"
                    )
                    .foregroundColor(gatewayService.isRunning ? .green : .secondary)
                }

                HStack {
                    Text("Local endpoint")
                    Spacer()
                    Text(gatewayService.localURLString)
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .textSelection(.enabled)
                }
            }

            Section("Network Binding") {
                Picker("Bind mode", selection: $viewModel.settings.mobileAccessBindMode) {
                    ForEach(MobileAccessBindMode.allCases, id: \.self) { mode in
                        Text(mode.displayName).tag(mode)
                    }
                }
                .pickerStyle(.menu)

                Text(viewModel.settings.mobileAccessBindMode.description)
                    .font(.caption)
                    .foregroundColor(.secondary)

                if viewModel.settings.mobileAccessBindMode != .loopback {
                    if gatewayService.lanInterfaces.isEmpty {
                        Text("No non-loopback interfaces detected.")
                            .font(.caption)
                            .foregroundColor(.orange)
                    } else {
                        ForEach(gatewayService.lanInterfaces, id: \.ipv4) { iface in
                            interfaceRow(iface)
                        }
                    }

                    if !gatewayService.virtualInterfaces.isEmpty {
                        Toggle("Show VPN / virtual interfaces", isOn: $gatewayService.showVirtualInterfaces)
                            .font(.caption)
                        if gatewayService.showVirtualInterfaces {
                            ForEach(gatewayService.virtualInterfaces, id: \.ipv4) { iface in
                                interfaceRow(iface)
                            }
                        }
                    }
                    Button("Rescan interfaces") {
                        gatewayService.refreshNetworkInfo()
                    }
                }
            }

            Section("Tunnel (optional)") {
                HStack {
                    Text("Public URL")
                    Spacer()
                    TextField(
                        "https://your-tunnel.example.com",
                        text: $viewModel.settings.mobileAccessTunnelURL
                    )
                    .frame(maxWidth: 320)
                    .textFieldStyle(.roundedBorder)
                    .font(.caption)
                    .disableAutocorrection(true)
                }
                Text("Paste the HTTPS URL from cloudflared / ngrok / tailscale funnel. The gateway itself does not spawn tunnels — use your own command.")
                    .font(.caption)
                    .foregroundColor(.secondary)
                if !viewModel.settings.mobileAccessTunnelURL.isEmpty {
                    HStack {
                        Button("Copy Tunnel URL") {
                            NSPasteboard.general.clearContents()
                            NSPasteboard.general.setString(viewModel.settings.mobileAccessTunnelURL, forType: .string)
                        }
                        Button("Clear") {
                            viewModel.settings.mobileAccessTunnelURL = ""
                        }
                        Spacer()
                    }
                }
            }

            Section("Remote Control") {
                Toggle("Enable remote control", isOn: $viewModel.settings.remoteControlEnabled)

                HStack {
                    Text("Local proxy listener")
                    Spacer()
                    Label(
                        proxyListener.isRunning ? "Running" : "Stopped",
                        systemImage: proxyListener.isRunning ? "checkmark.circle.fill" : "xmark.circle.fill"
                    )
                    .foregroundColor(proxyListener.isRunning ? .green : .secondary)
                }

                HStack {
                    Text("Proxy socket path")
                    Spacer()
                    TextField("Socket path", text: $viewModel.settings.sessionProxySocketPath)
                        .frame(maxWidth: 280)
                        .textFieldStyle(.roundedBorder)
                        .font(.caption)
                }

                Stepper(value: $viewModel.settings.sessionOutputBufferLineLimit, in: 100...5000, step: 100) {
                    HStack {
                        Text("Buffer line limit")
                        Spacer()
                        Text(String(viewModel.settings.sessionOutputBufferLineLimit))
                            .foregroundColor(.secondary)
                    }
                }

                Stepper(value: $viewModel.settings.sessionOutputBufferByteLimit, in: 4 * 1024...1024 * 1024, step: 4 * 1024) {
                    HStack {
                        Text("Buffer byte limit")
                        Spacer()
                        Text(ByteCountFormatter.string(
                            fromByteCount: Int64(viewModel.settings.sessionOutputBufferByteLimit),
                            countStyle: .binary
                        ))
                        .foregroundColor(.secondary)
                    }
                }

                HStack {
                    Text("Proxy binary")
                    Spacer()
                    Text(SessionProxyCoordinator.shared.resolvedProxyBinaryPath() ?? "Unavailable")
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .textSelection(.enabled)
                        .lineLimit(1)
                }
            }

            Section("Pairing") {
                HStack {
                    Text("Pairing token")
                    Spacer()
                    if revealToken {
                        Text(gatewayService.pairingToken)
                            .font(.system(.caption, design: .monospaced))
                            .textSelection(.enabled)
                    } else {
                        Text(String(repeating: "•", count: min(gatewayService.pairingToken.count, 24)))
                            .font(.system(.caption, design: .monospaced))
                            .foregroundColor(.secondary)
                    }
                }

                HStack {
                    Button(revealToken ? "Hide Token" : "Reveal Token") {
                        revealToken.toggle()
                    }
                    Button(showPairingQR ? "Hide QR" : "Show QR") {
                        showPairingQR.toggle()
                    }
                    Button("Copy Token") {
                        gatewayService.copyPairingToken()
                    }
                    Button("Copy Pairing Payload") {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(pairingPayloadForQR, forType: .string)
                    }
                    Button("Copy Deep Link") {
                        NSPasteboard.general.clearContents()
                        NSPasteboard.general.setString(pairingDeepLink, forType: .string)
                    }
                    Button("Rotate Token") {
                        gatewayService.rotatePairingToken()
                        showPairingQR = false
                    }
                    Spacer()
                }

                if showPairingQR {
                    PairingQRCodeView(payload: pairingPayloadForQR)
                }
            }

            Section("Tailscale") {
                HStack {
                    Text("Serve status")
                    Spacer()
                    Text(gatewayService.tailscaleStatus.detail ?? "Unavailable")
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .multilineTextAlignment(.trailing)
                }

                if let baseURL = gatewayService.tailscaleStatus.baseURLString {
                    HStack {
                        Text("Tailnet URL")
                        Spacer()
                        Text(baseURL)
                            .font(.caption)
                            .foregroundColor(.secondary)
                            .textSelection(.enabled)
                    }
                }

                HStack {
                    Button("Refresh Status") {
                        Task { await gatewayService.refreshTailscaleStatus() }
                    }
                    Button("Enable Tailscale Serve") {
                        gatewayService.enableTailscaleServe()
                    }
                    .disabled(!gatewayService.isRunning)
                    Button("Copy URL") {
                        gatewayService.copyBaseURL()
                    }
                    .disabled(gatewayService.tailscaleStatus.baseURLString == nil)
                    Spacer()
                }
            }

            if let error = gatewayService.lastError, !error.isEmpty {
                Section("Last Error") {
                    Text(error)
                        .font(.caption)
                        .foregroundColor(.red)
                        .textSelection(.enabled)
                }
            }

            if let error = proxyListener.lastError, !error.isEmpty {
                Section("Remote Control Error") {
                    Text(error)
                        .font(.caption)
                        .foregroundColor(.red)
                        .textSelection(.enabled)
                }
            }
        }
        .formStyle(.grouped)
        .navigationTitle("Mobile Access")
        .task {
            await gatewayService.refreshTailscaleStatus()
        }
    }

    @ViewBuilder
    private func interfaceRow(_ iface: LocalNetworkInterface) -> some View {
        HStack {
            HStack(spacing: 4) {
                Text(iface.name)
                    .font(.caption)
                if iface.isPrimary {
                    Text("PRIMARY")
                        .font(.system(size: 9, weight: .semibold))
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(Color.green.opacity(0.2))
                        .foregroundColor(.green)
                        .cornerRadius(3)
                }
                if iface.isVirtual {
                    Text("VIRTUAL")
                        .font(.system(size: 9, weight: .semibold))
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(Color.secondary.opacity(0.15))
                        .foregroundColor(.secondary)
                        .cornerRadius(3)
                }
            }
            Spacer()
            let url = "http://\(iface.ipv4):\(viewModel.settings.mobileAccessPort)"
            Text(url)
                .font(.system(.caption, design: .monospaced))
                .textSelection(.enabled)
                .foregroundColor(.secondary)
            Button {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(url, forType: .string)
            } label: {
                Image(systemName: "doc.on.doc")
            }
            .buttonStyle(.borderless)
        }
    }
}

private struct PairingQRCodeView: View {
    private static let context = CIContext()

    let payload: String

    private var qrImage: NSImage? {
        guard !payload.isEmpty else {
            return nil
        }

        let filter = CIFilter.qrCodeGenerator()
        filter.setValue(Data(payload.utf8), forKey: "inputMessage")
        filter.setValue("M", forKey: "inputCorrectionLevel")

        guard let outputImage = filter.outputImage else {
            return nil
        }

        let scaledImage = outputImage.transformed(by: CGAffineTransform(scaleX: 6, y: 6))
        guard let cgImage = Self.context.createCGImage(scaledImage, from: scaledImage.extent) else {
            return nil
        }

        return NSImage(cgImage: cgImage, size: NSSize(width: scaledImage.extent.width, height: scaledImage.extent.height))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let qrImage {
                Image(nsImage: qrImage)
                    .interpolation(.none)
                    .resizable()
                    .frame(width: 168, height: 168)
                    .background(Color.white)
                    .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                    .overlay(
                        RoundedRectangle(cornerRadius: 8, style: .continuous)
                            .stroke(Color.secondary.opacity(0.35), lineWidth: 1)
                    )
            } else {
                Text("QR unavailable until a valid pairing payload is available.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            Text("Scan on your phone to import base URL + token payload.")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(.top, 2)
    }
}

// MARK: - Filters Settings Tab

struct FiltersSettingsTab: View {
    @ObservedObject var viewModel: SettingsViewModel
    @State private var showAddFilter = false
    @State private var editingFilter: FilterRule?
    
    var body: some View {
        VStack(spacing: 0) {
            if viewModel.settings.userPersona == .powerUser {
                VStack(alignment: .leading, spacing: 12) {
                    Text("Detection Sensitivity")
                        .font(.headline)
                    
                    HStack {
                        Slider(value: $viewModel.settings.detectionThreshold, in: 0.1...0.9, step: 0.1) {
                            Text("Threshold")
                        }
                        
                        Text(String(format: "%.1f", viewModel.settings.detectionThreshold))
                            .monospacedDigit()
                            .frame(width: 40, alignment: .trailing)
                            .foregroundColor(.secondary)
                    }
                    .help("Lower values detect more events but may increase false positives")
                    
                    Divider()
                }
                .padding()
            }
            
            // List of filters
            if viewModel.settings.filterRules.isEmpty {
                emptyState
            } else {
                List {
                    ForEach(viewModel.settings.filterRules) { rule in
                        FilterRuleRow(rule: rule) {
                            editingFilter = rule
                        } onToggle: { enabled in
                            if let index = viewModel.settings.filterRules.firstIndex(where: { $0.id == rule.id }) {
                                viewModel.settings.filterRules[index].isEnabled = enabled
                            }
                        } onDelete: {
                            viewModel.settings.filterRules.removeAll { $0.id == rule.id }
                        }
                    }
                }
            }
            
            Divider()
            
            // Add button
            HStack {
                Button(action: { showAddFilter = true }) {
                    Label("Add Filter Rule", systemImage: "plus")
                }
                Spacer()
            }
            .padding()
        }
        .navigationTitle("Filters")
        .sheet(isPresented: $showAddFilter) {
            FilterRuleEditor(rule: nil) { newRule in
                viewModel.settings.filterRules.append(newRule)
            }
        }
        .sheet(item: $editingFilter) { rule in
            FilterRuleEditor(rule: rule) { updatedRule in
                if let index = viewModel.settings.filterRules.firstIndex(where: { $0.id == rule.id }) {
                    viewModel.settings.filterRules[index] = updatedRule
                }
            }
        }
    }
    
    private var emptyState: some View {
        VStack(spacing: 16) {
            Image(systemName: "line.3.horizontal.decrease")
                .font(.system(size: 48))
                .foregroundColor(.secondary.opacity(0.5))
            Text("No Filter Rules")
                .font(.headline)
                .foregroundColor(.secondary)
            Text("Add rules to automatically filter or highlight log entries")
                .font(.caption)
                .foregroundColor(.secondary.opacity(0.7))
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

// MARK: - Filter Rule Row

struct FilterRuleRow: View {
    let rule: FilterRule
    let onEdit: () -> Void
    let onToggle: (Bool) -> Void
    let onDelete: () -> Void
    
    var body: some View {
        HStack {
            Toggle("", isOn: Binding(get: { rule.isEnabled }, set: onToggle))
                .labelsHidden()
            
            VStack(alignment: .leading, spacing: 2) {
                Text(rule.name)
                    .font(.subheadline)
                    .foregroundColor(rule.isEnabled ? .primary : .secondary)
                Text(rule.pattern)
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
            
            Spacer()
            
            Text(rule.action.rawValue.capitalized)
                .font(.caption)
                .padding(.horizontal, 8)
                .padding(.vertical, 2)
                .background(Color.secondary.opacity(0.2))
                .cornerRadius(4)
            
            Button(action: onEdit) {
                Image(systemName: "pencil")
            }
            .buttonStyle(.plain)
            
            Button(action: onDelete) {
                Image(systemName: "trash")
            }
            .buttonStyle(.plain)
            .foregroundColor(.red)
        }
        .padding(.vertical, 4)
    }
}

// MARK: - Filter Rule Editor

struct FilterRuleEditor: View {
    let rule: FilterRule?
    let onSave: (FilterRule) -> Void
    
    @Environment(\.dismiss) private var dismiss
    @State private var name: String = ""
    @State private var pattern: String = ""
    @State private var isRegex: Bool = false
    @State private var matchType: MatchType = .contains
    @State private var action: FilterAction = .highlight
    @State private var regexError: String?
    
    private var isValidPattern: Bool {
        guard isRegex && !pattern.isEmpty else { return true }
        do {
            _ = try NSRegularExpression(pattern: pattern)
            return true
        } catch {
            return false
        }
    }
    
    var body: some View {
        VStack(spacing: 20) {
            Text(rule == nil ? "Add Filter Rule" : "Edit Filter Rule")
                .font(.title2)
                .fontWeight(.semibold)
            
            Form {
                TextField("Name", text: $name)
                TextField("Pattern", text: $pattern)
                    .onChange(of: pattern) {
                        validateRegex()
                    }
                Toggle("Use regex", isOn: $isRegex)
                    .onChange(of: isRegex) {
                        validateRegex()
                    }
                
                if let error = regexError, isRegex {
                    Text(error)
                        .font(.caption)
                        .foregroundColor(.red)
                }
                
                Picker("Match type", selection: $matchType) {
                    ForEach(MatchType.allCases, id: \.self) { type in
                        Text(type.rawValue.capitalized).tag(type)
                    }
                }
                
                Picker("Action", selection: $action) {
                    ForEach(FilterAction.allCases, id: \.self) { action in
                        Text(action.rawValue.capitalized).tag(action)
                    }
                }
            }
            .formStyle(.grouped)
            
            HStack {
                Button("Cancel") { dismiss() }
                Spacer()
                Button("Save") {
                    let newRule = FilterRule(
                        id: rule?.id ?? UUID(),
                        name: name,
                        isEnabled: true,
                        pattern: pattern,
                        isRegex: isRegex,
                        matchType: matchType,
                        action: action
                    )
                    onSave(newRule)
                    dismiss()
                }
                .buttonStyle(.borderedProminent)
                .disabled(name.isEmpty || pattern.isEmpty || !isValidPattern)
            }
        }
        .padding()
        .frame(width: 400, height: 380)
        .onAppear {
            if let rule = rule {
                name = rule.name
                pattern = rule.pattern
                isRegex = rule.isRegex
                matchType = rule.matchType
                action = rule.action
            }
        }
    }
    
    private func validateRegex() {
        guard isRegex && !pattern.isEmpty else {
            regexError = nil
            return
        }
        
        do {
            _ = try NSRegularExpression(pattern: pattern)
            regexError = nil
        } catch let error as NSError {
            regexError = "Invalid regex: \(error.localizedDescription)"
        }
    }
}

// MARK: - About Settings Tab

struct AboutSettingsTab: View {
    @State private var ghosttyVersion: String = "Checking..."
    @State private var ghosttyInstalled: Bool = false
    @State private var ghosttyConfigPath: String = ""
    
    var body: some View {
        VStack(spacing: 24) {
            Image(systemName: "terminal.fill")
                .font(.system(size: 64))
                .foregroundColor(.accentColor)
            
            VStack(spacing: 8) {
                Text("Onibi")
                    .font(.title)
                    .fontWeight(.bold)
                
                Text("Version \(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0.0")")
                    .font(.subheadline)
                    .foregroundColor(.secondary)
            }
            
            Text("A macOS menubar application for displaying Ghostty terminal output, notifications, and task completions.")
                .multilineTextAlignment(.center)
                .foregroundColor(.secondary)
                .padding(.horizontal, 40)
            
            Divider()
                .frame(width: 200)
            
            // Ghostty diagnostics section
            VStack(alignment: .leading, spacing: 12) {
                HStack {
                    Text("Ghostty Status:")
                        .font(.headline)
                    Spacer()
                    Image(systemName: ghosttyInstalled ? "checkmark.circle.fill" : "xmark.circle.fill")
                        .foregroundColor(ghosttyInstalled ? .green : .red)
                }
                
                if ghosttyInstalled {
                    VStack(alignment: .leading, spacing: 6) {
                        HStack {
                            Text("Version:")
                                .foregroundColor(.secondary)
                            Spacer()
                            Text(ghosttyVersion)
                                .font(.caption)
                                .textSelection(.enabled)
                        }
                        
                        HStack {
                            Text("Config Path:")
                                .foregroundColor(.secondary)
                            Spacer()
                            Text(ghosttyConfigPath)
                                .font(.caption)
                                .textSelection(.enabled)
                                .lineLimit(1)
                        }
                        
                        Button(action: openGhosttyConfig) {
                            Label("Open Ghostty Config", systemImage: "doc.text")
                        }
                        .buttonStyle(.bordered)
                        .disabled(ghosttyConfigPath.isEmpty || !FileManager.default.fileExists(atPath: ghosttyConfigPath))
                    }
                    .padding(.leading, 8)
                } else {
                    Text("Ghostty not found in PATH")
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .padding(.leading, 8)
                }
            }
            .padding()
            .background(Color.secondary.opacity(0.1))
            .cornerRadius(8)
            .padding(.horizontal, 40)
            
            Divider()
                .frame(width: 200)
            
            VStack(spacing: 8) {
                Button(action: copyDiagnostics) {
                    Label("Copy Diagnostics", systemImage: "doc.on.clipboard")
                }
                .buttonStyle(.bordered)
                
                Link("GitHub Repository", destination: URL(string: "https://github.com/gongahkia/onibi")!)
                Link("Report an Issue", destination: URL(string: "https://github.com/gongahkia/onibi/issues")!)
            }
            .font(.subheadline)
            
            Spacer()
            
            Text("Made with ❤️ for the Ghostty community")
                .font(.caption)
                .foregroundColor(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
        .task {
            await loadGhosttyInfo()
        }
    }
    
    private func loadGhosttyInfo() async {
        let (installed, version) = await GhosttyCliService.shared.isGhosttyInstalled()
        ghosttyInstalled = installed
        ghosttyVersion = version ?? "Not available"
        
        // Find config path from known locations
        for path in OnibiConfig.configLocations {
            if FileManager.default.fileExists(atPath: path) {
                ghosttyConfigPath = path
                break
            }
        }
        
        if ghosttyConfigPath.isEmpty {
            ghosttyConfigPath = OnibiConfig.configLocations.first ?? "Not found"
        }
    }
    
    private func openGhosttyConfig() {
        guard !ghosttyConfigPath.isEmpty, FileManager.default.fileExists(atPath: ghosttyConfigPath) else {
            return
        }
        
        let url = URL(fileURLWithPath: ghosttyConfigPath)
        NSWorkspace.shared.open(url)
    }
    
    private func copyDiagnostics() {
        let appVersion = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "Unknown"
        let osVersion = ProcessInfo.processInfo.operatingSystemVersionString
        let settings = SettingsViewModel.shared.settings
        let logPath = settings.logFilePath
        
        let diagnostics = """
        Onibi Diagnostics
        =================
        
        App Version: \(appVersion)
        macOS Version: \(osVersion)
        Ghostty Installed: \(ghosttyInstalled ? "Yes" : "No")
        Ghostty Version: \(ghosttyVersion)
        Ghostty Config Path: \(ghosttyConfigPath)
        Log File Path: \(logPath)
        
        System Info:
        - Process ID: \(ProcessInfo.processInfo.processIdentifier)
        - Physical Memory: \(ByteCountFormatter.string(fromByteCount: Int64(ProcessInfo.processInfo.physicalMemory), countStyle: .memory))
        
        Settings:
        - User Persona: \(settings.userPersona.rawValue)
        - Auto Start: \(settings.autoStartOnLogin)
        - Sync Theme: \(settings.syncThemeWithGhostty)
        - Log Retention: \(settings.logRetentionDays) days
        """
        
        let pasteboard = NSPasteboard.general
        pasteboard.clearContents()
        pasteboard.setString(diagnostics, forType: .string)
    }
}

// MARK: - Preview

#Preview {
    SettingsView()
}
