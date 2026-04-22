import Dispatch
import Foundation
import OnibiCore
import Darwin

struct SessionProxyLaunchConfiguration {
    let socketPath: String
    let sessionId: String
    let shellPath: String
    let shellArguments: [String]
    let version: String
    let workingDirectory: String?

    init(environment: [String: String] = ProcessInfo.processInfo.environment) throws {
        guard let socketPath = environment["ONIBI_PROXY_SOCKET_PATH"], !socketPath.isEmpty else {
            throw SessionProxyRuntimeError.missingEnvironment("ONIBI_PROXY_SOCKET_PATH")
        }
        guard let sessionId = environment["ONIBI_HOST_SESSION_ID"], !sessionId.isEmpty else {
            throw SessionProxyRuntimeError.missingEnvironment("ONIBI_HOST_SESSION_ID")
        }
        guard let shellPath = environment["ONIBI_PARENT_SHELL"], !shellPath.isEmpty else {
            throw SessionProxyRuntimeError.missingEnvironment("ONIBI_PARENT_SHELL")
        }

        self.socketPath = socketPath
        self.sessionId = sessionId
        self.shellPath = shellPath
        self.shellArguments = SessionProxyLaunchConfiguration.parseShellArguments(
            environment["ONIBI_PARENT_SHELL_ARGS"] ?? ""
        )
        self.version = environment["ONIBI_PROXY_VERSION"] ?? "dev"
        self.workingDirectory = environment["PWD"]
    }

    static func parseShellArguments(_ rawValue: String) -> [String] {
        rawValue
            .split(whereSeparator: \.isWhitespace)
            .map(String.init)
    }
}

enum SessionProxyRuntimeError: LocalizedError {
    case missingEnvironment(String)
    case unixSocketPathTooLong(String)
    case posixFailure(function: String, code: Int32)
    case invalidFrameType(String)
    case invalidOutputPayload
    case invalidResizePayload

    var errorDescription: String? {
        switch self {
        case .missingEnvironment(let name):
            return "Missing required environment variable \(name)"
        case .unixSocketPathTooLong(let path):
            return "Unix socket path is too long: \(path)"
        case .posixFailure(let function, let code):
            return "\(function) failed with errno \(code): \(String(cString: strerror(code)))"
        case .invalidFrameType(let type):
            return "Unsupported proxy frame type: \(type)"
        case .invalidOutputPayload:
            return "Failed to encode or decode session output"
        case .invalidResizePayload:
            return "Failed to decode resize payload"
        }
    }
}

final class SessionProxyRuntime {
    private let configuration: SessionProxyLaunchConfiguration
    private let queue = DispatchQueue(label: "com.onibi.session-proxy", qos: .userInitiated)

    private var hostSocketFD: Int32 = -1
    private var childPID: pid_t = 0
    private var ptyMasterFD: Int32 = -1

    private var hostReadSource: DispatchSourceRead?
    private var stdinReadSource: DispatchSourceRead?
    private var ptyReadSource: DispatchSourceRead?
    private var processSource: DispatchSourceProcess?
    private var heartbeatTimer: DispatchSourceTimer?
    private var winchSource: DispatchSourceSignal?

    private var hostBuffer = Data()
    private var originalTerminalState: termios?
    private var lastReportedTerminalSize: RemoteTerminalResizePayload?
    private var lastReportedTerminalTitle: String?
    private var titleParser = TerminalTitleParser()
    private var commandLifecycleParser = TerminalCommandLifecycleParser()
    private var terminalEventParser = TerminalEventParser()
    private var inputByteCount = 0
    private var outputByteCount = 0
    private var droppedOutputByteCount = 0
    private var lastInputAt: Date?
    private var lastOutputAt: Date?
    private var isShuttingDown = false

    init(configuration: SessionProxyLaunchConfiguration) {
        self.configuration = configuration
    }

    func start() throws {
        try configureParentTerminal()
        hostSocketFD = try connectToHostSocket(path: configuration.socketPath)
        ptyMasterFD = try spawnChildShell()
        try sendFrame(
            LocalSessionProxyRegisterMessage(
                sessionId: configuration.sessionId,
                shell: configuration.shellPath,
                pid: childPID,
                startedAt: Date(),
                workingDirectory: configuration.workingDirectory,
                hostname: Host.current().localizedName ?? "localhost",
                proxyVersion: configuration.version
            )
        )
        try sendFrame(
            LocalSessionProxyStateMessage(
                sessionId: configuration.sessionId,
                status: .running
            )
        )
        syncWindowSize()
        startSources()
    }

