import Foundation

/// Service for interacting with Ghostty CLI
final class GhosttyCliService {
    static let shared = GhosttyCliService()
    
    private let client = GhosttyIPCClient.shared
    
    private init() {}
    
    /// specific error type
    enum CliError: Error {
        case parseError
        case executionError(Error)
    }
    
    /// Get current Ghostty configuration
    /// Runs `ghostty +show-config` and parses the output
    func showConfig() async throws -> [String: String] {
        do {
            let output = try await client.executeCommand(["+show-config", "--default-config=false"])
            return parseConfigOutput(output)
        } catch {
            throw CliError.executionError(error)
        }
    }
    
    /// List available themes
    /// Runs `ghostty +list-themes`
    func listThemes() async throws -> [String] {
        do {
            let output = try await client.executeCommand(["+list-themes"])
            // Output format: one theme per line
            return output.components(separatedBy: .newlines)
                .map { $0.trimmingCharacters(in: .whitespaces) }
                .filter { !$0.isEmpty }
                .sorted()
        } catch {
            throw CliError.executionError(error)
        }
    }
    
    /// Check if Ghostty is installed and get version
    func getVersion() async throws -> String {
        do {
             let output = try await client.executeCommand(["--version"])
             // Output format: 
             // ghostty 1.0.0 (abcd123)
             // ...
             if let firstLine = output.components(separatedBy: .newlines).first {
                 return firstLine
             }
             return "Unknown"
        } catch {
            throw CliError.executionError(error)
        }
    }
    
    /// Check if Ghostty binary exists in PATH and return version string
    func isGhosttyInstalled() async -> (installed: Bool, version: String?) {
        do {
            let version = try await getVersion()
            return (true, version)
        } catch {
            return (false, nil)
        }
    }
    
    // MARK: - Private
    
    private func parseConfigOutput(_ output: String) -> [String: String] {
        var config: [String: String] = [:]
        
        let lines = output.components(separatedBy: .newlines)
        for line in lines {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            guard !trimmed.isEmpty, !trimmed.hasPrefix("#") else { continue }
            
            let parts = trimmed.split(separator: "=", maxSplits: 1)
            guard parts.count == 2 else { continue }
            
            let key = String(parts[0]).trimmingCharacters(in: .whitespaces)
            let value = String(parts[1]).trimmingCharacters(in: .whitespaces)
            
            config[key] = value
        }
        
        return config
    }
}
