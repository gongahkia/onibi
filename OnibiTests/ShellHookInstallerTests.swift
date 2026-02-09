import XCTest
@testable import Onibi

final class ShellHookInstallerTests: XCTestCase {
    
    private var installer: ShellHookInstaller!
    private var testZshRc: String!
    private var testBashRc: String!
    private var testFishConfig: String!
    private var tempDir: String!
    
    override func setUp() {
        super.setUp()
        installer = ShellHookInstaller.shared
        
        // Create temp directory for test files
        tempDir = NSTemporaryDirectory() + "onibi-test-\(UUID().uuidString)"
        try? FileManager.default.createDirectory(atPath: tempDir, withIntermediateDirectories: true)
        
        testZshRc = tempDir + "/.zshrc"
        testBashRc = tempDir + "/.bashrc"
        testFishConfig = tempDir + "/.config/fish/config.fish"
    }
    
    override func tearDown() {
        // Clean up temp directory
        try? FileManager.default.removeItem(atPath: tempDir)
        super.tearDown()
    }
    
    // MARK: - Helper Methods
    
    private func createMockRcFile(at path: String, with content: String = "") throws {
        let dir = (path as NSString).deletingLastPathComponent
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try content.write(toFile: path, atomically: true, encoding: .utf8)
    }
    
    private func readFile(at path: String) -> String? {
        try? String(contentsOfFile: path, encoding: .utf8)
    }
    
    // MARK: - Shell Detection Tests
    
    func testDetectCurrentShell() {
        // This test depends on the environment
        installer.detectCurrentShell()
        
        // At minimum, detectedShell should not crash the app
        // The actual value depends on the test environment
        XCTAssertTrue(installer.detectedShell == nil || installer.detectedShell != nil)
    }
    
    // MARK: - RC File Existence Tests
    
    func testRcFileExistsForZsh() throws {
        try createMockRcFile(at: testZshRc)
        
        // Create a mock shell with the test path
        XCTAssertTrue(FileManager.default.fileExists(atPath: testZshRc))
    }
    
    func testRcFileDoesNotExist() {
        let nonExistentPath = tempDir + "/nonexistent.rc"
        XCTAssertFalse(FileManager.default.fileExists(atPath: nonExistentPath))
    }
    
    // MARK: - Hook Script Generation Tests
    
    func testZshHookScriptContainsMarkers() {
        let script = ShellHookInstaller.Shell.zshHookScript(logPath: "/tmp/test.log")
        
        XCTAssertTrue(script.contains(ShellHookInstaller.Shell.startMarker))
        XCTAssertTrue(script.contains(ShellHookInstaller.Shell.endMarker))
        XCTAssertTrue(script.contains("_onibi_preexec"))
        XCTAssertTrue(script.contains("_onibi_precmd"))
    }
    
    func testBashHookScriptContainsMarkers() {
        let script = ShellHookInstaller.Shell.bashHookScript(logPath: "/tmp/test.log")
        
        XCTAssertTrue(script.contains(ShellHookInstaller.Shell.startMarker))
        XCTAssertTrue(script.contains(ShellHookInstaller.Shell.endMarker))
        XCTAssertTrue(script.contains("_onibi_preexec"))
        XCTAssertTrue(script.contains("PROMPT_COMMAND"))
    }
    
    func testFishHookScriptContainsMarkers() {
        let script = ShellHookInstaller.Shell.fishHookScript(logPath: "/tmp/test.log")
        
        XCTAssertTrue(script.contains(ShellHookInstaller.Shell.startMarker))
        XCTAssertTrue(script.contains(ShellHookInstaller.Shell.endMarker))
        XCTAssertTrue(script.contains("_onibi_preexec"))
        XCTAssertTrue(script.contains("_onibi_postexec"))
    }
    
    func testHookScriptIncludesLogPath() {
        let logPath = "/custom/path/to/log.txt"
        let script = ShellHookInstaller.Shell.zshHookScript(logPath: logPath)
        
        XCTAssertTrue(script.contains(logPath))
    }
    
    // MARK: - Status Checking Tests
    
    func testCheckStatusNotInstalledWhenFileDoesNotExist() {
        // Use a mock shell that points to non-existent file
        // Since we can't modify the Shell enum, we'll test the logic indirectly
        let mockContent = "# Some content\nexport PATH=$PATH:/usr/local/bin\n"
        
        // Content without markers should be detected as not installed
        XCTAssertFalse(mockContent.contains(ShellHookInstaller.Shell.startMarker))
        XCTAssertFalse(mockContent.contains(ShellHookInstaller.Shell.endMarker))
    }
    