    func fallbackExecShell() -> Never {
        restoreParentTerminalIfNeeded()
        Self.execShell(
            shellPath: configuration.shellPath,
            shellArguments: configuration.shellArguments,
            environment: sanitizedEnvironment(from: ProcessInfo.processInfo.environment)
        )
    }

    static func fallbackLaunchContext(
        environment: [String: String] = ProcessInfo.processInfo.environment
    ) -> (shellPath: String, shellArguments: [String]) {
        let shellPath = environment["ONIBI_PARENT_SHELL"]
            ?? environment["SHELL"]
            ?? "/bin/zsh"
        let shellArguments = SessionProxyLaunchConfiguration.parseShellArguments(
            environment["ONIBI_PARENT_SHELL_ARGS"] ?? ""
        )
        return (shellPath, shellArguments)
    }

    static func fallbackExecShell(
        environment: [String: String] = ProcessInfo.processInfo.environment
    ) -> Never {
        let context = fallbackLaunchContext(environment: environment)
        var sanitizedEnvironment = environment
        sanitizedEnvironment["ONIBI_SESSION_PROXY_ACTIVE"] = "1"
        sanitizedEnvironment["ONIBI_PARENT_SHELL"] = context.shellPath
        execShell(
            shellPath: context.shellPath,
            shellArguments: context.shellArguments,
            environment: sanitizedEnvironment
        )
    }

    private func startSources() {
        let stdinReadSource = DispatchSource.makeReadSource(fileDescriptor: STDIN_FILENO, queue: queue)
        stdinReadSource.setEventHandler { [weak self] in
            self?.handleReadableSTDIN()
        }
        stdinReadSource.setCancelHandler {}
        self.stdinReadSource = stdinReadSource
        stdinReadSource.resume()

        let ptyReadSource = DispatchSource.makeReadSource(fileDescriptor: ptyMasterFD, queue: queue)
        ptyReadSource.setEventHandler { [weak self] in
            self?.handleReadablePTY()
        }
        ptyReadSource.setCancelHandler {}
        self.ptyReadSource = ptyReadSource
        ptyReadSource.resume()

        let hostReadSource = DispatchSource.makeReadSource(fileDescriptor: hostSocketFD, queue: queue)
        hostReadSource.setEventHandler { [weak self] in
            self?.handleReadableHostSocket()
        }
        hostReadSource.setCancelHandler {}
        self.hostReadSource = hostReadSource
        hostReadSource.resume()

        let processSource = DispatchSource.makeProcessSource(identifier: childPID, eventMask: .exit, queue: queue)
        processSource.setEventHandler { [weak self] in
            self?.handleChildExit()
        }
        processSource.setCancelHandler {}
        self.processSource = processSource
        processSource.resume()

        let heartbeatTimer = DispatchSource.makeTimerSource(queue: queue)
        heartbeatTimer.schedule(deadline: .now() + .seconds(30), repeating: .seconds(30))
        heartbeatTimer.setEventHandler { [weak self] in
            self?.sendHeartbeat()
        }
        self.heartbeatTimer = heartbeatTimer
        heartbeatTimer.resume()

        Darwin.signal(SIGWINCH, SIG_IGN)
        let winchSource = DispatchSource.makeSignalSource(signal: SIGWINCH, queue: queue)
        winchSource.setEventHandler { [weak self] in
            self?.syncWindowSize()
        }
        self.winchSource = winchSource
        winchSource.resume()
    }

    private func handleReadableSTDIN() {
        guard !isShuttingDown else {
            return
        }

        do {
            let chunks = try readAvailable(from: STDIN_FILENO)
            if chunks.isEmpty {
                shutdown(exitCode: 0)
                return
            }

            for chunk in chunks {
                try writeAll(chunk, to: ptyMasterFD)
                inputByteCount += chunk.count
                lastInputAt = Date()
            }
        } catch {
            shutdown(exitCode: 1)
        }
    }

