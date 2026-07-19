using System.Collections.ObjectModel;
using System.Windows.Input;
using CmdMgr.Models;
using CmdMgr.Views;

namespace CmdMgr.ViewModels;

public enum CommandFilter
{
    All,
    Running,
    LongRunning,
    OneShot
}

public static class CommandFilterExtensions
{
    public static string DisplayName(this CommandFilter filter) => filter switch
    {
        CommandFilter.All => "All Commands",
        CommandFilter.Running => "Running",
        CommandFilter.LongRunning => "Long-running",
        CommandFilter.OneShot => "One-shot",
        _ => filter.ToString()
    };
}

public enum CommandDetailTab
{
    Overview,
    Output,
    History
}

/// <summary>
/// Main ViewModel – owns persistence, process lifecycle, and the current selection.
/// </summary>
public class MainViewModel : ViewModelBase, IDisposable
{
    public Database Database { get; }
    public ProcessManager ProcessManager { get; } = new();
    public ObservableCollection<Command> Commands { get; } = new();
    public ObservableCollection<Command> FilteredCommands { get; } = new();

    public IReadOnlyList<CommandFilter> Filters { get; } = Enum.GetValues<CommandFilter>();

    private string _searchText = "";
    public string SearchText
    {
        get => _searchText;
        set
        {
            if (SetField(ref _searchText, value))
            {
                UpdateFilteredCommands();
                RaiseEmptyStateChanged();
            }
        }
    }

    private CommandFilter _commandFilter = CommandFilter.All;
    public CommandFilter CommandFilterValue
    {
        get => _commandFilter;
        set
        {
            if (SetField(ref _commandFilter, value))
            {
                UpdateFilteredCommands();
                RaiseEmptyStateChanged();
            }
        }
    }

    private Command? _selectedCommand;
    public Command? SelectedCommand
    {
        get => _selectedCommand;
        set
        {
            if (SetField(ref _selectedCommand, value))
            {
                OnPropertyChanged(nameof(HasSelection));
                OnPropertyChanged(nameof(CurrentProcessInfo));
            }
        }
    }

    public bool HasSelection => SelectedCommand != null;

    public ProcessInfo? CurrentProcessInfo =>
        SelectedCommand == null ? null : ProcessManager.GetProcessInfo(SelectedCommand.Id);

    private CommandDetailTab _selectedDetailTab = CommandDetailTab.Overview;
    public CommandDetailTab SelectedDetailTab
    {
        get => _selectedDetailTab;
        set => SetField(ref _selectedDetailTab, value);
    }

    public string? ErrorMessage => ProcessManager.LastError;

    public bool HasCommands => Commands.Count > 0;
    public string EmptyStateIcon => HasCommands ? "🔍" : "⌨";
    public string EmptyStateTitle => HasCommands ? "No Matches" : "No Commands";
    public string EmptyStateHint => HasCommands
        ? "Change the search or filter."
        : "Create a command to get started.";

    public ICommand AddCommandAction { get; }
    public ICommand EditCommandAction { get; }
    public ICommand DuplicateCommandAction { get; }
    public ICommand DeleteCommandAction { get; }
    public ICommand RunCommandAction { get; }
    public ICommand RunSelectedCommandAction { get; }
    public ICommand ShowOutputCommandAction { get; }
    public ICommand ShowHistoryCommandAction { get; }
    public ICommand ClearSearchCommand { get; }
    public ICommand DismissErrorCommand { get; }

    public MainViewModel()
    {
        Database = new Database();

        ProcessManager.PropertyChanged += (_, e) =>
        {
            if (e.PropertyName == nameof(ProcessManager.LastError))
                OnPropertyChanged(nameof(ErrorMessage));
        };
        ProcessManager.ProcessesChanged += commandId =>
        {
            UpdateFilteredCommands();
            RaiseEmptyStateChanged();
            if (SelectedCommand?.Id == commandId)
                OnPropertyChanged(nameof(CurrentProcessInfo));
        };

        LoadCommands();
        SelectedCommand = Commands.FirstOrDefault();

        AddCommandAction = new RelayCommand(OnAddCommand);
        EditCommandAction = new RelayCommand(OnEditCommand);
        DuplicateCommandAction = new RelayCommand(OnDuplicateCommand);
        DeleteCommandAction = new RelayCommand(OnDeleteCommand);
        RunCommandAction = new RelayCommand(OnRunCommand);
        RunSelectedCommandAction = new RelayCommand(_ => { if (SelectedCommand != null) OnRunCommand(SelectedCommand); });
        ShowOutputCommandAction = new RelayCommand(OnShowOutput);
        ShowHistoryCommandAction = new RelayCommand(OnShowHistory);
        ClearSearchCommand = new RelayCommand(_ => SearchText = "");
        DismissErrorCommand = new RelayCommand(_ => ProcessManager.LastError = null);
    }

    public void LoadCommands()
    {
        var selectedId = SelectedCommand?.Id;
        Commands.Clear();
        foreach (var cmd in Database.GetAllCommands())
            Commands.Add(cmd);
        UpdateFilteredCommands();
        RaiseEmptyStateChanged();

        if (selectedId.HasValue)
            SelectedCommand = Commands.FirstOrDefault(c => c.Id == selectedId.Value) ?? Commands.FirstOrDefault();
    }

