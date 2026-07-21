using System.Windows;
using CmdMgr.ViewModels;

namespace CmdMgr.Views;

public partial class MainWindow : Window
{
    public MainWindow()
    {
        InitializeComponent();
    }

    private void Window_Closing(object sender, System.ComponentModel.CancelEventArgs e)
    {
        if (DataContext is MainViewModel vm)
        {
            e.Cancel = !vm.OnClosing();
            if (!e.Cancel)
                vm.Dispose();
        }
    }
}
