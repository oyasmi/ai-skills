using System.Windows;
using System.Windows.Forms;
using CmdMgr.Models;

namespace CmdMgr.Views;

public partial class AddEditDialog : Window
{
    private readonly Command? _existing;

    public string CommandName => NameBox.Text.Trim();
    public string CommandText => CommandBox.Text.Trim();
    public string CommandType => LongRunningRadio.IsChecked == true ? "long-running" : "one-shot";
    public string? WorkingDirectory
    {
        get
        {
            var s = WorkingDirBox.Text.Trim();
            return string.IsNullOrEmpty(s) ? null : s;
        }
    }

    public AddEditDialog(Command? existing = null)
    {
        InitializeComponent();
        _existing = existing;

        if (existing != null)
        {
            Title = "Edit Command";
            DialogTitle.Text = "Edit Command";
            NameBox.Text = existing.Name;
            CommandBox.Text = existing.CommandText;
            WorkingDirBox.Text = existing.WorkingDirectory ?? "";
            LongRunningRadio.IsChecked = existing.IsLongRunning;
            OneShotRadio.IsChecked = existing.IsOneShot;
        }
    }

    private void Browse_Click(object sender, RoutedEventArgs e)
    {
        using var dlg = new FolderBrowserDialog
        {
            Description = "Select working directory",
            UseDescriptionForTitle = true,
            SelectedPath = WorkingDirBox.Text.Trim()
        };
        if (dlg.ShowDialog() == System.Windows.Forms.DialogResult.OK)
            WorkingDirBox.Text = dlg.SelectedPath;
    }

    private void Save_Click(object sender, RoutedEventArgs e)
    {
        if (string.IsNullOrWhiteSpace(CommandName) || string.IsNullOrWhiteSpace(CommandText))
        {
            System.Windows.MessageBox.Show("Name and Command fields cannot be empty.",
                "Validation Error", MessageBoxButton.OK, MessageBoxImage.Warning);
            return;
        }
        DialogResult = true;
    }

    private void Cancel_Click(object sender, RoutedEventArgs e) => DialogResult = false;
}