    private void UpdateFilteredCommands()
    {
        FilteredCommands.Clear();
        var q = SearchText.Trim().ToLowerInvariant();
        foreach (var cmd in Commands)
        {
            var matchesQuery = string.IsNullOrEmpty(q)
                || cmd.Name.ToLowerInvariant().Contains(q)
                || cmd.CommandText.ToLowerInvariant().Contains(q)
                || (cmd.WorkingDirectory?.ToLowerInvariant().Contains(q) ?? false);

            var matchesFilter = CommandFilterValue switch
            {
                CommandFilter.All => true,
                CommandFilter.Running => cmd.IsRunning,
                CommandFilter.LongRunning => cmd.IsLongRunning,
                CommandFilter.OneShot => cmd.IsOneShot,
                _ => true
            };

            if (matchesQuery && matchesFilter)
                FilteredCommands.Add(cmd);
        }
    }

    private void RaiseEmptyStateChanged()
    {
        OnPropertyChanged(nameof(HasCommands));
        OnPropertyChanged(nameof(EmptyStateIcon));
        OnPropertyChanged(nameof(EmptyStateTitle));
        OnPropertyChanged(nameof(EmptyStateHint));
    }

    public bool IsRunning(int commandId) => ProcessManager.IsRunning(commandId);

    public List<CommandHistory> HistoryFor(Command command) => Database.GetHistory(command.Id);

    // MARK: - CRUD

    private void OnAddCommand(object? _)
    {
        var dialog = new AddEditDialog { Owner = System.Windows.Application.Current.MainWindow };
        if (dialog.ShowDialog() == true)
        {
            var id = Database.AddCommand(dialog.CommandName, dialog.CommandText,
                dialog.CommandType, dialog.WorkingDirectory);
            LoadCommands();
            SelectedCommand = Commands.FirstOrDefault(c => c.Id == id) ?? Commands.FirstOrDefault();
            SelectedDetailTab = CommandDetailTab.Overview;
        }
    }

    private void OnEditCommand(object? param)
    {
        if (param is not Command cmd) return;
        var dialog = new AddEditDialog(cmd) { Owner = System.Windows.Application.Current.MainWindow };
        if (dialog.ShowDialog() == true)
        {
            Database.UpdateCommand(cmd.Id, dialog.CommandName, dialog.CommandText,
                dialog.CommandType, dialog.WorkingDirectory);
            LoadCommands();
            SelectedCommand = Commands.FirstOrDefault(c => c.Id == cmd.Id);
        }
    }

    private void OnDuplicateCommand(object? param)
    {
        if (param is not Command cmd) return;
        var id = Database.AddCommand($"{cmd.Name} Copy", cmd.CommandText, cmd.CmdType, cmd.WorkingDirectory);
        LoadCommands();
        var copy = Commands.FirstOrDefault(c => c.Id == id);
        if (copy == null) return;
        SelectedCommand = copy;
        OnEditCommand(copy);
    }

    private void OnDeleteCommand(object? param)
    {
        if (param is not Command cmd) return;
        var message = cmd.IsRunning
            ? $"\"{cmd.Name}\" is running. Deleting it will stop the process and remove its execution history."
            : $"Deleting \"{cmd.Name}\" also removes its execution history. This cannot be undone.";
        var result = System.Windows.MessageBox.Show(message, "Delete Command?",
            System.Windows.MessageBoxButton.YesNo, System.Windows.MessageBoxImage.Warning);
        if (result != System.Windows.MessageBoxResult.Yes) return;

        if (ProcessManager.IsRunning(cmd.Id))
            ProcessManager.Stop(cmd.Id, Database);

        Database.DeleteCommand(cmd.Id);
        LoadCommands();
    }

    // MARK: - Process actions

    private void OnRunCommand(object? param)
    {
        if (param is not Command cmd) return;
        SelectedCommand = cmd;
        SelectedDetailTab = CommandDetailTab.Output;
        if (cmd.IsLongRunning && ProcessManager.IsRunning(cmd.Id))
            ProcessManager.Stop(cmd.Id, Database);
        else if (!ProcessManager.IsRunning(cmd.Id))
            ProcessManager.Start(cmd, Database);
        CommandManager.InvalidateRequerySuggested();
    }

    private void OnShowOutput(object? param)
    {
        if (param is not Command cmd) return;
        SelectedCommand = cmd;
        SelectedDetailTab = CommandDetailTab.Output;
    }

    private void OnShowHistory(object? param)
    {
        if (param is not Command cmd) return;
        SelectedCommand = cmd;
        SelectedDetailTab = CommandDetailTab.History;
    }

    public bool OnClosing()
    {
        if (ProcessManager.RunningProcesses.IsEmpty) return true;

        var result = System.Windows.MessageBox.Show(
            "There are still running commands. Do you want to terminate them and exit?",
            "Running Processes", System.Windows.MessageBoxButton.YesNo, System.Windows.MessageBoxImage.Question);
        if (result == System.Windows.MessageBoxResult.Yes)
        {
            ProcessManager.StopAll(Database);
            return true;
        }
        return false;
    }

    public void Dispose() => Database.Dispose();
}
