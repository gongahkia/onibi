import SwiftUI

/// Settings tabs
enum SettingsTab: String, CaseIterable {
    case general = "General"
    case notifications = "Notifications"
    case appearance = "Appearance"
    case logs = "Logs"
    case filters = "Filters"
    case about = "About"
    
    var icon: String {
        switch self {
        case .general: return "gear"
        case .notifications: return "bell"
        case .appearance: return "paintbrush"
        case .logs: return "doc.text"
        case .filters: return "line.3.horizontal.decrease"
        case .about: return "info.circle"
        }
    }
}

/// Main settings view
struct SettingsView: View {
    @StateObject private var viewModel = SettingsViewModel()
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
        }
        .formStyle(.grouped)
        .navigationTitle("General")
        .toolbar {
            resetButton
        }
    }
    
    @State private var showResetConfirmation = false

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
    
    var body: some View {
        Form {
            Section("Theme") {
                Toggle("Sync with Ghostty", isOn: $viewModel.settings.syncThemeWithGhostty)
                    .onChange(of: viewModel.settings.syncThemeWithGhostty) { newValue in
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
                        iconOption("terminal", selected: true)
                        iconOption("terminal.fill", selected: false)
                        iconOption("chevron.left.forwardslash.chevron.right", selected: false)
                    }
                }
            }
        }
        .formStyle(.grouped)
        .navigationTitle("Appearance")
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
}

// MARK: - Logs Settings Tab

struct LogsSettingsTab: View {
    @ObservedObject var viewModel: SettingsViewModel
    @State private var showClearConfirmation = false
    @State private var storageSize: String = "Calculating..."
    
    var body: some View {
        Form {
            Section("Performance Profile") {
                Picker("Log Volume", selection: $viewModel.settings.logVolumeProfile) {
                    ForEach(LogVolumeProfile.allCases, id: \.self) { profile in
                        Text(profile.displayName).tag(profile)
                    }
                }
                .pickerStyle(.menu)
                .onChange(of: viewModel.settings.logVolumeProfile) { newValue in
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
        .onAppear { calculateStorageSize() }
    }
    
    private func calculateStorageSize() {
        DispatchQueue.global(qos: .utility).async {
            let logsPath = OnibiConfig.appDataDirectory + "/logs.json"
            if let attrs = try? FileManager.default.attributesOfItem(atPath: logsPath),
               let size = attrs[.size] as? Int64 {
                let sizeStr = ByteCountFormatter.string(fromByteCount: size, countStyle: .file)
                DispatchQueue.main.async {
                    storageSize = sizeStr
                }
            } else {
                DispatchQueue.main.async {
                    storageSize = "0 bytes"
                }
            }
        }
    }
    
    private func clearAllLogs() {
        let logsPath = OnibiConfig.appDataDirectory + "/logs.json"
        try? FileManager.default.removeItem(atPath: logsPath)
        calculateStorageSize()
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
    
    var body: some View {
        VStack(spacing: 20) {
            Text(rule == nil ? "Add Filter Rule" : "Edit Filter Rule")
                .font(.title2)
                .fontWeight(.semibold)
            
            Form {
                TextField("Name", text: $name)
                TextField("Pattern", text: $pattern)
                Toggle("Use regex", isOn: $isRegex)
                
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
                .disabled(name.isEmpty || pattern.isEmpty)
            }
        }
        .padding()
        .frame(width: 400, height: 350)
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
}

// MARK: - About Settings Tab

struct AboutSettingsTab: View {
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
            
            VStack(spacing: 8) {
                Link("GitHub Repository", destination: URL(string: "https://github.com")!)
                Link("Report an Issue", destination: URL(string: "https://github.com")!)
            }
            .font(.subheadline)
            
            Spacer()
            
            Text("Made with ❤️ for the Ghostty community")
                .font(.caption)
                .foregroundColor(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding()
    }
}

// MARK: - Preview

#Preview {
    SettingsView()
}
