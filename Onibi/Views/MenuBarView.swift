import SwiftUI

extension Notification.Name {
    static let openLogsWindow = Notification.Name("openLogsWindow")
    static let openSettingsWindow = Notification.Name("openSettingsWindow")
}

/// Main menubar dropdown view
struct MenuBarView: View {
    @StateObject private var viewModel = NotificationViewModel()
    @ObservedObject private var settingsVM = SettingsViewModel.shared
    @StateObject private var ghosttyClient = GhosttyIPCClient.shared
    @State private var showSettings = false
    @State private var showClearConfirmation = false
    @State private var showQuitConfirmation = false
    
    var body: some View {
        VStack(spacing: 0) {
            // Ghostty status banner
            if !ghosttyClient.isGhosttyRunning {
                ghosttyNotRunningBanner
            }
            
            // Header
            headerSection
            
            Divider()
            
            // Notifications content
            if viewModel.notifications.isEmpty {
                emptyStateView
            } else {
                notificationsList
            }
            
            Divider()
            
            // Footer
            footerSection
        }
        .frame(width: Constants.Popover.width, height: Constants.Popover.height)
        .background(Color(NSColor.windowBackgroundColor))
    }
    
    // MARK: - Ghostty Status Banner
    
    private var ghosttyNotRunningBanner: some View {
        HStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundColor(.orange)
            
            Text("Ghostty Not Running")
                .font(.caption)
                .foregroundColor(.secondary)
            
            Spacer()
            
            Button("Launch") {
                ghosttyClient.launchGhostty()
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(Color.orange.opacity(0.1))
    }
    
    // MARK: - Header
    
    private var headerSection: some View {
        HStack(spacing: 12) {
            Image(systemName: "terminal.fill")
                .font(.title3)
                .foregroundColor(.accentColor)
            
            Text("Onibi")
                .font(.headline)
                .fontWeight(.semibold)
            
            Spacer()
            
            if !viewModel.notifications.isEmpty {
                Button(action: { showClearConfirmation = true }) {
                    Image(systemName: "trash")
                        .font(.system(size: 14))
                        .foregroundColor(.secondary)
                }
                .buttonStyle(.plain)
                .help("Clear all notifications")
                .alert("Clear All Notifications?", isPresented: $showClearConfirmation) {
                    Button("Cancel", role: .cancel) {}
                    Button("Clear All", role: .destructive) {
                        viewModel.clearAll()
                    }
                }
            }
            
            Button(action: {
                NSApp.activate(ignoringOtherApps: true)
                NotificationCenter.default.post(name: .openSettingsWindow, object: nil)
            }) {
                Image(systemName: "gearshape")
                    .font(.system(size: 14))
                    .foregroundColor(.secondary)
            }
            .buttonStyle(.plain)
            .help("Settings")
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .background(Color(NSColor.controlBackgroundColor))
    }
    
    // MARK: - Empty State
    
    private var emptyStateView: some View {
        VStack(spacing: 16) {
            Spacer()
            
            Image(systemName: "bell.slash")
                .font(.system(size: 48))
                .foregroundColor(.secondary.opacity(0.5))
            
            VStack(spacing: 4) {
                Text("No Notifications")
                    .font(.headline)
                    .foregroundColor(.secondary)
                
                Text("Terminal events will appear here")
                    .font(.caption)
                    .foregroundColor(.secondary.opacity(0.7))
            }
            
            Spacer()
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
    
    // MARK: - Notifications List
    
    private var notificationsList: some View {
        ScrollView {
            LazyVStack(spacing: 1) {
                ForEach(viewModel.notifications) { notification in
                    NotificationCard(
                        notification: notification,
                        userPersona: settingsVM.settings.userPersona
                    ) {
                        viewModel.dismiss(notification)
                    }
                }
            }
            .padding(.vertical, 4)
        }
        .background(Color(NSColor.controlBackgroundColor).opacity(0.5))
    }
    
    // MARK: - Footer
    
    private var footerSection: some View {
        HStack {
            Button(action: {
                // Open Logs Window
                NSApp.activate(ignoringOtherApps: true)
                // We use a URL scheme or notification to trigger openWindow from AppDelegate/App
                NotificationCenter.default.post(name: .openLogsWindow, object: nil)
            }) {
                HStack(spacing: 4) {
                    Image(systemName: "list.bullet.rectangle")
                        .font(.system(size: 12))
                    Text("View Logs")
                        .font(.caption)
                }
            }
            .buttonStyle(.plain)
            .foregroundColor(.accentColor)
            
            Spacer()
            
            Text("\(viewModel.notifications.count) notifications")
                .font(.caption2)
                .foregroundColor(.secondary)
            
            Spacer()
            
            Button("Quit") {
                showQuitConfirmation = true
            }
            .buttonStyle(.plain)
            .foregroundColor(.secondary)
            .alert("Quit Onibi?", isPresented: $showQuitConfirmation) {
                Button("Cancel", role: .cancel) {}
                Button("Quit", role: .destructive) {
                    NSApplication.shared.terminate(nil)
                }
            } message: {
                Text("Background monitoring will stop.")
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(Color(NSColor.controlBackgroundColor))
    }
}

// MARK: - Notification Card

struct NotificationCard: View {
    let notification: AppNotification
    let userPersona: UserPersona
    let onDismiss: () -> Void
    
    @State private var isHovered = false
    @State private var currentTime = Date()
    
    let timer = Timer.publish(every: 60, on: .main, in: .common).autoconnect()
    
    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            // Type icon
            Image(systemName: notification.type.iconName)
                .font(.system(size: 16))
                .foregroundColor(iconColor)
                .frame(width: 24, height: 24)
            
            VStack(alignment: .leading, spacing: 4) {
                HStack {
                    Text(notification.title)
                        .font(.subheadline)
                        .fontWeight(.medium)
                        .lineLimit(1)
                    
                    Spacer()
                    
                    Text(relativeTime)
                        .font(.caption2)
                        .foregroundColor(.secondary)
                }
                
                Text(notification.message)
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .lineLimit(2)
                
                if userPersona == .powerUser, let event = notification.sourceEvent {
                    HStack(spacing: 8) {
                        if let cmd = event.command {
                            Label(cmd, systemImage: "terminal")
                        }
                        if let session = event.sessionId {
                            Label(session, systemImage: "macwindow")
                        }
                    }
                    .font(.caption2)
                    .foregroundColor(.secondary.opacity(0.8))
                    .padding(.top, 2)
                }
            }
            
            if isHovered {
                Button(action: onDismiss) {
                    Image(systemName: "xmark")
                        .font(.system(size: 10))
                        .foregroundColor(.secondary)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .background(
            RoundedRectangle(cornerRadius: 8)
                .fill(isHovered ? Color(NSColor.selectedContentBackgroundColor).opacity(0.1) : Color.clear)
        )
        .onHover { hovering in
            withAnimation(.easeInOut(duration: 0.15)) {
                isHovered = hovering
            }
        }
        .onReceive(timer) { _ in
            currentTime = Date()
        }
    }
    
    private var iconColor: Color {
        switch notification.type {
        case .system: return .gray
        case .taskCompletion: return .green
        case .aiOutput: return .purple
        case .devWorkflow: return .orange
        case .automation: return .blue
        case .terminalNotification: return .red
        }
    }
    
    private var relativeTime: String {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .abbreviated
        return formatter.localizedString(for: notification.timestamp, relativeTo: currentTime)
    }
}

// MARK: - Preview

#Preview {
    MenuBarView()
        .frame(width: 360, height: 520)
}