    private func handleReadablePTY() {
        guard !isShuttingDown else {
            return
        }

        do {
            let chunks = try readAvailable(from: ptyMasterFD)
            if chunks.isEmpty {
                return
            }

            for chunk in chunks {
                let timestamp = Date()
                try writeAll(chunk, to: STDOUT_FILENO)
                try sendFrame(
                    LocalSessionProxyOutputMessage(
                        sessionId: configuration.sessionId,
                        stream: .stdout,
                        timestamp: timestamp,
                        outputData: chunk
                    )
                )
                outputByteCount += chunk.count
                lastOutputAt = timestamp
                if let title = titleParser.consume(chunk).last {
                    sendTerminalTitleMetadataIfChanged(title)
                }
                for event in commandLifecycleParser.consume(chunk) {
                    sendCommandLifecycleEvent(event)
                }
                for event in terminalEventParser.consume(chunk) {
                    sendTerminalEvent(event)
                }
            }
        } catch {
            shutdown(exitCode: 1)
        }
    }

    private func handleReadableHostSocket() {
        guard !isShuttingDown else {
            return
        }

        do {
            let chunks = try readAvailable(from: hostSocketFD)
            if chunks.isEmpty {
                shutdown(exitCode: 1)
                return
            }

            for chunk in chunks {
                hostBuffer.append(chunk)
                let frames = RealtimeGatewayCodec.extractFrames(from: &hostBuffer)
                try frames.forEach(handleHostFrame)
            }
        } catch {
            shutdown(exitCode: 1)
        }
    }

    private func handleHostFrame(_ frameData: Data) throws {
        let envelope = try RealtimeGatewayCodec.decodeEnvelope(from: frameData)
        switch envelope.type {
        case .input:
            let message = try JSONDateCodec.decoder.decode(LocalSessionProxyInputMessage.self, from: frameData)
            let bytes = try RemoteInputByteTranslator.data(for: message.payload)
            try writeAll(bytes, to: ptyMasterFD)
            inputByteCount += bytes.count
            lastInputAt = Date()
            sendHealth()
        case .resize:
            let message = try JSONDateCodec.decoder.decode(LocalSessionProxyResizeMessage.self, from: frameData)
            let payload = message.payload
            guard payload.isValid else {
                throw SessionProxyRuntimeError.invalidResizePayload
            }
            applyWindowSize(cols: payload.cols, rows: payload.rows)
            sendTerminalMetadataIfChanged(
                RemoteTerminalResizePayload(cols: payload.cols, rows: payload.rows)
            )
        case .register, .metadata, .output, .commandStart, .commandEnd, .terminalEvent, .health, .state, .exit, .heartbeat:
            throw SessionProxyRuntimeError.invalidFrameType(envelope.type.rawValue)
        }
    }

    private func handleChildExit() {
        guard !isShuttingDown else {
            return
        }

        var status: Int32 = 0
        let waitResult = waitpid(childPID, &status, 0)
        let exitCode: Int32
        if waitResult == childPID {
            if childDidExitNormally(status) {
                exitCode = childExitStatus(status)
            } else if childWasSignaled(status) {
                exitCode = 128 + childTerminationSignal(status)
            } else {
                exitCode = 1
            }
        } else {
            exitCode = 1
        }

        do {
            try sendFrame(
                LocalSessionProxyExitMessage(
                    sessionId: configuration.sessionId,
                    exitCode: exitCode,
                    timestamp: Date()
                )
            )
        } catch {
            // Best effort; process is already exiting.
        }

        shutdown(exitCode: exitCode)
    }

    private func sendHeartbeat() {
        guard !isShuttingDown else {
            return
        }

        do {
            try sendFrame(
                LocalSessionProxyHeartbeatMessage(
                    sessionId: configuration.sessionId,
                    timestamp: Date()
                )
            )
            try sendFrame(currentHealthMessage(timestamp: Date()))
        } catch {
            shutdown(exitCode: 1)
        }
    }

    private func sendHealth() {
        guard !isShuttingDown else {
            return
        }

        do {
            try sendFrame(currentHealthMessage(timestamp: Date()))
        } catch {
            shutdown(exitCode: 1)
        }
    }

    private func shutdown(exitCode: Int32) {
        guard !isShuttingDown else {
            return
        }
        isShuttingDown = true

        stdinReadSource?.cancel()
        stdinReadSource = nil

        ptyReadSource?.cancel()
        ptyReadSource = nil

        hostReadSource?.cancel()
        hostReadSource = nil

        processSource?.cancel()
        processSource = nil

        heartbeatTimer?.cancel()
        heartbeatTimer = nil

        winchSource?.cancel()
        winchSource = nil

        closeIfNeeded(&ptyMasterFD)
        closeIfNeeded(&hostSocketFD)
        restoreParentTerminalIfNeeded()
        Darwin.exit(exitCode)
    }

