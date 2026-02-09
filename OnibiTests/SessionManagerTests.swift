import XCTest
@testable import Onibi

final class SessionManagerTests: XCTestCase {
    
    private var manager: SessionManager!
    
    override func setUp() {
        super.setUp()
        manager = SessionManager.shared
        manager.clearAll()
    }
    
    override func tearDown() {
        manager.clearAll()
        super.tearDown()
    }
    
    // MARK: - Session Creation Tests
    
    func testGetOrCreateSessionCreatesNewSession() {
        let sessionId = "test-session-1"
        
        let session = manager.getOrCreateSession(id: sessionId)
        
        XCTAssertEqual(session.id, sessionId)
        XCTAssertTrue(session.isActive)
        XCTAssertEqual(session.commandCount, 0)
        XCTAssertEqual(manager.activeSessions.count, 1)
    }
    
    func testGetOrCreateSessionReturnsExistingSession() {
        let sessionId = "test-session-2"
        
        let session1 = manager.getOrCreateSession(id: sessionId)
        let session2 = manager.getOrCreateSession(id: sessionId)
        
        XCTAssertEqual(session1.id, session2.id)
        XCTAssertEqual(manager.activeSessions.count, 1)
    }
    
    func testGetOrCreateSessionMultipleSessions() {
        let session1 = manager.getOrCreateSession(id: "session-1")
        let session2 = manager.getOrCreateSession(id: "session-2")
        let session3 = manager.getOrCreateSession(id: "session-3")
        
        XCTAssertNotEqual(session1.id, session2.id)
        XCTAssertNotEqual(session2.id, session3.id)
        XCTAssertEqual(manager.activeSessions.count, 3)
    }
    
    // MARK: - Activity Recording Tests
    
    func testRecordActivityCreatesSessionIfNotExists() {
        let sessionId = "new-session"
        
        manager.recordActivity(sessionId: sessionId)
        
        XCTAssertNotNil(manager.activeSessions[sessionId])
        XCTAssertEqual(manager.activeSessions[sessionId]?.commandCount, 1)
    }
    
    func testRecordActivityIncrementsCommandCount() {
        let sessionId = "test-session"
        _ = manager.getOrCreateSession(id: sessionId)
        
        manager.recordActivity(sessionId: sessionId)
        manager.recordActivity(sessionId: sessionId)
        
        XCTAssertEqual(manager.activeSessions[sessionId]?.commandCount, 2)
    }
    
    func testRecordActivityUpdatesLastActivityTime() {
        let sessionId = "time-test-session"
        let session = manager.getOrCreateSession(id: sessionId)
        let originalTime = session.lastActivityTime
        
        // Wait a bit to ensure time difference
        Thread.sleep(forTimeInterval: 0.1)
        
        manager.recordActivity(sessionId: sessionId)
        
        let updatedSession = manager.activeSessions[sessionId]
        XCTAssertNotNil(updatedSession)
        XCTAssertGreaterThan(updatedSession!.lastActivityTime, originalTime)
    }
    
    func testRecordActivityMarksSessionActive() {
        let sessionId = "inactive-session"
        _ = manager.getOrCreateSession(id: sessionId)
        manager.markInactive(sessionId: sessionId)
        
        XCTAssertFalse(manager.activeSessions[sessionId]?.isActive ?? true)
        
        manager.recordActivity(sessionId: sessionId)
        
        XCTAssertTrue(manager.activeSessions[sessionId]?.isActive ?? false)
    }
    
    // MARK: - Inactive Session Pruning Tests
    
    func testMarkInactive() {
        let sessionId = "active-session"
        _ = manager.getOrCreateSession(id: sessionId)
        
        XCTAssertTrue(manager.activeSessions[sessionId]?.isActive ?? false)
        
        manager.markInactive(sessionId: sessionId)
        
        XCTAssertFalse(manager.activeSessions[sessionId]?.isActive ?? true)
    }
    
    func testMarkInactiveAddsToRecentSessions() {
        let sessionId = "recent-session"
        _ = manager.getOrCreateSession(id: sessionId)
        
        manager.markInactive(sessionId: sessionId)
        
        XCTAssertEqual(manager.recentSessions.count, 1)
        XCTAssertEqual(manager.recentSessions.first?.id, sessionId)
    }
    
    func testMarkInactiveLimitsRecentSessionsTo10() {
        // Create 15 sessions and mark them inactive
        for i in 1...15 {
            let sessionId = "session-\(i)"
            _ = manager.getOrCreateSession(id: sessionId)
            manager.markInactive(sessionId: sessionId)
        }
        
        XCTAssertEqual(manager.recentSessions.count, 10)
    }
    
    func testCheckForInactiveSessionsMarksOldSessionsInactive() async {
        let sessionId = "old-session"
        var session = manager.getOrCreateSession(id: sessionId)
        
        // Manually set last activity to 6 minutes ago (> 5 min timeout)
        session.lastActivityTime = Date().addingTimeInterval(-360)
        manager.activeSessions[sessionId] = session
        
        manager.checkForInactiveSessions()
        
        XCTAssertFalse(manager.activeSessions[sessionId]?.isActive ?? true)
    }
    
