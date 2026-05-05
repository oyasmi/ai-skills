import SwiftUI
import AppKit

/// Sheet for adding or editing a command.
struct AddEditCommandSheet: View {
    @EnvironmentObject var appState: AppState
    @Environment(\.dismiss) private var dismiss

    let command: Command?
    private var isEdit: Bool { command != nil }

    @State private var name: String = ""
    @State private var commandText: String = ""
    @State private var cmdType: CommandType = .oneShot
    @State private var workingDirectory: String = ""
    @State private var showValidationError = false

    init(command: Command?) {
        self.command = command
        if let cmd = command {
            _name = State(initialValue: cmd.name)
            _commandText = State(initialValue: cmd.command)
            _cmdType = State(initialValue: cmd.cmdType)
            _workingDirectory = State(initialValue: cmd.workingDirectory ?? "")
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            // Title
            Text(isEdit ? "Edit Command" : "New Command")
                .font(.title2)
                .fontWeight(.bold)

            // Name field
            VStack(alignment: .leading, spacing: 4) {
                Text("Name")
                    .font(.subheadline)
                    .foregroundColor(.secondary)
                TextField("e.g. Start Dev Server", text: $name)
                    .textFieldStyle(.roundedBorder)
            }

            // Command field
            VStack(alignment: .leading, spacing: 4) {
                Text("Command")
                    .font(.subheadline)
                    .foregroundColor(.secondary)
                TextEditor(text: $commandText)
                    .font(.system(size: 12, design: .monospaced))
                    .frame(minHeight: 80, maxHeight: 120)
                    .overlay(
                        RoundedRectangle(cornerRadius: 6)
                            .stroke(Color.secondary.opacity(0.3), lineWidth: 1)
                    )
            }

            // Working directory field
            VStack(alignment: .leading, spacing: 4) {
                Text("Working Directory")
                    .font(.subheadline)
                    .foregroundColor(.secondary)
                HStack(spacing: 6) {
                    TextField("Default (inherit from app)", text: $workingDirectory)
                        .textFieldStyle(.roundedBorder)
                    Button("Browse…") { browseDirectory() }
                }
            }

            // Type selection
            VStack(alignment: .leading, spacing: 4) {
                Text("Type")
                    .font(.subheadline)
                    .foregroundColor(.secondary)
                Picker("Type", selection: $cmdType) {
                    ForEach(CommandType.allCases) { type in
                        Text(type.displayName).tag(type)
                    }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
            }

            // Buttons
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }
                    .keyboardShortcut(.cancelAction)

                Button(isEdit ? "Save" : "Add") { save() }
                    .keyboardShortcut(.defaultAction)
                    .buttonStyle(.borderedProminent)
            }
        }
        .padding(24)
        .frame(width: 480)
        .alert("Validation Error", isPresented: $showValidationError) {
            Button("OK") {}
        } message: {
            Text("Name and Command fields cannot be empty.")
        }
    }

    private func browseDirectory() {
        let panel = NSOpenPanel()
        panel.canChooseFiles = false
        panel.canChooseDirectories = true
        panel.allowsMultipleSelection = false
        if !workingDirectory.isEmpty {
            panel.directoryURL = URL(fileURLWithPath: workingDirectory)
        }
        if panel.runModal() == .OK, let url = panel.url {
            workingDirectory = url.path
        }
    }

    private func save() {
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedCmd = commandText.trimmingCharacters(in: .whitespacesAndNewlines)
        let trimmedWD = workingDirectory.trimmingCharacters(in: .whitespacesAndNewlines)

        guard !trimmedName.isEmpty, !trimmedCmd.isEmpty else {
            showValidationError = true
            return
        }

        let wd: String? = trimmedWD.isEmpty ? nil : trimmedWD

        if isEdit, let cmd = command {
            appState.updateCommand(id: cmd.id, name: trimmedName, command: trimmedCmd,
                                   cmdType: cmdType, workingDirectory: wd)
        } else {
            appState.addCommand(name: trimmedName, command: trimmedCmd,
                                cmdType: cmdType, workingDirectory: wd)
        }
        dismiss()
    }
}
