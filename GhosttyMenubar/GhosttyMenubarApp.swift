import SwiftUI
import Combine

@main
struct GhosttyMenubarApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate
    
    var body: some Scene {
        Settings {
            EmptyView()
        }
    }
}

class AppDelegate: NSObject, NSApplicationDelegate {
    var menuBarController: MenuBarController?
    
    func applicationDidFinishLaunching(_ notification: Notification) {
        // Ensure app data directory exists
        try? GhosttyConfig.ensureDirectoryExists()
        
        // Set up menubar controller
        menuBarController = MenuBarController()
        menuBarController?.setup()
    }
    
    func applicationWillTerminate(_ notification: Notification) {
        menuBarController?.cleanup()
    }
}

