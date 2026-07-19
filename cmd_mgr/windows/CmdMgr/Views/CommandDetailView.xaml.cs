using System.ComponentModel;
using System.Windows;
using System.Windows.Controls;
using CmdMgr.ViewModels;

namespace CmdMgr.Views;

/// <summary>
/// Detail pane for the selected command: overview, live output, and run history.
/// DataContext is inherited from MainWindow (the MainViewModel); this control
/// wires the Output/History child panels manually since they need direct access
/// to ProcessManager state rather than a plain property binding.
/// </summary>
public partial class CommandDetailView : UserControl
{
    private MainViewModel? _vm;

    public CommandDetailView()
    {
        InitializeComponent();
        DataContextChanged += OnDataContextChanged;
    }

    private void OnDataContextChanged(object sender, DependencyPropertyChangedEventArgs e)
    {
        if (_vm != null)
            _vm.PropertyChanged -= OnVmPropertyChanged;

        _vm = e.NewValue as MainViewModel;

        if (_vm != null)
            _vm.PropertyChanged += OnVmPropertyChanged;

        RefreshOutput();
        RefreshHistory();
    }

    private void OnVmPropertyChanged(object? sender, PropertyChangedEventArgs e)
    {
        switch (e.PropertyName)
        {
            case nameof(MainViewModel.SelectedCommand):
                RefreshOutput();
                RefreshHistory();
                break;
            case nameof(MainViewModel.SelectedDetailTab):
                if (_vm?.SelectedDetailTab == CommandDetailTab.History)
                    RefreshHistory();
                break;
            case nameof(MainViewModel.CurrentProcessInfo):
                RefreshOutput();
                RefreshHistory();
                break;
        }
    }

    private void RefreshOutput()
    {
        if (_vm?.SelectedCommand is not { } command) return;
        OutputPanelControl.Attach(_vm.CurrentProcessInfo, () => _vm.RunCommandAction.Execute(command));
    }

    private void RefreshHistory()
    {
        if (_vm?.SelectedCommand is not { } command) return;
        HistoryPanelControl.Attach(_vm.HistoryFor(command), () => _vm.RunCommandAction.Execute(command));
    }

    private void RunButton_Click(object sender, RoutedEventArgs e)
    {
        if (_vm?.SelectedCommand is { } command)
            _vm.RunCommandAction.Execute(command);
    }

    private void MoreButton_Click(object sender, RoutedEventArgs e)
    {
        if (sender is Button { ContextMenu: { } menu } button)
        {
            menu.PlacementTarget = button;
            menu.IsOpen = true;
        }
    }

    private void Edit_Click(object sender, RoutedEventArgs e)
    {
        if (_vm?.SelectedCommand is { } command)
            _vm.EditCommandAction.Execute(command);
    }

    private void Duplicate_Click(object sender, RoutedEventArgs e)
    {
        if (_vm?.SelectedCommand is { } command)
            _vm.DuplicateCommandAction.Execute(command);
    }

    private void Delete_Click(object sender, RoutedEventArgs e)
    {
        if (_vm?.SelectedCommand is { } command)
            _vm.DeleteCommandAction.Execute(command);
    }
}