    func testCheckForInactiveSessionsKeepsRecentSessionsActive() {
        let sessionId = "recent-session"
        _ = manager.getOrCreateSession(id: sessionId)
        
        manager.checkForInactiveSessions()
        
        XCTAssertTrue(manager.activeSessions[sessionId]?.isActive ?? false)
    }
    
    func testPruneStaleSessions() {
        let sessionId = "stale-session"
        var session = manager.getOrCreateSession(id: sessionId)
        
        // Set last activity to 25 hours ago (> 24h)
        session.lastActivityTime = Date().addingTimeInterval(-90000)
        manager.activeSessions[sessionId] = session
        
        manager.pruneStaleSessions()
        
        XCTAssertNil(manager.activeSessions[sessionId])
    }
    
    func testPruneStaleSessionsKeepsRecentSessions() {
        let sessionId = "recent-session"
        _ = manager.getOrCreateSession(id: sessionId)
        
        manager.pruneStaleSessions()
        
        XCTAssertNotNil(manager.activeSessions[sessionId])
    }
    
    // MARK: - Cleanup Tests
    
    func testClearAllRemovesAllSessions() {
        _ = manager.getOrCreateSession(id: "session-1")
        _ = manager.getOrCreateSession(id: "session-2")
        manager.markInactive(sessionId: "session-1")
        
        XCTAssertGreaterThan(manager.activeSessions.count, 0)
        XCTAssertGreaterThan(manager.recentSessions.count, 0)
        
        manager.clearAll()
        
        XCTAssertEqual(manager.activeSessions.count, 0)
        XCTAssertEqual(manager.recentSessions.count, 0)
    }
    
    // MARK: - Session ID Retrieval Tests
    
    func testAllSessionIds() {
        _ = manager.getOrCreateSession(id: "session-b")
        _ = manager.getOrCreateSession(id: "session-a")
        _ = manager.getOrCreateSession(id: "session-c")
        
        let sessionIds = manager.allSessionIds
        
        XCTAssertEqual(sessionIds.count, 3)
        XCTAssertEqual(sessionIds, ["session-a", "session-b", "session-c"])
    }
    
    func testActiveSessionIds() {
        _ = manager.getOrCreateSession(id: "active-1")
        _ = manager.getOrCreateSession(id: "active-2")
        _ = manager.getOrCreateSession(id: "inactive-1")
        manager.markInactive(sessionId: "inactive-1")
        
        let activeIds = manager.activeSessionIds
        
        XCTAssertEqual(activeIds.count, 2)
        XCTAssertTrue(activeIds.contains("active-1"))
        XCTAssertTrue(activeIds.contains("active-2"))
        XCTAssertFalse(activeIds.contains("inactive-1"))
    }
    
    // MARK: - Display Helpers Tests
    
    func testDisplayNameWithCustomName() {
        let sessionId = "custom-session"
        var session = manager.getOrCreateSession(id: sessionId)
        session.displayName = "My Custom Session"
        manager.activeSessions[sessionId] = session
        
        let displayName = manager.displayName(for: sessionId)
        
        XCTAssertEqual(displayName, "My Custom Session")
    }
    
    func testDisplayNameWithLongSessionId() {
        let sessionId = "very-long-session-id-that-should-be-shortened"
        _ = manager.getOrCreateSession(id: sessionId)
        
        let displayName = manager.displayName(for: sessionId)
        
        XCTAssertTrue(displayName.hasSuffix("..."))
        XCTAssertLessThan(displayName.count, sessionId.count)
    }
    
    func testDisplayNameWithShortSessionId() {
        let sessionId = "short"
        _ = manager.getOrCreateSession(id: sessionId)
        
        let displayName = manager.displayName(for: sessionId)
        
        XCTAssertEqual(displayName, sessionId)
    }
    
    func testSessionContextWithValidId() {
        let sessionId = "context-session"
        _ = manager.getOrCreateSession(id: sessionId)
        
        let context = manager.sessionContext(for: sessionId)
        
        XCTAssertNotNil(context)
        XCTAssertTrue(context!.contains("Session:"))
    }
    
    func testSessionContextWithNilId() {
        let context = manager.sessionContext(for: nil)
        
        XCTAssertNil(context)
    }
    
    // MARK: - Session Model Tests
    
    func testFormattedDuration() {
        let session = SessionManager.TerminalSession(
            id: "test",
            startTime: Date().addingTimeInterval(-125),
            lastActivityTime: Date(),
            commandCount: 5,
            isActive: true
        )
        
        let formatted = session.formattedDuration
        
        // Should format duration (around 2 minutes)
        XCTAssertFalse(formatted.isEmpty)
        XCTAssertNotEqual(formatted, "0s")
    }
    
    func testDisplayColorConsistency() {
        let session = SessionManager.TerminalSession(
            id: "color-test",
            startTime: Date(),
            lastActivityTime: Date(),
            commandCount: 0,
            isActive: true
        )
        
        let color1 = session.displayColor
        let color2 = session.displayColor
        
        // Same session should always return same color
        XCTAssertEqual(color1, color2)
    }
}
