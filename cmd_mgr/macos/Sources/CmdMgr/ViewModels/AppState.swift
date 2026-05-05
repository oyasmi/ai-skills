import Foundation
import SwiftUI
import Combine
import AppKit

/// Central application state – owns the database and process manager.
final class AppState: ObservableObject {

    let database = Database()
    let processManager = ProcessManager()
    private var cancellables: Set<AnyCancellable> = []
    private var outputWindows: [Int: NSWindow] = [:]

    @Published var commands: [Command] = []
    @Published var showAddEditSheet = false
    @Published var editingCommand: Command? = nil
    @Published var searchText: String = ""

    var filteredCommands: [Command] {
        guard !searchText.isEmpty else { return commands }
        let q = searchText.lowercased()
        return commands.filter {
            $0.name.lowercased().contains(q) || $0.command.lowercased().contains(q)
        }
    }

    init() {
        processManager.objectWillChange
            .receive(on: DispatchQueue.main)
            .sink { [weak self] _ in self?.objectWillChange.send() }
            .store(in: &cancellables)
        loadCommands()
    }

    func loadCommands() {
        commands = database.getAllCommands()
    }

    // MARK: - Command CRUD

    func addCommand(name: String, command: String, cmdType: CommandType, workingDirectory: String?) {
        database.addCommand(name: name, command: command, cmdType: cmdType, workingDirectory: workingDirectory)
        loadCommands()
    }

    func updateCommand(id: Int, name: String, command: String, cmdType: CommandType, workingDirectory: String?) {
        database.updateCommand(id: id, name: name, command: command, cmdType: cmdType, workingDirectory: workingDirectory)
        loadCommands()
    }

    func deleteCommand(_ command: Command) {
        if processManager.isRunning(command.id) {
            processManager.stop(commandId: command.id, database: database)
        }
        database.deleteCommand(id: command.id)
        loadCommands()
    }

    // MARK: - Process Actions

    func toggleLongRunning(_ command: Command) {
        if processManager.isRunning(command.id) {
            processManager.stop(commandId: command.id, database: database)
        } else {
            processManager.start(command: command, database: database)
        }
    }

    func executeOneShot(_ command: Command) {
        guard !processManager.isRunning(command.id) else { return }
        processManager.start(command: command, database: database)
        if let info = processManager.runningProcesses[command.id] {
            openOutputWindow(commandId: command.id, title: command.name, processInfo: info)
        }
    }

    func showOutput(_ command: Command) {
        if let info = processManager.processInfo(command.id) {
            openOutputWindow(commandId: command.id, title: command.name, processInfo: info)
        }
    }

    func showHistory(_ command: Command) {
        let history = database.getHistory(forCommandId: command.id)
        openHistoryWindow(title: command.name, history: history)
    }

    func openAddSheet() {
        editingCommand = nil
        showAddEditSheet = true
    }

    func openEditSheet(_ command: Command) {
        editingCommand = command
        showAddEditSheet = true
    }

    // MARK: - Window Management

    private func openOutputWindow(commandId: Int, title: String, processInfo: CommandProcess) {
        // Reuse existing window if still open.
        if let existing = outputWindows[commandId], existing.isVisible {
            existing.makeKeyAndOrderFront(nil)
            return
        }

        let view = OutputView(processInfo: processInfo)
        let hostingView = NSHostingView(rootView: view)

        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 700, height: 500),
            styleMask: [.titled, .closable, .resizable, .miniaturizable],
            backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.title = "Output: \(title)"
        window.contentView = hostingView
        window.center()
        window.makeKeyAndOrderFront(nil)

        outputWindows[commandId] = window

        NotificationCenter.default.addObserver(
            forName: NSWindow.willCloseNotification, object: window, queue: .main
        ) { [weak self] _ in
            self?.outputWindows.removeValue(forKey: commandId)
        }
    }

    private func openHistoryWindow(title: String, history: [CommandHistory]) {
        let view = HistoryView(history: history)
        let hostingView = NSHostingView(rootView: view)

        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 700, height: 500),
            styleMask: [.titled, .closable, .resizable, .miniaturizable],
            backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.title = "History: \(title)"
        window.contentView = hostingView
        window.center()
        window.makeKeyAndOrderFront(nil)
    }
}
