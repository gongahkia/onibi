import Foundation
import AppKit
import Combine
import ApplicationServices

/// Client for Ghostty-specific integration
/// Detects running Ghostty instances and communicates via available channels
final class GhosttyIPCClient: ObservableObject {
    static let shared = GhosttyIPCClient()
    
    @Published var isGhosttyRunning: Bool = false
    @Published var ghosttyVersion: String?
    @Published var activeSessions: [GhosttySession] = []
    private let processMonitor = GhosttyProcessMonitor()
    private var cancellables = Set<AnyCancellable>()
    private var settings: AppSettings = .default
    private init() {
        setupProcessMonitoring()
        EventBus.shared.settingsPublisher
            .sink { [weak self] s in self?.settings = s }
            .store(in: &cancellables)
    }
    
    // MARK: - Process Detection
    
    /// Check if Ghostty is currently running
    func checkGhosttyRunning() -> Bool {
        let bundleId = settings.ghosttyBundleId
        let running = NSWorkspace.shared.runningApplications.contains { app in
            app.bundleIdentifier == bundleId ||
            app.localizedName?.lowercased() == "ghostty"
        }
        
        DispatchQueue.main.async {
            self.isGhosttyRunning = running
        }
        
        return running
    }
    
    /// Get Ghostty application if running
    func getGhosttyApp() -> NSRunningApplication? {
        let bundleId = settings.ghosttyBundleId
        return NSWorkspace.shared.runningApplications.first { app in
            app.bundleIdentifier == bundleId ||
            app.localizedName?.lowercased() == "ghostty"
        }
    }
    
    /// Attempt to launch Ghostty
    func launchGhostty() {
        if let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: settings.ghosttyBundleId) {
            NSWorkspace.shared.openApplication(at: url, configuration: NSWorkspace.OpenConfiguration())
        } else if let url = NSWorkspace.shared.urlForApplication(withBundleIdentifier: "com.mitchellh.ghostty") {
            // Fallback: try default bundle identifier
            NSWorkspace.shared.openApplication(at: url, configuration: NSWorkspace.OpenConfiguration())
        }
    }
    
    // MARK: - IPC (Future)
    
    /// Execute a Ghostty CLI command
    func executeCommand(_ arguments: [String]) async throws -> String {
        guard isGhosttyRunning else {
            throw GhosttyError.notRunning
        }
        
        let binaryPath = settings.ghosttyBinaryPath
        guard FileManager.default.fileExists(atPath: binaryPath) else {
            throw GhosttyError.binaryNotFound
        }
        
        let process = Process()
        process.executableURL = URL(fileURLWithPath: binaryPath)
        process.arguments = arguments
        
        let pipe = Pipe()
        process.standardOutput = pipe
        
        try process.run()
        
        return try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global().async {
                process.waitUntilExit()
                
                let data = pipe.fileHandleForReading.readDataToEndOfFile()
                if let output = String(data: data, encoding: .utf8) {
                    continuation.resume(returning: output)
                } else {
                    continuation.resume(throwing: GhosttyError.outputDecodingFailed)
                }
            }
        }
    }
    
    enum GhosttyError: Error {
        case notRunning
        case binaryNotFound
        case outputDecodingFailed
    }
    
    // MARK: - Window/Session Detection
    
    /// Get active Ghostty windows
    func getActiveWindows() -> [GhosttySession] {
        guard
            isGhosttyRunning,
            let app = getGhosttyApp()
        else {
            activeSessions = []
            return []
        }

        let snapshots = accessibilityWindows(for: app.processIdentifier)
        let hasFocusedWindow = snapshots.contains { $0.isFocused }

        let sessions: [GhosttySession]
        if snapshots.isEmpty {
            sessions = [
                GhosttySession(
                    id: "\(app.processIdentifier)",
                    name: "Ghostty",
                    pid: app.processIdentifier,
                    isActive: app.isActive
                )
            ]
        } else {
            sessions = snapshots.enumerated().map { index, snapshot in
                let title = normalizedTitle(for: snapshot.title, fallbackIndex: index)
                let isSessionActive: Bool
                if hasFocusedWindow {
                    isSessionActive = snapshot.isFocused
                } else {
                    isSessionActive = app.isActive && !snapshot.isMinimized && index == 0
                }

                return GhosttySession(
                    id: sessionIdentifier(
                        for: app.processIdentifier,
                        snapshot: snapshot,
                        fallbackIndex: index
                    ),
                    name: title,
                    pid: app.processIdentifier,
                    isActive: isSessionActive,
                    windowTitle: snapshot.title
                )
            }
        }

        activeSessions = sessions
        return sessions
    }
    
    // MARK: - Private

    private struct AccessibilityWindowSnapshot {
        let windowNumber: Int?
        let title: String?
        let isFocused: Bool
        let isMinimized: Bool
    }

    private func accessibilityWindows(for pid: pid_t) -> [AccessibilityWindowSnapshot] {
        guard AXIsProcessTrusted() else {
            return []
        }

        let applicationElement = AXUIElementCreateApplication(pid)
        var windowsValue: CFTypeRef?
        let status = AXUIElementCopyAttributeValue(
            applicationElement,
            kAXWindowsAttribute as CFString,
            &windowsValue
        )
        guard
            status == .success,
            let windowElements = windowsValue as? [AXUIElement]
        else {
            return []
        }

        return windowElements.map { element in
            AccessibilityWindowSnapshot(
                windowNumber: intValue(for: "AXWindowNumber" as CFString, from: element),
                title: stringValue(for: kAXTitleAttribute as CFString, from: element),
                isFocused: boolValue(for: kAXFocusedAttribute as CFString, from: element)
                    || boolValue(for: kAXMainAttribute as CFString, from: element),
                isMinimized: boolValue(for: kAXMinimizedAttribute as CFString, from: element)
            )
        }
    }

    private func sessionIdentifier(
        for pid: pid_t,
        snapshot: AccessibilityWindowSnapshot,
        fallbackIndex: Int
    ) -> String {
        if let windowNumber = snapshot.windowNumber {
            return "\(pid)-\(windowNumber)"
        }
        return "\(pid)-\(fallbackIndex)"
    }

    private func normalizedTitle(for title: String?, fallbackIndex: Int) -> String {
        let trimmedTitle = title?.trimmingCharacters(in: .whitespacesAndNewlines)
        if let trimmedTitle, !trimmedTitle.isEmpty {
            return trimmedTitle
        }
        return "Ghostty Window \(fallbackIndex + 1)"
    }

    private func stringValue(for attribute: CFString, from element: AXUIElement) -> String? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, attribute, &value) == .success else {
            return nil
        }
        return value as? String
    }

    private func boolValue(for attribute: CFString, from element: AXUIElement) -> Bool {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, attribute, &value) == .success else {
            return false
        }
        return (value as? Bool) ?? false
    }

    private func intValue(for attribute: CFString, from element: AXUIElement) -> Int? {
        var value: CFTypeRef?
        guard AXUIElementCopyAttributeValue(element, attribute, &value) == .success else {
            return nil
        }
        if let number = value as? NSNumber {
            return number.intValue
        }
        return nil
    }

    private func setupProcessMonitoring() {
        // Initial check
        _ = checkGhosttyRunning()
        
        // Monitor for launches/terminations
        processMonitor.$isGhosttyRunning
            .receive(on: DispatchQueue.main)
            .assign(to: &$isGhosttyRunning)
    }
}

