import Foundation
import SwiftUI
import Combine

enum CommandFilter: String, CaseIterable, Identifiable {
    case all
    case running
    case longRunning
    case oneShot

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .all: return "All Commands"
        case .running: return "Running"
        case .longRunning: return "Long-running"
        case .oneShot: return "One-shot"
        }
    }
}

enum CommandDetailTab: String, CaseIterable, Identifiable {
    case overview
    case output
    case history

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .overview: return "Overview"
        case .output: return "Output"
        case .history: return "History"
        }
    }
}

/// Central application state – owns persistence, process lifecycle, and UI selection.
final class AppState: ObservableObject {
    let database = Database()
    let processManager = ProcessManager()
    private var cancellables: Set<AnyCancellable> = []

    @Published var commands: [Command] = []
    @Published var showAddEditSheet = false
    @Published var editingCommand: Command?
    @Published var searchText = ""
    @Published var commandFilter: CommandFilter = .all
    @Published var selectedCommandID: Int?
    @Published var selectedDetailTab: CommandDetailTab = .overview

    var filteredCommands: [Command] {
        let query = searchText.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        return commands.filter { command in
            let matchesQuery = query.isEmpty
                || command.name.lowercased().contains(query)
                || command.command.lowercased().contains(query)
                || (command.workingDirectory?.lowercased().contains(query) ?? false)

            let matchesFilter: Bool
            switch commandFilter {
            case .all:
                matchesFilter = true
            case .running:
                matchesFilter = processManager.isRunning(command.id)
            case .longRunning:
                matchesFilter = command.cmdType == .longRunning
            case .oneShot:
                matchesFilter = command.cmdType == .oneShot
            }
            return matchesQuery && matchesFilter
        }
    }

    var selectedCommand: Command? {
        commands.first { $0.id == selectedCommandID }
    }

    var errorMessage: String? { processManager.lastError }

    init() {
        processManager.objectWillChange
            .receive(on: DispatchQueue.main)
            .sink { [weak self] _ in self?.objectWillChange.send() }
            .store(in: &cancellables)
        loadCommands()
        selectedCommandID = commands.first?.id
    }

    func loadCommands() {
        commands = database.getAllCommands()
        if let selectedCommandID,
           !commands.contains(where: { $0.id == selectedCommandID }) {
            self.selectedCommandID = commands.first?.id
        }
    }

    // MARK: - Command CRUD

    func addCommand(name: String, command: String, cmdType: CommandType, workingDirectory: String?) {
        database.addCommand(name: name, command: command, cmdType: cmdType,
                            workingDirectory: workingDirectory)
        loadCommands()
        selectedCommandID = commands.first?.id
        selectedDetailTab = .overview
    }

    func updateCommand(id: Int, name: String, command: String, cmdType: CommandType,
                       workingDirectory: String?) {
        database.updateCommand(id: id, name: name, command: command, cmdType: cmdType,
                               workingDirectory: workingDirectory)
        loadCommands()
        selectedCommandID = id
    }

    func duplicateCommand(_ command: Command) {
        addCommand(name: "\(command.name) Copy", command: command.command,
                   cmdType: command.cmdType, workingDirectory: command.workingDirectory)
        openEditSheet(selectedCommand ?? command)
    }

    func deleteCommand(_ command: Command) {
        if processManager.isRunning(command.id) {
            processManager.stop(commandId: command.id, database: database)
        }
        database.deleteCommand(id: command.id)
        loadCommands()
    }

    // MARK: - Process Actions

    func run(_ command: Command) {
        selectedCommandID = command.id
        selectedDetailTab = .output
        if command.cmdType == .longRunning, processManager.isRunning(command.id) {
            processManager.stop(commandId: command.id, database: database)
        } else if !processManager.isRunning(command.id) {
            processManager.start(command: command, database: database)
        }
    }

    func runSelectedCommand() {
        guard let selectedCommand else { return }
        run(selectedCommand)
    }

    func showOutput(_ command: Command) {
        selectedCommandID = command.id
        selectedDetailTab = .output
    }

    func showHistory(_ command: Command) {
        selectedCommandID = command.id
        selectedDetailTab = .history
    }

    func history(for command: Command) -> [CommandHistory] {
        database.getHistory(forCommandId: command.id)
    }

    func dismissError() {
        processManager.lastError = nil
    }

    func openAddSheet() {
        editingCommand = nil
        showAddEditSheet = true
    }

    func openEditSheet(_ command: Command) {
        editingCommand = command
        showAddEditSheet = true
    }
}
