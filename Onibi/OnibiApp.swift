import SwiftUI
import Combine

@main
struct OnibiApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate
    
    @Environment(\.openWindow) private var openWindow
    
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
                .onReceive(NotificationCenter.default.publisher(for: .openOnboardingWindow)) { _ in
                    openWindow(id: "onboarding")
                }
        }
    }
}

// Extensions handled in respective files

class AppDelegate: NSObject, NSApplicationDelegate {
    var menuBarController: MenuBarController?
    private var logsWindow: NSWindow?
    
    func applicationDidFinishLaunching(_ notification: Notification) {
        // Set activation policy to accessory (menubar-only, no dock icon)
        // This is critical when running as a bare executable via `swift build`
        // since there's no Info.plist LSUIElement to configure this automatically
        NSApp.setActivationPolicy(.accessory)
        
        // Close the Settings window if it auto-opened (SwiftUI behavior for menubar apps)
        // We need a slight delay to let SwiftUI finish its initial setup
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.1) {
            for window in NSApp.windows {
                // Close only the Settings window (has "General" or similar title from Settings scene)
                // The Settings window typically has these characteristics
                if window.title == "General" || 
                   window.title == "Settings" ||
                   window.title == "Preferences" ||
                   (window.contentViewController != nil && 
                    String(describing: type(of: window.contentViewController!)).contains("Settings")) {
                    window.close()
                }
            }
        }
        
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
        
        // Set up notification observers for opening windows
        setupWindowNotificationObservers()
        
        // Check onboarding
        if !SettingsViewModel.shared.settings.hasCompletedOnboarding {
             DispatchQueue.main.asyncAfter(deadline: .now() + 1.0) {
                 NotificationCenter.default.post(name: .openOnboardingWindow, object: nil)
             }
        }
    }
    
    private func setupWindowNotificationObservers() {
        // Handle Settings window open request
        NotificationCenter.default.addObserver(
            forName: .openSettingsWindow,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            self?.openSettingsWindow()
        }
        
        // Handle Logs window open request
        NotificationCenter.default.addObserver(
            forName: .openLogsWindow,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            self?.openLogsWindow()
        }
        
        // Handle Onboarding window open request
        NotificationCenter.default.addObserver(
            forName: .openOnboardingWindow,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            self?.openOnboardingWindow()
        }
    }
    
    private var settingsWindow: NSWindow?
    
    private func openSettingsWindow() {
        // Check if settings window already exists
        if let existingWindow = settingsWindow, existingWindow.isVisible {
            existingWindow.makeKeyAndOrderFront(nil)
            NSApp.activate(ignoringOtherApps: true)
            return
        }
        
        // Create a new settings window manually since SwiftUI Settings scene
        // doesn't work reliably when running outside a bundled .app
        let settingsView = SettingsView()
        let hostingController = NSHostingController(rootView: settingsView)
        
        let window = NSWindow(contentViewController: hostingController)
        window.title = "Onibi Settings"
        window.setContentSize(NSSize(width: 550, height: 550))
        window.styleMask = [.titled, .closable]
        window.center()
        window.makeKeyAndOrderFront(nil)
        
        settingsWindow = window
        NSApp.activate(ignoringOtherApps: true)
    }
    
    private func openLogsWindow() {
        // Check if logs window already exists
        if let existingWindow = logsWindow, existingWindow.isVisible {
            existingWindow.makeKeyAndOrderFront(nil)
            NSApp.activate(ignoringOtherApps: true)
            return
        }
        
        // Create a new logs window
        let logsView = DetailedLogsView()
        let hostingController = NSHostingController(rootView: logsView)
        
        let window = NSWindow(contentViewController: hostingController)
        window.title = "Onibi Logs"
        window.setContentSize(NSSize(width: 600, height: 500))
        window.styleMask = [.titled, .closable, .resizable, .miniaturizable]
        window.center()
        window.makeKeyAndOrderFront(nil)
        
        logsWindow = window
        NSApp.activate(ignoringOtherApps: true)
    }
    
    private func openOnboardingWindow() {
        let onboardingView = OnboardingView()
        let hostingController = NSHostingController(rootView: onboardingView)
        
        let window = NSWindow(contentViewController: hostingController)
        window.title = "Welcome to Onibi"
        window.setContentSize(NSSize(width: 600, height: 400))
        window.styleMask = [.titled, .closable]
        window.center()
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
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