// MARK: - Ghostty Session

/// Represents a Ghostty terminal session
struct GhosttySession: Identifiable, Equatable {
    let id: String
    let name: String
    let pid: pid_t
    var isActive: Bool
    var windowTitle: String?
    var commandCount: Int = 0
    var startTime: Date = Date()
}

// MARK: - Process Monitor

/// Monitors Ghostty process lifecycle using NSWorkspace notifications
final class GhosttyProcessMonitor: ObservableObject {
    @Published var isGhosttyRunning: Bool = false
    private var launchObserver: Any?
    private var terminateObserver: Any?
    private var bundleId: String { SettingsViewModel.shared.settings.ghosttyBundleId }
    init() {
        setupObservers()
        checkInitialState()
    }
    
    deinit {
        if let observer = launchObserver {
            NSWorkspace.shared.notificationCenter.removeObserver(observer)
        }
        if let observer = terminateObserver {
            NSWorkspace.shared.notificationCenter.removeObserver(observer)
        }
    }
    
    private func setupObservers() {
        // Watch for app launches
        launchObserver = NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.didLaunchApplicationNotification,
            object: nil,
            queue: .main
        ) { [weak self] notification in
            self?.handleAppLaunch(notification)
        }
        
        // Watch for app terminations
        terminateObserver = NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.didTerminateApplicationNotification,
            object: nil,
            queue: .main
        ) { [weak self] notification in
            self?.handleAppTerminate(notification)
        }
    }
    
    private func checkInitialState() {
        isGhosttyRunning = NSWorkspace.shared.runningApplications.contains { app in
            app.bundleIdentifier == self.bundleId || app.localizedName?.lowercased() == "ghostty"
        }
    }
    private func isGhostty(_ app: NSRunningApplication) -> Bool {
        app.bundleIdentifier == bundleId || app.localizedName?.lowercased() == "ghostty"
    }
    private func handleAppLaunch(_ notification: Notification) {
        guard let app = notification.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication else { return }
        if isGhostty(app) {
            isGhosttyRunning = true
            NotificationCenter.default.post(name: .ghosttyDidLaunch, object: nil)
        }
    }
    private func handleAppTerminate(_ notification: Notification) {
        guard let app = notification.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication else { return }
        if isGhostty(app) {
            isGhosttyRunning = false
            NotificationCenter.default.post(name: .ghosttyDidTerminate, object: nil)
        }
    }
}

// MARK: - Notification Names

extension Notification.Name {
    static let ghosttyDidLaunch = Notification.Name("ghosttyDidLaunch")
    static let ghosttyDidTerminate = Notification.Name("ghosttyDidTerminate")
}
