import SwiftUI

/// Main menubar dropdown view
struct MenuBarView: View {
    @StateObject private var viewModel = NotificationViewModel()
    @StateObject private var ghosttyClient = GhosttyIPCClient.shared
    @State private var showSettings = false
    @State private var showClearConfirmation = false
    
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
        .frame(width: 360, height: 520)
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
            
            Button(action: { showSettings = true }) {
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
                    NotificationCard(notification: notification) {
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
            Button(action: { viewModel.showLogsView = true }) {
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
            
            Button(action: { NSApplication.shared.terminate(nil) }) {
                Text("Quit")
                    .font(.caption)
            }
            .buttonStyle(.plain)
            .foregroundColor(.secondary)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(Color(NSColor.controlBackgroundColor))
    }
}

// MARK: - Notification Card

struct NotificationCard: View {
    let notification: AppNotification
    let onDismiss: () -> Void
    
    @State private var isHovered = false
    
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
    }
    
    private var iconColor: Color {
        switch notification.type {
        case .system: return .gray
        case .taskCompletion: return .green
        case .aiOutput: return .purple
        case .devWorkflow: return .orange
        case .automation: return .blue
        }
    }
    
    private var relativeTime: String {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .abbreviated
        return formatter.localizedString(for: notification.timestamp, relativeTo: Date())
    }
}

// MARK: - Preview

#Preview {
    MenuBarView()
        .frame(width: 360, height: 520)
}
