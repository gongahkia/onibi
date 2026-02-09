import SwiftUI
import AppKit
import Combine

/// Manages the menubar status item and popover
final class MenuBarController: ObservableObject {
    var statusItem: NSStatusItem?
    private var popover: NSPopover?
    private var eventMonitor: Any?
    private var cancellables = Set<AnyCancellable>()
    
    @Published var notificationCount: Int = 0
    @Published var isAnimating: Bool = false
    
    private let eventBus = EventBus.shared
    
    init() {
        setupSubscriptions()
    }
    
    deinit {
        cleanup()
    }
    
    /// Set up the menubar status item
    func setup() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        
        if let button = statusItem?.button {
            updateIcon(hasNotifications: false)
            button.action = #selector(togglePopover(_:))
            button.target = self
        }
        
        setupPopover()
        setupEventMonitor()
    }
    
    /// Update the menubar icon based on state
    func updateIcon(hasNotifications: Bool) {
        guard let button = statusItem?.button else { return }
        
        let iconName = hasNotifications ? "terminal.fill" : "terminal"
        let image = NSImage(systemSymbolName: iconName, accessibilityDescription: "Ghostty Menubar")
        
        // Template image for proper light/dark mode adaptation
        image?.isTemplate = true
        button.image = image
        
        // Update badge
        if hasNotifications && notificationCount > 0 {
            button.title = " \(notificationCount > 99 ? "99+" : String(notificationCount))"
        } else {
            button.title = ""
        }
        
        // Sync badge with native notification center
        Task {
            await NotificationManager.shared.setBadgeCount(notificationCount)
        }
    }
    
    /// Start icon animation for new notifications
    func animateIcon() {
        guard !isAnimating else { return }
        isAnimating = true
        
        let icons = ["terminal", "terminal.fill"]
        var index = 0
        
        Timer.scheduledTimer(withTimeInterval: 0.3, repeats: true) { [weak self] timer in
            guard let self = self, self.isAnimating else {
                timer.invalidate()
                return
            }
            
            if let button = self.statusItem?.button {
                let image = NSImage(systemSymbolName: icons[index % 2], accessibilityDescription: nil)
                image?.isTemplate = true
                button.image = image
            }
            
            index += 1
            if index >= 6 { // 3 cycles
                self.isAnimating = false
                self.updateIcon(hasNotifications: self.notificationCount > 0)
                timer.invalidate()
            }
        }
    }
    
    /// Toggle the popover visibility
    @objc func togglePopover(_ sender: Any?) {
        guard let button = statusItem?.button, let popover = popover else { return }
        
        if popover.isShown {
            closePopover()
        } else {
            openPopover(relativeTo: button)
        }
    }
    
    /// Open the popover
    func openPopover(relativeTo button: NSStatusBarButton) {
        guard let popover = popover else { return }
        
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        
        // Focus the popover window
        popover.contentViewController?.view.window?.makeKey()
    }
    
    /// Close the popover
    func closePopover() {
        popover?.performClose(nil)
    }
    
    /// Clean up resources
    func cleanup() {
        if let monitor = eventMonitor {
            NSEvent.removeMonitor(monitor)
            eventMonitor = nil
        }
        statusItem = nil
    }
    
    // MARK: - Private
    
    private func setupPopover() {
        popover = NSPopover()
        popover?.contentSize = NSSize(width: 360, height: 520)
        popover?.behavior = .transient
        popover?.animates = true
        popover?.contentViewController = NSHostingController(rootView: MenuBarView())
    }
    
    private func setupEventMonitor() {
        // Close popover when clicking outside
        eventMonitor = NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown]) { [weak self] _ in
            if self?.popover?.isShown == true {
                self?.closePopover()
            }
        }
    }
    
    private func setupSubscriptions() {
        // Subscribe to notification updates
        eventBus.notificationPublisher
            .receive(on: DispatchQueue.main)
            .sink { [weak self] _ in
                guard let self = self else { return }
                self.notificationCount += 1
                self.updateIcon(hasNotifications: true)
                self.animateIcon()
            }
            .store(in: &cancellables)
        
        // Handle native notification tap to show popover
        NotificationCenter.default.publisher(for: .showNotificationInApp)
            .receive(on: DispatchQueue.main)
            .sink { [weak self] notification in
                guard let self = self, let button = self.statusItem?.button else { return }
                self.openPopover(relativeTo: button)
                
                // Optionally scroll to specific notification
                if let notificationId = notification.userInfo?["notificationId"] as? String {
                    NotificationCenter.default.post(
                        name: .scrollToNotification,
                        object: nil,
                        userInfo: ["notificationId": notificationId]
                    )
                }
            }
            .store(in: &cancellables)
    }
}

extension Notification.Name {
    static let scrollToNotification = Notification.Name("scrollToNotification")
}
