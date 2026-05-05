using System.Collections.ObjectModel;
using System.Windows.Input;
using System.Windows;
using CmdMgr.Models;
using CmdMgr.Views;

namespace CmdMgr.ViewModels;

/// <summary>
/// Main ViewModel for the command list.
/// </summary>
public class MainViewModel : ViewModelBase, IDisposable
{
    public Database Database { get; }
    public ProcessManager ProcessManager { get; } = new();
    public ObservableCollection<Command> Commands { get; } = new();
    public ObservableCollection<Command> FilteredCommands { get; } = new();

    private string _searchText = "";
    public string SearchText
    {
        get => _searchText;
        set
        {
            if (SetField(ref _searchText, value))
            {
                UpdateFilteredCommands();
                OnPropertyChanged(nameof(EmptyStateIcon));
                OnPropertyChanged(nameof(EmptyStateTitle));
                OnPropertyChanged(nameof(EmptyStateHint));
            }
        }
    }

    public string EmptyStateIcon  => string.IsNullOrEmpty(SearchText) ? "⌨" : "🔍";
    public string EmptyStateTitle => string.IsNullOrEmpty(SearchText) ? "No commands yet" : "No results";
    public string EmptyStateHint  => string.IsNullOrEmpty(SearchText)
        ? "Click \"+ Add Command\" to get started."
        : "Try a different search term.";

    // Tracks open output windows by commandId to avoid duplicates.
    private readonly Dictionary<int, Window> _outputWindows = new();

    public ICommand AddCommand { get; }
    public ICommand EditCommand { get; }
    public ICommand DeleteCommand { get; }
    public ICommand ToggleLongRunningCommand { get; }
    public ICommand ExecuteOneShotCommand { get; }
    public ICommand ShowOutputCommand { get; }
    public ICommand ShowHistoryCommand { get; }
    public ICommand ClearSearchCommand { get; }

    public MainViewModel()
    {
        Database = new Database();
        LoadCommands();

        AddCommand = new RelayCommand(OnAddCommand);
        EditCommand = new RelayCommand(OnEditCommand);
        DeleteCommand = new RelayCommand(OnDeleteCommand);
        ToggleLongRunningCommand = new RelayCommand(OnToggleLongRunning);
        ExecuteOneShotCommand = new RelayCommand(OnExecuteOneShot);
        ShowOutputCommand = new RelayCommand(OnShowOutput);
        ShowHistoryCommand = new RelayCommand(OnShowHistory);
        ClearSearchCommand = new RelayCommand(_ => SearchText = "");
    }

    public void LoadCommands()
    {
        Commands.Clear();
        foreach (var cmd in Database.GetAllCommands())
            Commands.Add(cmd);
        UpdateFilteredCommands();
    }

    private void UpdateFilteredCommands()
    {
        FilteredCommands.Clear();
        var q = SearchText.Trim().ToLowerInvariant();
        foreach (var cmd in Commands)
        {
            if (string.IsNullOrEmpty(q)
                || cmd.Name.ToLowerInvariant().Contains(q)
                || cmd.CommandText.ToLowerInvariant().Contains(q))
            {
                FilteredCommands.Add(cmd);
            }
        }
    }

    public bool IsRunning(int commandId) => ProcessManager.IsRunning(commandId);

    private void OnAddCommand(object? _)
    {
        var dialog = new AddEditDialog { Owner = System.Windows.Application.Current.MainWindow };
        if (dialog.ShowDialog() == true)
        {
            Database.AddCommand(dialog.CommandName, dialog.CommandText,
                dialog.CommandType, dialog.WorkingDirectory);
            LoadCommands();
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
        }
    }

    private void OnDeleteCommand(object? param)
    {
        if (param is not Command cmd) return;
        var result = System.Windows.MessageBox.Show(
            $"Are you sure you want to delete \"{cmd.Name}\"?",
            "Delete Command", System.Windows.MessageBoxButton.YesNo, System.Windows.MessageBoxImage.Question);
        if (result != System.Windows.MessageBoxResult.Yes) return;

        if (ProcessManager.IsRunning(cmd.Id))
            ProcessManager.Stop(cmd.Id, Database);

        Database.DeleteCommand(cmd.Id);
        LoadCommands();
    }

    private void OnToggleLongRunning(object? param)
    {
        if (param is not Command cmd) return;
        if (ProcessManager.IsRunning(cmd.Id))
            ProcessManager.Stop(cmd.Id, Database);
        else
            ProcessManager.Start(cmd, Database);
        CommandManager.InvalidateRequerySuggested();
    }

    private void OnExecuteOneShot(object? param)
    {
        if (param is not Command cmd) return;
        if (ProcessManager.IsRunning(cmd.Id)) return;  // one-shot protection
        var info = ProcessManager.Start(cmd, Database);
        if (info != null)
            OpenOrFocusOutputWindow(cmd, info);
    }

    private void OnShowOutput(object? param)
    {
        if (param is not Command cmd) return;
        var info = ProcessManager.GetProcessInfo(cmd.Id);
        if (info != null)
            OpenOrFocusOutputWindow(cmd, info);
        else
            System.Windows.MessageBox.Show("No output available for this command.", "Info",
                System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Information);
    }

    private void OpenOrFocusOutputWindow(Command cmd, ProcessInfo info)
    {
        if (_outputWindows.TryGetValue(cmd.Id, out var existing) && existing.IsLoaded)
        {
            existing.Activate();
            return;
        }

        var win = new OutputWindow(cmd.Name, info) { Owner = System.Windows.Application.Current.MainWindow };
        win.Closed += (_, _) => _outputWindows.Remove(cmd.Id);
        _outputWindows[cmd.Id] = win;
        win.Show();
    }

    private void OnShowHistory(object? param)
    {
        if (param is not Command cmd) return;
        var history = Database.GetHistory(cmd.Id);
        var win = new HistoryWindow(cmd.Name, history) { Owner = System.Windows.Application.Current.MainWindow };
        win.Show();
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
