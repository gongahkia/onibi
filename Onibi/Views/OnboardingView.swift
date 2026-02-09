import SwiftUI

/// Onboarding view for first-time users
struct OnboardingView: View {
    @Environment(\.dismiss) private var dismiss
    @StateObject private var settingsViewModel = SettingsViewModel.shared
    @State private var currentPage = 0
    
    private let pages: [OnboardingPage] = [
        // Page 0 is Persona Selection (custom view)
        
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
    
    // Total pages = 1 (Persona) + pages.count
    private var totalPages: Int { pages.count + 1 }
    
    var body: some View {
        VStack(spacing: 0) {
            // Page content
            TabView(selection: $currentPage) {
                // Step 0: Persona Selection
                PersonaSelectionView(selectedPersona: $settingsViewModel.settings.userPersona)
                    .tag(0)
                
                // Content Pages
                ForEach(pages.indices, id: \.self) { index in
                    OnboardingPageView(page: pages[index])
                        .tag(index + 1)
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
                    ForEach(0..<totalPages, id: \.self) { index in
                        Circle()
                            .fill(currentPage == index ? Color.accentColor : Color.secondary.opacity(0.3))
                            .frame(width: 8, height: 8)
                    }
                }
                
                Spacer()
                
                if currentPage < totalPages - 1 {
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
        settingsViewModel.settings.hasCompletedOnboarding = true
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
        let settings = SettingsViewModel.shared.settings
        let script = ShellHookInstaller.Shell.zsh.hookScript(logPath: settings.logFilePath)
        
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(script, forType: .string)
        showCopied = true
        
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
            showCopied = false
        }
    }
}

// MARK: - Persona Selection View

struct PersonaSelectionView: View {
    @Binding var selectedPersona: UserPersona
    
    var body: some View {
        VStack(spacing: 32) {
            VStack(spacing: 16) {
                Text("Choose Your Experience")
                    .font(.largeTitle)
                    .fontWeight(.bold)
                
                Text("Select the mode that best fits your workflow.")
                    .font(.body)
                    .foregroundColor(.secondary)
            }
            
            HStack(spacing: 20) {
                PersonaCard(
                    persona: .casual,
                    isSelected: selectedPersona == .casual,
                    icon: "cup.and.saucer.fill",
                    color: .green
                ) {
                    selectedPersona = .casual
                }
                
                PersonaCard(
                    persona: .powerUser,
                    isSelected: selectedPersona == .powerUser,
                    icon: "terminal.fill",
                    color: .purple
                ) {
                    selectedPersona = .powerUser
                }
            }
            .padding(.horizontal)
            
            Text(selectedPersona.description)
                .font(.headline)
                .foregroundColor(.secondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 40)
                .frame(height: 60)
        }
        .padding()
    }
}

struct PersonaCard: View {
    let persona: UserPersona
    let isSelected: Bool
    let icon: String
    let color: Color
    let action: () -> Void
    
    var body: some View {
        Button(action: action) {
            VStack(spacing: 16) {
                Image(systemName: icon)
                    .font(.system(size: 40))
                    .foregroundColor(isSelected ? .white : color)
                
                Text(persona.displayName)
                    .font(.headline)
                    .foregroundColor(isSelected ? .white : .primary)
            }
            .frame(width: 140, height: 140)
            .background(
                RoundedRectangle(cornerRadius: 16)
                    .fill(isSelected ? color : Color(NSColor.controlBackgroundColor))
                    .shadow(color: Color.black.opacity(0.1), radius: 5, x: 0, y: 2)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 16)
                    .stroke(isSelected ? color : Color.clear, lineWidth: 2)
            )
        }
        .buttonStyle(.plain)
        .scaleEffect(isSelected ? 1.05 : 1.0)
        .animation(.spring(response: 0.3, dampingFraction: 0.7), value: isSelected)
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
        
        // Use global monitor so hotkey works when app is not focused
        // Note: Requires Accessibility permissions for full functionality
        eventMonitor = NSEvent.addGlobalMonitorForEvents(matching: .keyDown) { [weak self] event in
            if event.modifierFlags.contains([.command, .shift]) && event.charactersIgnoringModifiers == "g" {
                self?.onToggle?()
            }
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