    private func configureParentTerminal() throws {
        guard isatty(STDIN_FILENO) == 1 else {
            return
        }

        var terminalState = termios()
        guard tcgetattr(STDIN_FILENO, &terminalState) == 0 else {
            throw SessionProxyRuntimeError.posixFailure(function: "tcgetattr", code: errno)
        }
        originalTerminalState = terminalState

        var rawState = terminalState
        cfmakeraw(&rawState)
        guard tcsetattr(STDIN_FILENO, TCSANOW, &rawState) == 0 else {
            throw SessionProxyRuntimeError.posixFailure(function: "tcsetattr", code: errno)
        }
    }

    private func restoreParentTerminalIfNeeded() {
        guard var terminalState = originalTerminalState else {
            return
        }
        _ = tcsetattr(STDIN_FILENO, TCSANOW, &terminalState)
        originalTerminalState = nil
    }

    private func connectToHostSocket(path: String) throws -> Int32 {
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else {
            throw SessionProxyRuntimeError.posixFailure(function: "socket", code: errno)
        }

        do {
            try setNoSigPipe(fd)
            try withUnixSocketAddress(path: path) { pointer, length in
                guard connect(fd, pointer, length) == 0 else {
                    throw SessionProxyRuntimeError.posixFailure(function: "connect", code: errno)
                }
            }
            return fd
        } catch {
            var closeFD = fd
            closeIfNeeded(&closeFD)
            throw error
        }
    }

    private func spawnChildShell() throws -> Int32 {
        var masterFD: Int32 = -1
        let pid = forkpty(&masterFD, nil, nil, nil)
        if pid == -1 {
            throw SessionProxyRuntimeError.posixFailure(function: "forkpty", code: errno)
        }

        if pid == 0 {
            let environment = sanitizedEnvironment(from: ProcessInfo.processInfo.environment)
            if let workingDirectory = configuration.workingDirectory {
                _ = chdir(workingDirectory)
            }

            var argvStorage = makeCStringArray([configuration.shellPath] + configuration.shellArguments)
            var envStorage = makeCStringArray(environment.map { "\($0.key)=\($0.value)" })
            _ = configuration.shellPath.withCString { shellCString in
                argvStorage.withUnsafeMutableBufferPointer { argvBuffer in
                    envStorage.withUnsafeMutableBufferPointer { envBuffer in
                        execve(shellCString, argvBuffer.baseAddress, envBuffer.baseAddress)
                    }
                }
            }
            _exit(127)
        }

        childPID = pid
        try setNonBlocking(masterFD)
        return masterFD
    }

    private func syncWindowSize() {
        guard ptyMasterFD >= 0 else {
            return
        }

        guard let terminalSize = currentTerminalSize() else {
            return
        }

        var size = winsize()
        size.ws_col = UInt16(clamping: terminalSize.cols)
        size.ws_row = UInt16(clamping: terminalSize.rows)
        _ = ioctl(ptyMasterFD, TIOCSWINSZ, &size)
        sendTerminalMetadataIfChanged(terminalSize)
    }

    private func applyWindowSize(cols: Int, rows: Int) {
        guard ptyMasterFD >= 0 else {
            return
        }

        var size = winsize()
        size.ws_col = UInt16(clamping: cols)
        size.ws_row = UInt16(clamping: rows)
        _ = ioctl(ptyMasterFD, TIOCSWINSZ, &size)
    }

    private func currentTerminalSize() -> RemoteTerminalResizePayload? {
        var size = winsize()
        guard ioctl(STDIN_FILENO, TIOCGWINSZ, &size) == 0 else {
            return nil
        }
        let cols = Int(size.ws_col)
        let rows = Int(size.ws_row)
        guard cols > 0, rows > 0 else {
            return nil
        }
        return RemoteTerminalResizePayload(cols: cols, rows: rows)
    }

    private func sendTerminalMetadataIfChanged(_ size: RemoteTerminalResizePayload) {
        guard lastReportedTerminalSize != size else {
            return
        }
        lastReportedTerminalSize = size
        do {
            try sendFrame(
                LocalSessionProxyMetadataMessage(
                    sessionId: configuration.sessionId,
                    workingDirectory: configuration.workingDirectory,
                    terminalCols: size.cols,
                    terminalRows: size.rows
                )
            )
        } catch {
            shutdown(exitCode: 1)
        }
    }

