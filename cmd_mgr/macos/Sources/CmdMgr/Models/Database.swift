import Foundation
import CSQLite

// MARK: - Database Error

enum DatabaseError: Error, LocalizedError {
    case openFailed(String)
    case queryFailed(String)

    var errorDescription: String? {
        switch self {
        case .openFailed(let msg): return "Database open failed: \(msg)"
        case .queryFailed(let msg): return "Query failed: \(msg)"
        }
    }
}

// MARK: - Database

/// SQLite database wrapper – schema-compatible with the Python CmdMgr implementation.
final class Database {

    private var db: OpaquePointer?

    // Compatible with Python's datetime format used in the original app.
    private static let dateFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm:ss.SSSSSS"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f
    }()

    // Fallback parser without microseconds (CURRENT_TIMESTAMP format).
    private static let dateFormatterShort: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm:ss"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f
    }()

    init() {
        let fileManager = FileManager.default
        let configDir: URL
        if let appSupport = fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first {
            configDir = appSupport.appendingPathComponent("CmdMgr")
        } else {
            configDir = fileManager.homeDirectoryForCurrentUser
                .appendingPathComponent(".config/CmdMgr")
        }

        try? fileManager.createDirectory(at: configDir, withIntermediateDirectories: true)
        let dbPath = configDir.appendingPathComponent("cmd_mgr.db").path
        let legacyDbURL = fileManager.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/CmdMgr/cmd_mgr.db")

        migrateLegacyDatabaseIfNeeded(to: URL(fileURLWithPath: dbPath), from: legacyDbURL)

        guard sqlite3_open(dbPath, &db) == SQLITE_OK else {
            let msg = db.flatMap { String(cString: sqlite3_errmsg($0)) } ?? "unknown"
            fatalError("Cannot open database: \(msg)")
        }

        sqlite3_exec(db, "PRAGMA journal_mode=WAL", nil, nil, nil)
        sqlite3_exec(db, "PRAGMA foreign_keys=ON", nil, nil, nil)
        createTables()
        migrateSchema()
    }

    deinit {
        sqlite3_close(db)
    }

    // MARK: - Helpers

    private func migrateLegacyDatabaseIfNeeded(to newDbURL: URL, from legacyDbURL: URL) {
        let fileManager = FileManager.default
        guard !fileManager.fileExists(atPath: newDbURL.path),
              fileManager.fileExists(atPath: legacyDbURL.path) else { return }

        try? fileManager.copyItem(at: legacyDbURL, to: newDbURL)

        for suffix in ["-wal", "-shm"] {
            let legacySidecar = URL(fileURLWithPath: legacyDbURL.path + suffix)
            let newSidecar = URL(fileURLWithPath: newDbURL.path + suffix)
            if fileManager.fileExists(atPath: legacySidecar.path),
               !fileManager.fileExists(atPath: newSidecar.path) {
                try? fileManager.copyItem(at: legacySidecar, to: newSidecar)
            }
        }
    }

    private func createTables() {
        let sqls = [
            """
            CREATE TABLE IF NOT EXISTS commands (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                command TEXT NOT NULL,
                cmd_type TEXT NOT NULL,
                created_at DATETIME DEFAULT CURRENT_TIMESTAMP
            )
            """,
            """
            CREATE TABLE IF NOT EXISTS command_history (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                command_id INTEGER NOT NULL,
                start_time DATETIME NOT NULL,
                end_time DATETIME,
                status TEXT NOT NULL,
                output TEXT,
                FOREIGN KEY(command_id) REFERENCES commands(id) ON DELETE CASCADE
            )
            """
        ]
        for sql in sqls {
            sqlite3_exec(db, sql, nil, nil, nil)
        }
    }

    /// Adds columns introduced after the initial schema. Safe to call on every launch.
    private func migrateSchema() {
        let hasColumn = { [self] (table: String, column: String) -> Bool in
            var stmt: OpaquePointer?
            defer { sqlite3_finalize(stmt) }
            guard sqlite3_prepare_v2(db, "PRAGMA table_info(\(table))", -1, &stmt, nil) == SQLITE_OK
            else { return false }
            while sqlite3_step(stmt) == SQLITE_ROW {
                if let raw = sqlite3_column_text(stmt, 1), String(cString: raw) == column {
                    return true
                }
            }
            return false
        }

        if !hasColumn("commands", "working_directory") {
            sqlite3_exec(db, "ALTER TABLE commands ADD COLUMN working_directory TEXT", nil, nil, nil)
        }
    }

    private func parseDate(_ raw: UnsafePointer<UInt8>?) -> Date {
        guard let raw = raw else { return Date() }
        let str = String(cString: raw)
        return Self.dateFormatter.date(from: str)
            ?? Self.dateFormatterShort.date(from: str)
            ?? Date()
    }

    private func formatDate(_ date: Date) -> String {
        Self.dateFormatter.string(from: date)
    }

    /// Bind a Swift String to a prepared statement parameter (1-indexed).
    private func bind(_ stmt: OpaquePointer?, index: Int32, text: String) {
        let SQLITE_TRANSIENT = unsafeBitCast(-1, to: sqlite3_destructor_type.self)
        sqlite3_bind_text(stmt, index, text, -1, SQLITE_TRANSIENT)
    }

    // MARK: - Commands CRUD

    func getAllCommands() -> [Command] {
        let sql = "SELECT id, name, command, cmd_type, created_at, working_directory FROM commands ORDER BY created_at DESC"
        var stmt: OpaquePointer?
        var result: [Command] = []

        guard sqlite3_prepare_v2(db, sql, -1, &stmt, nil) == SQLITE_OK else { return result }
        defer { sqlite3_finalize(stmt) }

        while sqlite3_step(stmt) == SQLITE_ROW {
            let id = Int(sqlite3_column_int64(stmt, 0))
            let name = String(cString: sqlite3_column_text(stmt, 1))
            let command = String(cString: sqlite3_column_text(stmt, 2))
            let typeStr = String(cString: sqlite3_column_text(stmt, 3))
            let cmdType = CommandType(rawValue: typeStr) ?? .oneShot
            let createdAt = parseDate(sqlite3_column_text(stmt, 4))
            let workingDirectory: String? = sqlite3_column_type(stmt, 5) != SQLITE_NULL
                ? String(cString: sqlite3_column_text(stmt, 5)) : nil

            result.append(Command(id: id, name: name, command: command, cmdType: cmdType,
                                  workingDirectory: workingDirectory, createdAt: createdAt))
        }
        return result
    }

    @discardableResult
    func addCommand(name: String, command: String, cmdType: CommandType, workingDirectory: String?) -> Int {
        let sql = "INSERT INTO commands (name, command, cmd_type, working_directory) VALUES (?, ?, ?, ?)"
        var stmt: OpaquePointer?

        guard sqlite3_prepare_v2(db, sql, -1, &stmt, nil) == SQLITE_OK else { return -1 }
        defer { sqlite3_finalize(stmt) }

        bind(stmt, index: 1, text: name)
        bind(stmt, index: 2, text: command)
        bind(stmt, index: 3, text: cmdType.rawValue)
        if let wd = workingDirectory, !wd.isEmpty {
            bind(stmt, index: 4, text: wd)
        } else {
            sqlite3_bind_null(stmt, 4)
        }

        guard sqlite3_step(stmt) == SQLITE_DONE else { return -1 }
        return Int(sqlite3_last_insert_rowid(db))
    }

    func updateCommand(id: Int, name: String, command: String, cmdType: CommandType, workingDirectory: String?) {
        let sql = "UPDATE commands SET name=?, command=?, cmd_type=?, working_directory=? WHERE id=?"
        var stmt: OpaquePointer?

        guard sqlite3_prepare_v2(db, sql, -1, &stmt, nil) == SQLITE_OK else { return }
        defer { sqlite3_finalize(stmt) }

        bind(stmt, index: 1, text: name)
        bind(stmt, index: 2, text: command)
        bind(stmt, index: 3, text: cmdType.rawValue)
        if let wd = workingDirectory, !wd.isEmpty {
            bind(stmt, index: 4, text: wd)
        } else {
            sqlite3_bind_null(stmt, 4)
        }
        sqlite3_bind_int64(stmt, 5, Int64(id))

        sqlite3_step(stmt)
    }

    func deleteCommand(id: Int) {
        let sql = "DELETE FROM commands WHERE id = ?"
        var stmt: OpaquePointer?

        guard sqlite3_prepare_v2(db, sql, -1, &stmt, nil) == SQLITE_OK else { return }
        defer { sqlite3_finalize(stmt) }

        sqlite3_bind_int64(stmt, 1, Int64(id))
        sqlite3_step(stmt)
    }

    // MARK: - History CRUD

    @discardableResult
    func addHistoryEntry(commandId: Int, startTime: Date, status: String) -> Int {
        let sql = "INSERT INTO command_history (command_id, start_time, status) VALUES (?, ?, ?)"
        var stmt: OpaquePointer?

        guard sqlite3_prepare_v2(db, sql, -1, &stmt, nil) == SQLITE_OK else { return -1 }
        defer { sqlite3_finalize(stmt) }

        sqlite3_bind_int64(stmt, 1, Int64(commandId))
        bind(stmt, index: 2, text: formatDate(startTime))
        bind(stmt, index: 3, text: status)

        guard sqlite3_step(stmt) == SQLITE_DONE else { return -1 }
        return Int(sqlite3_last_insert_rowid(db))
    }

    func updateHistoryEntry(id: Int, endTime: Date, status: String, output: String) {
        let sql = "UPDATE command_history SET end_time=?, status=?, output=? WHERE id=?"
        var stmt: OpaquePointer?

        guard sqlite3_prepare_v2(db, sql, -1, &stmt, nil) == SQLITE_OK else { return }
        defer { sqlite3_finalize(stmt) }

        bind(stmt, index: 1, text: formatDate(endTime))
        bind(stmt, index: 2, text: status)
        bind(stmt, index: 3, text: output)
        sqlite3_bind_int64(stmt, 4, Int64(id))

        sqlite3_step(stmt)
    }

    func getHistory(forCommandId commandId: Int) -> [CommandHistory] {
        let sql = "SELECT id, command_id, start_time, end_time, status, output FROM command_history WHERE command_id = ? ORDER BY start_time DESC"
        var stmt: OpaquePointer?
        var result: [CommandHistory] = []

        guard sqlite3_prepare_v2(db, sql, -1, &stmt, nil) == SQLITE_OK else { return result }
        defer { sqlite3_finalize(stmt) }

        sqlite3_bind_int64(stmt, 1, Int64(commandId))

        while sqlite3_step(stmt) == SQLITE_ROW {
            let id = Int(sqlite3_column_int64(stmt, 0))
            let cmdId = Int(sqlite3_column_int64(stmt, 1))
            let startTime = parseDate(sqlite3_column_text(stmt, 2))
            let endTime: Date? = sqlite3_column_type(stmt, 3) != SQLITE_NULL
                ? parseDate(sqlite3_column_text(stmt, 3)) : nil
            let status = String(cString: sqlite3_column_text(stmt, 4))
            let output: String? = sqlite3_column_type(stmt, 5) != SQLITE_NULL
                ? String(cString: sqlite3_column_text(stmt, 5)) : nil

            result.append(CommandHistory(id: id, commandId: cmdId, startTime: startTime,
                                         endTime: endTime, status: status, output: output))
        }
        return result
    }
}
