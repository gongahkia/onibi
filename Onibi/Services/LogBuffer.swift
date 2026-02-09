import Foundation

/// Handles incremental file reading and partial line handling
final class LogBuffer {
    private var buffer: String = ""
    private var lastReadPosition: UInt64 = 0
    private var currentFileID: NSNumber?
    private let filePath: String
    
    init(filePath: String) {
        self.filePath = filePath
    }
    
    /// Read new content from file since last read
    func readNewContent() throws -> String {
        guard FileManager.default.fileExists(atPath: filePath) else {
            return ""
        }
        
        guard let fileHandle = FileHandle(forReadingAtPath: filePath) else {
            throw LogBufferError.cannotOpenFile
        }
        
        defer { try? fileHandle.close() }
        
        // Check for file rotation via inode
        let attributes = try? FileManager.default.attributesOfItem(atPath: filePath)
        let fileID = attributes?[.systemFileNumber] as? NSNumber
        
        if let currentID = currentFileID, let newID = fileID,  currentID != newID {
            // File rotated (new inode)
            lastReadPosition = 0
            buffer = ""
            currentFileID = newID
        } else if currentFileID == nil {
            currentFileID = fileID
        }
        
        // Check if file was truncated/rotated
        let fileSize = try fileHandle.seekToEnd()
        if fileSize < lastReadPosition {
            // File was rotated, start from beginning
            lastReadPosition = 0
            buffer = ""
        }
        
        // Seek to last position
        try fileHandle.seek(toOffset: lastReadPosition)
        
        // Read new data
        guard let data = try fileHandle.readToEnd(),
              let newContent = String(data: data, encoding: .utf8) else {
            return ""
        }
        
        lastReadPosition = fileSize
        
        return newContent
    }
    
    /// Get complete lines from new content, buffering incomplete lines
    func getCompleteLines(from newContent: String) -> [String] {
        buffer += newContent
        
        var lines: [String] = []
        var remaining = buffer
        
        while let newlineIndex = remaining.firstIndex(of: "\n") {
            let line = String(remaining[..<newlineIndex])
            if !line.isEmpty {
                lines.append(line)
            }
            remaining = String(remaining[remaining.index(after: newlineIndex)...])
        }
        
        // Keep incomplete line in buffer
        buffer = remaining
        
        return lines
    }
    
    /// Reset the buffer and file position
    func reset() {
        buffer = ""
        lastReadPosition = 0
        currentFileID = nil
    }
    
    /// Set position to end of file (skip existing content)
    func seekToEnd() throws {
        guard FileManager.default.fileExists(atPath: filePath) else {
            lastReadPosition = 0
            return
        }
        
        guard let fileHandle = FileHandle(forReadingAtPath: filePath) else {
            throw LogBufferError.cannotOpenFile
        }
        
        defer { try? fileHandle.close() }
        
        lastReadPosition = try fileHandle.seekToEnd()
        
        let attributes = try? FileManager.default.attributesOfItem(atPath: filePath)
        currentFileID = attributes?[.systemFileNumber] as? NSNumber
    }
}

enum LogBufferError: Error, LocalizedError {
    case cannotOpenFile
    case readError
    
    var errorDescription: String? {
        switch self {
        case .cannotOpenFile:
            return "Cannot open log file"
        case .readError:
            return "Error reading log file"
        }
    }
}