    private func sendTerminalTitleMetadataIfChanged(_ title: String) {
        guard lastReportedTerminalTitle != title else {
            return
        }
        lastReportedTerminalTitle = title
        do {
            try sendFrame(
                LocalSessionProxyMetadataMessage(
                    sessionId: configuration.sessionId,
                    terminalTitle: title
                )
            )
        } catch {
            shutdown(exitCode: 1)
        }
    }

    private func sendCommandLifecycleEvent(_ event: TerminalCommandLifecycleEvent) {
        do {
            switch event {
            case .start(let command, let workingDirectory):
                try sendFrame(
                    LocalSessionProxyCommandStartMessage(
                        sessionId: configuration.sessionId,
                        command: command,
                        workingDirectory: workingDirectory,
                        timestamp: Date()
                    )
                )
            case .end(let exitCode, let workingDirectory):
                try sendFrame(
                    LocalSessionProxyCommandEndMessage(
                        sessionId: configuration.sessionId,
                        exitCode: exitCode,
                        workingDirectory: workingDirectory,
                        timestamp: Date()
                    )
                )
            }
        } catch {
            shutdown(exitCode: 1)
        }
    }

    private func sendTerminalEvent(_ event: TerminalEvent) {
        do {
            switch event {
            case .bell:
                try sendFrame(
                    LocalSessionProxyTerminalEventMessage(
                        sessionId: configuration.sessionId,
                        event: .bell,
                        timestamp: Date()
                    )
                )
            case .workingDirectory(let workingDirectory):
                try sendFrame(
                    LocalSessionProxyTerminalEventMessage(
                        sessionId: configuration.sessionId,
                        event: .workingDirectory,
                        value: workingDirectory,
                        timestamp: Date()
                    )
                )
            }
        } catch {
            shutdown(exitCode: 1)
        }
    }

    private func currentHealthMessage(timestamp: Date) -> LocalSessionProxyHealthMessage {
        let canReceiveInput = ptyMasterFD >= 0 && !isShuttingDown
        let flowControl: SessionFlowControlState
        if !canReceiveInput {
            flowControl = .blocked
        } else if droppedOutputByteCount > 0 {
            flowControl = .limited
        } else {
            flowControl = .open
        }
        return LocalSessionProxyHealthMessage(
            sessionId: configuration.sessionId,
            timestamp: timestamp,
            canReceiveInput: canReceiveInput,
            flowControl: flowControl,
            inputByteCount: inputByteCount,
            outputByteCount: outputByteCount,
            droppedOutputByteCount: droppedOutputByteCount,
            lastInputAt: lastInputAt,
            lastOutputAt: lastOutputAt
        )
    }

    private func sendFrame<T: Encodable>(_ frame: T) throws {
        let data = try RealtimeGatewayCodec.encodeFrame(frame)
        try writeAll(data, to: hostSocketFD)
    }

    private func sanitizedEnvironment(from environment: [String: String]) -> [String: String] {
        var copy = environment
        copy["ONIBI_SESSION_PROXY_ACTIVE"] = "1"
        copy["TERM_SESSION_ID"] = configuration.sessionId
        copy["ONIBI_PARENT_SHELL"] = configuration.shellPath
        copy["ONIBI_PROXY_VERSION"] = configuration.version
        return copy
    }

    private static func execShell(
        shellPath: String,
        shellArguments: [String],
        environment: [String: String]
    ) -> Never {
        var argvStorage = makeCStringArray([shellPath] + shellArguments)
        var envStorage = makeCStringArray(environment.map { "\($0.key)=\($0.value)" })

        let result = shellPath.withCString { shellCString in
            argvStorage.withUnsafeMutableBufferPointer { argvBuffer in
                envStorage.withUnsafeMutableBufferPointer { envBuffer in
                    execve(shellCString, argvBuffer.baseAddress, envBuffer.baseAddress)
                }
            }
        }

        let code = result == -1 ? errno : 127
        fputs("OnibiSessionProxy fallback exec failed: \(String(cString: strerror(code)))\n", stderr)
        Darwin.exit(code)
    }
}

