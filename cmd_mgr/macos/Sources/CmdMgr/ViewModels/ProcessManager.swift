import Foundation

/// Information about a running (or recently completed) process.
final class CommandProcess: ObservableObject, Identifiable {
    let commandId: Int
    let process: Process
    let historyId: Int

    @Published var output: String = ""
    @Published var isRunning: Bool = true

    private static let maxLines = 5000

    init(commandId: Int, process: Process, historyId: Int) {
        self.commandId = commandId
        self.process = process
        self.historyId = historyId
    }

    func appendOutput(_ text: String) {
        output.append(text)
        // Trim to avoid unbounded memory growth for long-running services.
        let lines = output.components(separatedBy: "\n")
        if lines.count > Self.maxLines {
            output = lines.suffix(Self.maxLines).joined(separator: "\n")
        }
    }
}

/// Manages subprocess lifecycle: start, stop, output capture.
final class ProcessManager: ObservableObject {

    @Published var runningProcesses: [Int: CommandProcess] = [:]
    // Keeps last completed run per command so output remains viewable after exit.
    @Published var completedProcesses: [Int: CommandProcess] = [:]

    private var requestedStops: Set<Int> = []

    func isRunning(_ commandId: Int) -> Bool {
        runningProcesses[commandId]?.isRunning ?? false
    }

    func hasOutput(_ commandId: Int) -> Bool {
        let info = runningProcesses[commandId] ?? completedProcesses[commandId]
        return !(info?.output.isEmpty ?? true)
    }

    func processInfo(_ commandId: Int) -> CommandProcess? {
        runningProcesses[commandId] ?? completedProcesses[commandId]
    }

    /// Start a command as a subprocess.
    func start(command: Command, database: Database) {
        guard !isRunning(command.id) else { return }

        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: "/bin/sh")
        proc.arguments = ["-c", command.command]
        if let wd = command.workingDirectory, !wd.isEmpty {
            proc.currentDirectoryURL = URL(fileURLWithPath: wd)
        }

        let outPipe = Pipe()
        let errPipe = Pipe()
        proc.standardOutput = outPipe
        proc.standardError = errPipe

        let historyId = database.addHistoryEntry(
            commandId: command.id, startTime: Date(), status: "running")

        let info = CommandProcess(commandId: command.id, process: proc, historyId: historyId)

        outPipe.fileHandleForReading.readabilityHandler = { [weak info] handle in
            let data = handle.availableData
            guard !data.isEmpty, let str = String(data: data, encoding: .utf8) else { return }
            DispatchQueue.main.async { info?.appendOutput(str) }
        }

        errPipe.fileHandleForReading.readabilityHandler = { [weak info] handle in
            let data = handle.availableData
            guard !data.isEmpty, let str = String(data: data, encoding: .utf8) else { return }
            DispatchQueue.main.async { info?.appendOutput("[STDERR] \(str)") }
        }

        proc.terminationHandler = { [weak self, weak info] proc in
            guard let self = self, let info = info else { return }
            outPipe.fileHandleForReading.readabilityHandler = nil
            errPipe.fileHandleForReading.readabilityHandler = nil
            let remainOut = outPipe.fileHandleForReading.readDataToEndOfFile()
            let remainErr = errPipe.fileHandleForReading.readDataToEndOfFile()

            DispatchQueue.main.async {
                let wasRequestedStop = self.requestedStops.remove(info.commandId) != nil
                let status = wasRequestedStop ? "terminated"
                    : (proc.terminationStatus == 0 ? "success" : "failed")
                if let s = String(data: remainOut, encoding: .utf8), !s.isEmpty {
                    info.appendOutput(s)
                }
                if let s = String(data: remainErr, encoding: .utf8), !s.isEmpty {
                    info.appendOutput("[STDERR] \(s)")
                }
                info.isRunning = false
                database.updateHistoryEntry(
                    id: info.historyId, endTime: Date(), status: status, output: info.output)
                self.runningProcesses.removeValue(forKey: info.commandId)
                self.completedProcesses[info.commandId] = info
            }
        }

        do {
            completedProcesses.removeValue(forKey: command.id)
            runningProcesses[command.id] = info
            try proc.run()
        } catch {
            runningProcesses.removeValue(forKey: command.id)
            database.updateHistoryEntry(
                id: historyId, endTime: Date(), status: "failed",
                output: "Launch error: \(error.localizedDescription)")
        }
    }

    /// Stop a running command. Only sends signals; cleanup is done in terminationHandler.
    func stop(commandId: Int, database: Database) {
        guard let info = runningProcesses[commandId] else { return }
        requestedStops.insert(commandId)
        info.process.terminate()  // SIGTERM – give process a chance to clean up

        // Escalate to SIGKILL after 3 s if still alive.
        DispatchQueue.global().asyncAfter(deadline: .now() + 3) { [weak info] in
            guard let info = info, info.process.isRunning else { return }
            Foundation.kill(Int32(info.process.processIdentifier), SIGKILL)
        }
    }

    /// Stop all running processes.
    func stopAll(database: Database) {
        for cmdId in runningProcesses.keys {
            stop(commandId: cmdId, database: database)
        }
    }

    /// Blocks the calling (background) thread until all processes in `infos` have exited,
    /// then waits briefly for terminationHandler dispatches to drain on the main queue.
    func waitForAll(processes: [CommandProcess]) {
        for info in processes {
            // Safety kill after 3 s in case SIGTERM was ignored.
            DispatchQueue.global().asyncAfter(deadline: .now() + 3) { [weak info] in
                guard let info = info, info.process.isRunning else { return }
                Foundation.kill(Int32(info.process.processIdentifier), SIGKILL)
            }
            info.process.waitUntilExit()
        }
        // Allow terminationHandler's DispatchQueue.main.async closures to flush.
        Thread.sleep(forTimeInterval: 0.2)
    }
}