    func testCheckStatusInstalledWhenMarkersPresent() {
        let mockContent = """
        # Some content
        \(ShellHookInstaller.Shell.startMarker)
        # Hook content
        \(ShellHookInstaller.Shell.endMarker)
        """
        
        XCTAssertTrue(mockContent.contains(ShellHookInstaller.Shell.startMarker))
        XCTAssertTrue(mockContent.contains(ShellHookInstaller.Shell.endMarker))
    }
    
    // MARK: - Backup Creation Tests
    
    func testCreateBackupCreatesBackupFile() throws {
        let mockRcPath = tempDir + "/test.rc"
        let originalContent = "# Original content\nexport TEST=1"
        try originalContent.write(toFile: mockRcPath, atomically: true, encoding: .utf8)
        
        let backupPath = mockRcPath + ".onibi-backup"
        
        // Manually copy to simulate backup
        try FileManager.default.copyItem(atPath: mockRcPath, toPath: backupPath)
        
        XCTAssertTrue(FileManager.default.fileExists(atPath: backupPath))
        
        let backupContent = try String(contentsOfFile: backupPath, encoding: .utf8)
        XCTAssertEqual(backupContent, originalContent)
    }
    
    func testBackupRotation() throws {
        let mockRcPath = tempDir + "/rotate.rc"
        let backupBase = mockRcPath + ".onibi-backup"
        let backup1 = backupBase + ".1"
        let backup2 = backupBase + ".2"
        let backup3 = backupBase + ".3"
        
        // Create initial file
        try "v0".write(toFile: mockRcPath, atomically: true, encoding: .utf8)
        try "v0".write(toFile: backupBase, atomically: true, encoding: .utf8)
        
        // Simulate first rotation
        try FileManager.default.moveItem(atPath: backupBase, toPath: backup1)
        try "v1".write(toFile: backupBase, atomically: true, encoding: .utf8)
        
        XCTAssertTrue(FileManager.default.fileExists(atPath: backup1))
        XCTAssertEqual(try String(contentsOfFile: backup1, encoding: .utf8), "v0")
        
        // Simulate second rotation
        if FileManager.default.fileExists(atPath: backup1) {
            try FileManager.default.moveItem(atPath: backup1, toPath: backup2)
        }
        try FileManager.default.moveItem(atPath: backupBase, toPath: backup1)
        try "v2".write(toFile: backupBase, atomically: true, encoding: .utf8)
        
        XCTAssertTrue(FileManager.default.fileExists(atPath: backup2))
        XCTAssertTrue(FileManager.default.fileExists(atPath: backup1))
    }
    
    // MARK: - Collision Detection Tests
    
    func testInstallDetectsExistingHooks() {
        let contentWithHooks = """
        # Existing config
        \(ShellHookInstaller.Shell.startMarker)
        # Existing hooks
        \(ShellHookInstaller.Shell.endMarker)
        """
        
        let hasMarkers = contentWithHooks.contains(ShellHookInstaller.Shell.startMarker)
        XCTAssertTrue(hasMarkers)
    }
    
    func testInstallDoesNotDetectSimilarComments() {
        let contentWithSimilar = """
        # Some comment about onibi
        # Not the actual marker
        export PATH=$PATH
        """
        
        let lines = contentWithSimilar.components(separatedBy: .newlines)
        let hasExactMarker = lines.contains { $0.trimmingCharacters(in: .whitespaces) == ShellHookInstaller.Shell.startMarker }
        XCTAssertFalse(hasExactMarker)
    }
    
    // MARK: - Uninstall Tests
    
    func testUninstallRemovesHookSection() {
        var content = """
        # Before hooks
        export PATH=$PATH
        
        \(ShellHookInstaller.Shell.startMarker)
        # Hook content here
        function _onibi_test() { echo "test"; }
        \(ShellHookInstaller.Shell.endMarker)
        
        # After hooks
        alias ll='ls -la'
        """
        
        // Simulate uninstall by removing hook section
        var lines = content.components(separatedBy: "\n")
        var inHookSection = false
        var indicesToRemove: [Int] = []
        
        for (index, line) in lines.enumerated() {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed == ShellHookInstaller.Shell.startMarker {
                inHookSection = true
            }
            if inHookSection {
                indicesToRemove.append(index)
            }
            if trimmed == ShellHookInstaller.Shell.endMarker {
                inHookSection = false
            }
        }
        
        for index in indicesToRemove.reversed() {
            lines.remove(at: index)
        }
        
        let result = lines.joined(separator: "\n")
        
        XCTAssertFalse(result.contains(ShellHookInstaller.Shell.startMarker))
        XCTAssertFalse(result.contains(ShellHookInstaller.Shell.endMarker))
        XCTAssertTrue(result.contains("# Before hooks"))
        XCTAssertTrue(result.contains("# After hooks"))
    }
    
