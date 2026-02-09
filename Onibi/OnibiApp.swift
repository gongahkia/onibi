import SwiftUI
import Combine

@main
struct OnibiApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate
    
    @Environment(\.openWindow) private var openWindow
    @Environment(\.openSettings) private var openSettings
    
    var body: some Scene {
        Settings {
            SettingsView()
        }
        
        WindowGroup("Logs", id: "logs") {
            DetailedLogsView()
                .onReceive(NotificationCenter.default.publisher(for: .openLogsWindow)) { _ in
                    // Already handled by openWindow, but here for completeness within the view if needed
                }
        }
        .windowStyle(.hiddenTitleBar)
        
        WindowGroup("Welcome", id: "onboarding") {
            OnboardingView()
                .frame(minWidth: 600, minHeight: 400)
        }
        .windowStyle(.hiddenTitleBar)
        .windowResizability(.contentSize)
        // Invisible window group to handle commands
        WindowGroup(id: "command-handler") {
            EmptyView()
                .onReceive(NotificationCenter.default.publisher(for: .openLogsWindow)) { _ in
                    openWindow(id: "logs")
                }
                .onReceive(NotificationCenter.default.publisher(for: .openSettingsWindow)) { _ in
                    DispatchQueue.main.async {
                         NSApp.sendAction(Selector(("showPreferencesWindow:")), to: nil, from: nil)
                    }
                }
        }
    }
}

// Extensions handled in respective files


class AppDelegate: NSObject, NSApplicationDelegate {
    var menuBarController: MenuBarController?
    
    func applicationDidFinishLaunching(_ notification: Notification) {
        // Ensure app data directory exists
        try? OnibiConfig.ensureDirectoryExists()
        
        // Set up menubar controller
        menuBarController = MenuBarController()
        menuBarController?.setup()
        
        // Register URL handler
        NSAppleEventManager.shared().setEventHandler(
            self,
            andSelector: #selector(handleURL(_:withReplyEvent:)),
            forEventClass: AEEventClass(kInternetEventClass),
            andEventID: AEEventID(kAEGetURL)
        )
        
        // Check onboarding
        if !SettingsViewModel.shared.settings.hasCompletedOnboarding {
             // We need to trigger openWindow(id: "onboarding") but we don't have access to Environment here.
             // We can use a notification observed by the command-handler window group.
             DispatchQueue.main.asyncAfter(deadline: .now() + 1.0) {
                 NotificationCenter.default.post(name: .openOnboardingWindow, object: nil)
             }
        }
    }
    
    func applicationWillTerminate(_ notification: Notification) {
        menuBarController?.cleanup()
    }
    
    /// Handle onibi:// URLs
    /// Format: onibi://log/{logId} or onibi://notification/{notificationId}
    @objc func handleURL(_ event: NSAppleEventDescriptor, withReplyEvent reply: NSAppleEventDescriptor) {
        guard let urlString = event.paramDescriptor(forKeyword: keyDirectObject)?.stringValue,
              let url = URL(string: urlString) else { return }
        
        handleDeepLink(url)
    }
    
    private func handleDeepLink(_ url: URL) {
        guard url.scheme == "onibi" else { return }
        
        let host = url.host
        let pathComponents = url.pathComponents.filter { $0 != "/" }
        
        switch host {
        case "log":
            // onibi://log/{logId}
            if let logId = pathComponents.first {
                NotificationCenter.default.post(
                    name: .openLogEntry,
                    object: nil,
                    userInfo: ["logId": logId]
                )
            }
            
        case "notification":
            // onibi://notification/{notificationId}
            if let notificationId = pathComponents.first {
                NotificationCenter.default.post(
                    name: .showNotificationInApp,
                    object: nil,
                    userInfo: ["notificationId": notificationId]
                )
            }
            
        case "open":
            // onibi://open - just open the popover
            if let button = menuBarController?.statusItem?.button {
                menuBarController?.openPopover(relativeTo: button)
            }
            
        default:
            break
        }
    }
}

extension Notification.Name {
    static let openLogEntry = Notification.Name("openLogEntry")
    static let openOnboardingWindow = Notification.Name("openOnboardingWindow")
}

