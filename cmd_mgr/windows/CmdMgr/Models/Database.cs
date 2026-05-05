using System.IO;
using Microsoft.Data.Sqlite;

namespace CmdMgr.Models;

/// <summary>
/// SQLite database wrapper – schema-compatible with the Python CmdMgr.
/// </summary>
public sealed class Database : IDisposable
{
    private readonly SqliteConnection _conn;

    private const string DateFormat = "yyyy-MM-dd HH:mm:ss.ffffff";

    public Database()
    {
        var dbDir = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData),
            "CmdMgr");
        Directory.CreateDirectory(dbDir);
        var dbPath = Path.Combine(dbDir, "cmd_mgr.db");

        _conn = new SqliteConnection($"Data Source={dbPath}");
        _conn.Open();

        Execute("PRAGMA journal_mode=WAL");
        Execute("PRAGMA foreign_keys=ON");
        CreateTables();
        MigrateSchema();
    }

    public void Dispose()
    {
        _conn.Close();
        _conn.Dispose();
    }

    private void Execute(string sql)
    {
        using var cmd = _conn.CreateCommand();
        cmd.CommandText = sql;
        cmd.ExecuteNonQuery();
    }

    private void CreateTables()
    {
        Execute(@"
            CREATE TABLE IF NOT EXISTS commands (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                command TEXT NOT NULL,
                cmd_type TEXT NOT NULL,
                created_at DATETIME DEFAULT CURRENT_TIMESTAMP
            )");

        Execute(@"
            CREATE TABLE IF NOT EXISTS command_history (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                command_id INTEGER NOT NULL,
                start_time DATETIME NOT NULL,
                end_time DATETIME,
                status TEXT NOT NULL,
                output TEXT,
                FOREIGN KEY(command_id) REFERENCES commands(id) ON DELETE CASCADE
            )");
    }

    /// <summary>Adds columns introduced after the initial schema. Safe to call on every launch.</summary>
    private void MigrateSchema()
    {
        var columns = new HashSet<string>();
        using (var cmd = _conn.CreateCommand())
        {
            cmd.CommandText = "PRAGMA table_info(commands)";
            using var reader = cmd.ExecuteReader();
            while (reader.Read())
                columns.Add(reader.GetString(1));
        }

        if (!columns.Contains("working_directory"))
            Execute("ALTER TABLE commands ADD COLUMN working_directory TEXT");
    }

    private static DateTime ParseDate(string? s)
    {
        if (string.IsNullOrEmpty(s)) return DateTime.Now;
        if (DateTime.TryParseExact(s, DateFormat, null, System.Globalization.DateTimeStyles.None, out var d))
            return d;
        if (DateTime.TryParseExact(s, "yyyy-MM-dd HH:mm:ss", null, System.Globalization.DateTimeStyles.None, out d))
            return d;
        return DateTime.TryParse(s, out d) ? d : DateTime.Now;
    }

    // --- Commands CRUD ---

    public List<Command> GetAllCommands()
    {
        var result = new List<Command>();
        using var cmd = _conn.CreateCommand();
        cmd.CommandText = "SELECT id, name, command, cmd_type, created_at, working_directory FROM commands ORDER BY created_at DESC";
        using var reader = cmd.ExecuteReader();
        while (reader.Read())
        {
            result.Add(new Command
            {
                Id = reader.GetInt32(0),
                Name = reader.GetString(1),
                CommandText = reader.GetString(2),
                CmdType = reader.GetString(3),
                CreatedAt = ParseDate(reader.IsDBNull(4) ? null : reader.GetString(4)),
                WorkingDirectory = reader.IsDBNull(5) ? null : reader.GetString(5)
            });
        }
        return result;
    }

    public int AddCommand(string name, string commandText, string cmdType, string? workingDirectory)
    {
        using var cmd = _conn.CreateCommand();
        cmd.CommandText = "INSERT INTO commands (name, command, cmd_type, working_directory) VALUES ($name, $cmd, $type, $wd)";
        cmd.Parameters.AddWithValue("$name", name);
        cmd.Parameters.AddWithValue("$cmd", commandText);
        cmd.Parameters.AddWithValue("$type", cmdType);
        cmd.Parameters.AddWithValue("$wd", string.IsNullOrWhiteSpace(workingDirectory) ? DBNull.Value : workingDirectory);
        cmd.ExecuteNonQuery();

        cmd.CommandText = "SELECT last_insert_rowid()";
        cmd.Parameters.Clear();
        return Convert.ToInt32(cmd.ExecuteScalar());
    }

    public void UpdateCommand(int id, string name, string commandText, string cmdType, string? workingDirectory)
    {
        using var cmd = _conn.CreateCommand();
        cmd.CommandText = "UPDATE commands SET name=$name, command=$cmd, cmd_type=$type, working_directory=$wd WHERE id=$id";
        cmd.Parameters.AddWithValue("$name", name);
        cmd.Parameters.AddWithValue("$cmd", commandText);
        cmd.Parameters.AddWithValue("$type", cmdType);
        cmd.Parameters.AddWithValue("$wd", string.IsNullOrWhiteSpace(workingDirectory) ? DBNull.Value : workingDirectory);
        cmd.Parameters.AddWithValue("$id", id);
        cmd.ExecuteNonQuery();
    }

    public void DeleteCommand(int id)
    {
        using var cmd = _conn.CreateCommand();
        cmd.CommandText = "DELETE FROM commands WHERE id = $id";
        cmd.Parameters.AddWithValue("$id", id);
        cmd.ExecuteNonQuery();
    }

    // --- History CRUD ---

    public int AddHistoryEntry(int commandId, DateTime startTime, string status)
    {
        using var cmd = _conn.CreateCommand();
        cmd.CommandText = "INSERT INTO command_history (command_id, start_time, status) VALUES ($cid, $start, $status)";
        cmd.Parameters.AddWithValue("$cid", commandId);
        cmd.Parameters.AddWithValue("$start", startTime.ToString(DateFormat));
        cmd.Parameters.AddWithValue("$status", status);
        cmd.ExecuteNonQuery();

        cmd.CommandText = "SELECT last_insert_rowid()";
        cmd.Parameters.Clear();
        return Convert.ToInt32(cmd.ExecuteScalar());
    }

    public void UpdateHistoryEntry(int id, DateTime endTime, string status, string output)
    {
        using var cmd = _conn.CreateCommand();
        cmd.CommandText = "UPDATE command_history SET end_time=$end, status=$status, output=$output WHERE id=$id";
        cmd.Parameters.AddWithValue("$end", endTime.ToString(DateFormat));
        cmd.Parameters.AddWithValue("$status", status);
        cmd.Parameters.AddWithValue("$output", output);
        cmd.Parameters.AddWithValue("$id", id);
        cmd.ExecuteNonQuery();
    }

    public List<CommandHistory> GetHistory(int commandId)
    {
        var result = new List<CommandHistory>();
        using var cmd = _conn.CreateCommand();
        cmd.CommandText = "SELECT id, command_id, start_time, end_time, status, output FROM command_history WHERE command_id = $cid ORDER BY start_time DESC";
        cmd.Parameters.AddWithValue("$cid", commandId);
        using var reader = cmd.ExecuteReader();
        while (reader.Read())
        {
            result.Add(new CommandHistory
            {
                Id = reader.GetInt32(0),
                CommandId = reader.GetInt32(1),
                StartTime = ParseDate(reader.GetString(2)),
                EndTime = reader.IsDBNull(3) ? null : ParseDate(reader.GetString(3)),
                Status = reader.GetString(4),
                Output = reader.IsDBNull(5) ? null : reader.GetString(5)
            });
        }
        return result;
    }
}