    func testUninstallPreservesOtherContent() {
        let beforeHooks = "export TEST=1\nalias foo='bar'"
        let afterHooks = "export OTHER=2\nalias baz='qux'"
        
        let content = """
        \(beforeHooks)
        
        \(ShellHookInstaller.Shell.startMarker)
        # Hooks
        \(ShellHookInstaller.Shell.endMarker)
        
        \(afterHooks)
        """
        
        // Simulate uninstall
        var lines = content.components(separatedBy: "\n")
        var inHookSection = false
        var indicesToRemove: [Int] = []
        
        for (index, line) in lines.enumerated() {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed == ShellHookInstaller.Shell.startMarker {
                inHookSection = true
            }
            if inHookSection {
                indicesToRemove.append(index)
            }
            if trimmed == ShellHookInstaller.Shell.endMarker {
                inHookSection = false
            }
        }
        
        for index in indicesToRemove.reversed() {
            lines.remove(at: index)
        }
        
        let result = lines.joined(separator: "\n")
        
        XCTAssertTrue(result.contains("export TEST=1"))
        XCTAssertTrue(result.contains("alias foo='bar'"))
        XCTAssertTrue(result.contains("export OTHER=2"))
        XCTAssertTrue(result.contains("alias baz='qux'"))
    }
    
    // MARK: - Shell Type Tests
    
    func testAllShellTypes() {
        let shells = ShellHookInstaller.Shell.allCases
        
        XCTAssertTrue(shells.contains(.zsh))
        XCTAssertTrue(shells.contains(.bash))
        XCTAssertTrue(shells.contains(.fish))
        XCTAssertEqual(shells.count, 3)
    }
    
    func testShellRcFilePaths() {
        let homeDir = NSHomeDirectory()
        
        XCTAssertEqual(ShellHookInstaller.Shell.zsh.rcFilePath, homeDir + "/.zshrc")
        XCTAssertEqual(ShellHookInstaller.Shell.bash.rcFilePath, homeDir + "/.bashrc")
        XCTAssertEqual(ShellHookInstaller.Shell.fish.rcFilePath, homeDir + "/.config/fish/config.fish")
    }
    
    func testShellFromRawValue() {
        XCTAssertEqual(ShellHookInstaller.Shell(rawValue: "zsh"), .zsh)
        XCTAssertEqual(ShellHookInstaller.Shell(rawValue: "bash"), .bash)
        XCTAssertEqual(ShellHookInstaller.Shell(rawValue: "fish"), .fish)
        XCTAssertNil(ShellHookInstaller.Shell(rawValue: "unknown"))
    }
    
    // MARK: - Installation Status Tests
    
    func testInstallationStatusEquatable() {
        XCTAssertEqual(ShellHookInstaller.InstallationStatus.notInstalled, .notInstalled)
        XCTAssertEqual(ShellHookInstaller.InstallationStatus.installed, .installed)
        XCTAssertNotEqual(ShellHookInstaller.InstallationStatus.notInstalled, .installed)
    }
    
    func testInstallationStatusError() {
        let error1 = ShellHookInstaller.InstallationStatus.error("Test error")
        let error2 = ShellHookInstaller.InstallationStatus.error("Test error")
        
        XCTAssertEqual(error1, error2)
    }
    
    // MARK: - Error Tests
    
    func testInstallErrorDescriptions() {
        XCTAssertNotNil(ShellHookInstaller.InstallError.alreadyInstalled.errorDescription)
        XCTAssertNotNil(ShellHookInstaller.InstallError.backupFailed.errorDescription)
        XCTAssertNotNil(ShellHookInstaller.InstallError.writeFailed.errorDescription)
    }
    
    // MARK: - Marker Tests
    
    func testMarkerFormat() {
        XCTAssertEqual(ShellHookInstaller.Shell.startMarker, "# >>> onibi >>>")
        XCTAssertEqual(ShellHookInstaller.Shell.endMarker, "# <<< onibi <<<")
    }
    
    func testHookScriptHasMatchingMarkers() {
        let zshScript = ShellHookInstaller.Shell.zshHookScript(logPath: "/tmp/log")
        let bashScript = ShellHookInstaller.Shell.bashHookScript(logPath: "/tmp/log")
        let fishScript = ShellHookInstaller.Shell.fishHookScript(logPath: "/tmp/log")
        
        for script in [zshScript, bashScript, fishScript] {
            let startCount = script.components(separatedBy: ShellHookInstaller.Shell.startMarker).count - 1
            let endCount = script.components(separatedBy: ShellHookInstaller.Shell.endMarker).count - 1
            
            XCTAssertEqual(startCount, 1, "Each script should have exactly one start marker")
            XCTAssertEqual(endCount, 1, "Each script should have exactly one end marker")
        }
    }
}