private func readAvailable(from fileDescriptor: Int32) throws -> [Data] {
    var chunks: [Data] = []

    while true {
        var buffer = [UInt8](repeating: 0, count: 4096)
        let byteCount = Darwin.read(fileDescriptor, &buffer, buffer.count)

        if byteCount > 0 {
            chunks.append(Data(buffer.prefix(Int(byteCount))))
            if byteCount < buffer.count {
                break
            }
        } else if byteCount == 0 {
            return chunks
        } else if errno == EAGAIN || errno == EWOULDBLOCK {
            return chunks
        } else {
            throw SessionProxyRuntimeError.posixFailure(function: "read", code: errno)
        }
    }

    return chunks
}

private func writeAll(_ data: Data, to fileDescriptor: Int32) throws {
    try data.withUnsafeBytes { rawBuffer in
        guard let baseAddress = rawBuffer.baseAddress else {
            return
        }

        var offset = 0
        while offset < rawBuffer.count {
            let pointer = baseAddress.advanced(by: offset)
            let written = Darwin.write(fileDescriptor, pointer, rawBuffer.count - offset)
            if written > 0 {
                offset += written
            } else if written == -1 && (errno == EAGAIN || errno == EWOULDBLOCK) {
                continue
            } else {
                throw SessionProxyRuntimeError.posixFailure(function: "write", code: errno)
            }
        }
    }
}

private func setNonBlocking(_ fileDescriptor: Int32) throws {
    let currentFlags = fcntl(fileDescriptor, F_GETFL)
    guard currentFlags != -1 else {
        throw SessionProxyRuntimeError.posixFailure(function: "fcntl(F_GETFL)", code: errno)
    }
    guard fcntl(fileDescriptor, F_SETFL, currentFlags | O_NONBLOCK) == 0 else {
        throw SessionProxyRuntimeError.posixFailure(function: "fcntl(F_SETFL)", code: errno)
    }
}

private func setNoSigPipe(_ fileDescriptor: Int32) throws {
    var value: Int32 = 1
    let result = withUnsafePointer(to: &value) {
        setsockopt(fileDescriptor, SOL_SOCKET, SO_NOSIGPIPE, $0, socklen_t(MemoryLayout<Int32>.size))
    }
    guard result == 0 else {
        throw SessionProxyRuntimeError.posixFailure(function: "setsockopt(SO_NOSIGPIPE)", code: errno)
    }
}

private func withUnixSocketAddress<Result>(
    path: String,
    _ body: (UnsafePointer<sockaddr>, socklen_t) throws -> Result
) throws -> Result {
    var address = sockaddr_un()
    let pathLength = path.utf8.count
    let maxPathLength = MemoryLayout.size(ofValue: address.sun_path)
    guard pathLength < maxPathLength else {
        throw SessionProxyRuntimeError.unixSocketPathTooLong(path)
    }

    address.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
    address.sun_family = sa_family_t(AF_UNIX)
    withUnsafeMutableBytes(of: &address.sun_path) { rawBuffer in
        guard let baseAddress = rawBuffer.baseAddress else {
            return
        }
        let buffer = baseAddress.assumingMemoryBound(to: CChar.self)
        path.withCString { source in
            strncpy(buffer, source, rawBuffer.count - 1)
            buffer[rawBuffer.count - 1] = 0
        }
    }

    let length = socklen_t(MemoryLayout<sockaddr_un>.size)
    return try withUnsafePointer(to: &address) { pointer in
        try pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockaddrPointer in
            try body(sockaddrPointer, length)
        }
    }
}

private func closeIfNeeded(_ fileDescriptor: inout Int32) {
    guard fileDescriptor >= 0 else {
        return
    }
    _ = Darwin.close(fileDescriptor)
    fileDescriptor = -1
}

private func childDidExitNormally(_ status: Int32) -> Bool {
    (status & 0x7F) == 0
}

private func childExitStatus(_ status: Int32) -> Int32 {
    (status >> 8) & 0xFF
}

private func childWasSignaled(_ status: Int32) -> Bool {
    let signal = status & 0x7F
    return signal != 0 && signal != 0x7F
}

private func childTerminationSignal(_ status: Int32) -> Int32 {
    status & 0x7F
}

private func makeCStringArray(_ strings: [String]) -> [UnsafeMutablePointer<CChar>?] {
    let pointers = strings.map { strdup($0) }
    return pointers + [nil]
}
