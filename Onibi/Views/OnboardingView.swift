import SwiftUI

/// Onboarding view for first-time users
struct OnboardingView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var currentPage = 0
    
    private let pages: [OnboardingPage] = [
        OnboardingPage(
            icon: "terminal.fill",
            title: "Welcome to Onibi",
            description: "Monitor your terminal output, get notifications for important events, and stay on top of your development workflow.",
            color: .accentColor
        ),
        OnboardingPage(
            icon: "bell.fill",
            title: "Smart Notifications",
            description: "Get notified when AI assistants respond, tasks complete, builds finish, or custom patterns are detected in your terminal.",
            color: .purple
        ),
        OnboardingPage(
            icon: "doc.text.magnifyingglass",
            title: "Detailed Logs",
            description: "Search and filter through your command history. Export logs in multiple formats for sharing or archiving.",
            color: .orange
        ),
        OnboardingPage(
            icon: "gearshape.fill",
            title: "Customize Everything",
            description: "Configure notification types, create custom filters, and personalize the app to match your workflow.",
            color: .green
        ),
        OnboardingPage(
            icon: "terminal",
            title: "Set Up Integration",
            description: "Add the shell hook to your terminal to start receiving notifications. Copy the script to your shell configuration file.",
            color: .blue,
            showSetupButton: true
        )
    ]
    
    var body: some View {
        VStack(spacing: 0) {
            // Page content
            TabView(selection: $currentPage) {
                ForEach(pages.indices, id: \.self) { index in
                    OnboardingPageView(page: pages[index])
                        .tag(index)
                }
            }
            .tabViewStyle(.automatic)
            
            // Navigation
            HStack {
                if currentPage > 0 {
                    Button("Back") {
                        withAnimation { currentPage -= 1 }
                    }
                    .buttonStyle(.plain)
                    .foregroundColor(.secondary)
                }
                
                Spacer()
                
                // Page indicators
                HStack(spacing: 6) {
                    ForEach(pages.indices, id: \.self) { index in
                        Circle()
                            .fill(currentPage == index ? Color.accentColor : Color.secondary.opacity(0.3))
                            .frame(width: 8, height: 8)
                    }
                }
                
                Spacer()
                
                if currentPage < pages.count - 1 {
                    Button("Next") {
                        withAnimation { currentPage += 1 }
                    }
                    .buttonStyle(.borderedProminent)
                } else {
                    Button("Get Started") {
                        finishOnboarding()
                    }
                    .buttonStyle(.borderedProminent)
                }
            }
            .padding()
        }
        .frame(width: 500, height: 450)
    }
    
    private func finishOnboarding() {
        UserDefaults.standard.set(true, forKey: "hasCompletedOnboarding")
        dismiss()
    }
}

struct OnboardingPage {
    let icon: String
    let title: String
    let description: String
    let color: Color
    var showSetupButton: Bool = false
}

struct OnboardingPageView: View {
    let page: OnboardingPage
    @State private var showCopied = false
    
    var body: some View {
        VStack(spacing: 24) {
            Spacer()
            
            Image(systemName: page.icon)
                .font(.system(size: 64))
                .foregroundColor(page.color)
            
            VStack(spacing: 12) {
                Text(page.title)
                    .font(.title)
                    .fontWeight(.bold)
                
                Text(page.description)
                    .font(.body)
                    .foregroundColor(.secondary)
                    .multilineTextAlignment(.center)
                    .padding(.horizontal, 40)
            }
            
            if page.showSetupButton {
                VStack(spacing: 12) {
                    Button(action: copyShellHook) {
                        HStack {
                            Image(systemName: showCopied ? "checkmark" : "doc.on.doc")
                            Text(showCopied ? "Copied!" : "Copy Shell Hook Script")
                        }
                    }
                    .buttonStyle(.bordered)
                    
                    Text("Add to ~/.zshrc or ~/.bashrc")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            }
            
            Spacer()
        }
    }
    
    private func copyShellHook() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(OnibiConfig.zshHookScript, forType: .string)
        showCopied = true
        
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
            showCopied = false
        }
    }
}

// MARK: - Tutorial Tooltip

struct TooltipModifier: ViewModifier {
    let tip: String
    let isShowing: Bool
    
    func body(content: Content) -> some View {
        content
            .overlay(alignment: .bottom) {
                if isShowing {
                    Text(tip)
                        .font(.caption)
                        .padding(8)
                        .background(Color(NSColor.controlBackgroundColor))
                        .cornerRadius(8)
                        .shadow(radius: 4)
                        .offset(y: 30)
                        .transition(.opacity.combined(with: .move(edge: .top)))
                }
            }
    }
}

extension View {
    func tooltip(_ tip: String, isShowing: Bool) -> some View {
        modifier(TooltipModifier(tip: tip, isShowing: isShowing))
    }
}

// MARK: - Keyboard Shortcuts

struct KeyboardShortcuts {
    static let search = KeyEquivalent("k")
    static let settings = KeyEquivalent(",")
    static let refresh = KeyEquivalent("r")
    static let clearAll = KeyEquivalent("l")
    static let quit = KeyEquivalent("q")
}

// MARK: - Context Menu Items

struct NotificationContextMenu: View {
    let notification: AppNotification
    let onDismiss: () -> Void
    let onCopy: () -> Void
    let onSnooze: () -> Void
    
    var body: some View {
        Button("Copy Message") { onCopy() }
        Button("Snooze (5 min)") { onSnooze() }
        Divider()
        Button("Dismiss", role: .destructive) { onDismiss() }
    }
}

// MARK: - Hotkey Manager

final class HotkeyManager {
    static let shared = HotkeyManager()
    
    private var eventMonitor: Any?
    private var onToggle: (() -> Void)?
    
    private init() {}
    
    /// Register global hotkey (Cmd+Shift+G)
    func register(onToggle: @escaping () -> Void) {
        self.onToggle = onToggle
        
        // Note: In a real app, use MASShortcut or similar for proper global hotkeys
        // This is a simplified local approach
        eventMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            if event.modifierFlags.contains([.command, .shift]) && event.charactersIgnoringModifiers == "g" {
                self?.onToggle?()
                return nil
            }
            return event
        }
    }
    
    func unregister() {
        if let monitor = eventMonitor {
            NSEvent.removeMonitor(monitor)
            eventMonitor = nil
        }
    }
}

// MARK: - Preview

#Preview {
    OnboardingView()
}
