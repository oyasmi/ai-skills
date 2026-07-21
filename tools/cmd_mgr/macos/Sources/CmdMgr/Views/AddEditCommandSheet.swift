import SwiftUI
import AppKit

struct AddEditCommandSheet: View {
    @EnvironmentObject var appState: AppState
    @Environment(\.dismiss) private var dismiss

    let command: Command?
    private var isEdit: Bool { command != nil }

    @State private var name = ""
    @State private var commandText = ""
    @State private var cmdType: CommandType = .oneShot
    @State private var workingDirectory = ""
    @FocusState private var focusedField: Field?

    private enum Field { case name, command }

    init(command: Command?) {
        self.command = command
        if let command {
            _name = State(initialValue: command.name)
            _commandText = State(initialValue: command.command)
            _cmdType = State(initialValue: command.cmdType)
            _workingDirectory = State(initialValue: command.workingDirectory ?? "")
        }
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                Image(systemName: isEdit ? "pencil" : "plus")
                    .font(.system(size: 18, weight: .semibold))
                    .foregroundStyle(.tint)
                    .frame(width: 36, height: 36)
                    .background(.tint.opacity(0.1), in: RoundedRectangle(cornerRadius: 9))
                VStack(alignment: .leading, spacing: 2) {
                    Text(isEdit ? "Edit Command" : "New Command")
                        .font(.title2.weight(.semibold))
                    Text("Commands run with /bin/sh.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                Spacer()
            }
            .padding(24)

            Divider()

            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    fieldSection("Name") {
                        TextField("Start development server", text: $name)
                            .textFieldStyle(.roundedBorder)
                            .focused($focusedField, equals: .name)
                    }

                    fieldSection("Type") {
                        Picker("Type", selection: $cmdType) {
                            ForEach(CommandType.allCases) { type in
                                Text(type.displayName).tag(type)
                            }
                        }
                        .pickerStyle(.segmented)
                        .labelsHidden()

                        Text(cmdType == .longRunning
                             ? "For servers and background processes. The command keeps running until you stop it."
                             : "For scripts and tasks that finish on their own.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }

                    fieldSection("Command") {
                        ZStack(alignment: .topLeading) {
                            if commandText.isEmpty {
                                Text("npm run dev")
                                    .font(.system(.body, design: .monospaced))
                                    .foregroundStyle(.tertiary)
                                    .padding(.horizontal, 6)
                                    .padding(.vertical, 8)
                            }
                            TextEditor(text: $commandText)
                                .font(.system(.body, design: .monospaced))
                                .scrollContentBackground(.hidden)
                                .padding(2)
                                .focused($focusedField, equals: .command)
                        }
                        .frame(minHeight: 105)
                        .background(Color(nsColor: .textBackgroundColor),
                                    in: RoundedRectangle(cornerRadius: 7))
                        .overlay {
                            RoundedRectangle(cornerRadius: 7)
                                .stroke(.separator, lineWidth: 1)
                        }
                    }

                    fieldSection("Working Directory") {
                        HStack(spacing: 8) {
                            TextField("Application default", text: $workingDirectory)
                                .textFieldStyle(.roundedBorder)
                            Button("Choose…", action: browseDirectory)
                        }
                        if let directoryError {
                            Label(directoryError, systemImage: "exclamationmark.circle.fill")
                                .font(.caption)
                                .foregroundStyle(.red)
                        } else {
                            Text("Leave empty to inherit the app's working directory.")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                .padding(24)
            }

            Divider()

            HStack {
                if !requiredFieldsComplete {
                    Text("Name and command are required.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Button("Cancel") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button(isEdit ? "Save Changes" : "Add Command", action: save)
                    .keyboardShortcut(.defaultAction)
                    .buttonStyle(.borderedProminent)
                    .disabled(!canSave)
            }
            .padding(16)
        }
        .frame(width: 560)
        .frame(minHeight: 610)
        .onAppear { focusedField = .name }
    }

    private func fieldSection<Content: View>(_ title: String,
                                             @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(title)
                .font(.subheadline.weight(.medium))
            content()
        }
    }

    private var trimmedName: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var trimmedCommand: String {
        commandText.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var trimmedDirectory: String {
        workingDirectory.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private var requiredFieldsComplete: Bool {
        !trimmedName.isEmpty && !trimmedCommand.isEmpty
    }

    private var directoryError: String? {
        guard !trimmedDirectory.isEmpty else { return nil }
        var isDirectory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: trimmedDirectory, isDirectory: &isDirectory),
              isDirectory.boolValue else {
            return "Choose an existing directory."
        }
        return nil
    }

    private var canSave: Bool {
        requiredFieldsComplete && directoryError == nil
    }

    private func browseDirectory() {
        let panel = NSOpenPanel()
        panel.canChooseFiles = false
        panel.canChooseDirectories = true
        panel.allowsMultipleSelection = false
        panel.prompt = "Choose"
        if directoryError == nil, !trimmedDirectory.isEmpty {
            panel.directoryURL = URL(fileURLWithPath: trimmedDirectory)
        }
        if panel.runModal() == .OK, let url = panel.url {
            workingDirectory = url.path
        }
    }

    private func save() {
        guard canSave else { return }
        let directory = trimmedDirectory.isEmpty ? nil : trimmedDirectory
        if let command {
            appState.updateCommand(id: command.id, name: trimmedName,
                                   command: trimmedCommand, cmdType: cmdType,
                                   workingDirectory: directory)
        } else {
            appState.addCommand(name: trimmedName, command: trimmedCommand,
                                cmdType: cmdType, workingDirectory: directory)
        }
        dismiss()
    }
}
