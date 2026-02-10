import SwiftUI
import AppKit
import Combine

/// Manages the menubar status item and popover
final class MenuBarController: ObservableObject {
    var statusItem: NSStatusItem?
    private var popover: NSPopover?
    private var eventMonitor: Any?
    private var cancellables = Set<AnyCancellable>()
    private var animationTimer: Timer?
    
    @Published var notificationCount: Int = 0
    @Published var isAnimating: Bool = false
    @Published var menuBarState: MenuBarState = .idle
    
    enum MenuBarState {
        case idle
        case ghosttyNotRunning
        case monitoringActive
        case newNotifications
        
        var iconName: String {
            switch self {
            case .idle:
                return "terminal"
            case .ghosttyNotRunning:
                return "terminal"
            case .monitoringActive:
                return "terminal.fill"
            case .newNotifications:
                return "terminal.fill"
            }
        }
        
        var iconColor: NSColor? {
            switch self {
            case .idle:
                return .secondaryLabelColor
            case .ghosttyNotRunning:
                return .secondaryLabelColor
            case .monitoringActive:
                return nil // Use template for system colors
            case .newNotifications:
                return nil // Use template for system colors
            }
        }
    }
    
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
            
            // Ensure the button can receive clicks even in accessory mode
            button.sendAction(on: [.leftMouseUp, .rightMouseUp])
        }
        
        setupPopover()
        setupEventMonitor()
    }
    
    /// Update the menubar icon based on state
    func updateIcon(hasNotifications: Bool) {
        guard statusItem?.button != nil else { return }

        // Determine state
        if hasNotifications && notificationCount > 0 {
            menuBarState = .newNotifications
        } else {
            // Check if Ghostty is running/monitoring is active
            Task {
                let (installed, _) = await GhosttyCliService.shared.isGhosttyInstalled()
                DispatchQueue.main.async {
                    self.menuBarState = installed ? .monitoringActive : .ghosttyNotRunning
                    self.updateIconAppearance()
                }
            }
            return
        }
        
        updateIconAppearance()
    }
    
    private func updateIconAppearance() {
        guard let button = statusItem?.button else { return }
        
        // Use the user's preferred icon style from settings
        let iconName = SettingsViewModel.shared.settings.menubarIconStyle
        let image = NSImage(systemSymbolName: iconName, accessibilityDescription: "Onibi")
        
        // Template image for proper light/dark mode adaptation
        image?.isTemplate = menuBarState.iconColor == nil
        button.image = image
        
        // Set custom tint for ghostty not running state
        if let color = menuBarState.iconColor {
            button.contentTintColor = color
        } else {
            button.contentTintColor = nil
        }
        
        // Update badge
        if menuBarState == .newNotifications && notificationCount > 0 {
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
        
        let selectedIcon = SettingsViewModel.shared.settings.menubarIconStyle
        var index = 0
        
        animationTimer?.invalidate()
        animationTimer = Timer.scheduledTimer(withTimeInterval: 0.3, repeats: true) { [weak self] timer in
            guard let self = self, self.isAnimating else {
                timer.invalidate()
                return
            }
            if let button = self.statusItem?.button {
                if index % 2 == 0 {
                    // Show icon
                    let image = NSImage(systemSymbolName: selectedIcon, accessibilityDescription: nil)
                    image?.isTemplate = true
                    button.image = image
                } else {
                    // Hide icon (blink off)
                    button.image = nil
                }
            }
            index += 1
            if index >= 6 { // 3 cycles
                self.isAnimating = false
                self.animationTimer = nil
                self.updateIconAppearance()
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
        
        // Activate the app to ensure popover can receive focus
        NSApp.activate(ignoringOtherApps: true)
        
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
        animationTimer?.invalidate()
        animationTimer = nil
        if let monitor = eventMonitor {
            NSEvent.removeMonitor(monitor)
            eventMonitor = nil
        }
        statusItem = nil
    }
    
    // MARK: - Private
    
    private func setupPopover() {
        popover = NSPopover()
        popover?.contentSize = Constants.Popover.size
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
        
        // Subscribe to notification count changes (dismiss/clearAll)
        eventBus.notificationCountDeltaPublisher
            .receive(on: DispatchQueue.main)
            .sink { [weak self] delta in
                guard let self = self else { return }
                if delta == Int.min {
                    self.notificationCount = 0
                } else {
                    self.notificationCount = max(0, self.notificationCount + delta)
                }
                self.updateIcon(hasNotifications: self.notificationCount > 0)
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
        
        // Handle menubar icon style changes from settings
        NotificationCenter.default.publisher(for: .menubarIconChanged)
            .receive(on: DispatchQueue.main)
            .sink { [weak self] _ in
                self?.updateIconAppearance()
            }
            .store(in: &cancellables)
    }
}

extension Notification.Name {
    static let scrollToNotification = Notification.Name("scrollToNotification")
}
