import Foundation

let configuration: SessionProxyLaunchConfiguration
do {
    configuration = try SessionProxyLaunchConfiguration()
} catch {
    fputs("OnibiSessionProxy configuration error: \(error.localizedDescription)\n", stderr)
    Darwin.exit(1)
}

let runtime = SessionProxyRuntime(configuration: configuration)
do {
    try runtime.start()
    dispatchMain()
} catch {
    fputs("OnibiSessionProxy startup failed: \(error.localizedDescription)\n", stderr)
    runtime.fallbackExecShell()
}
