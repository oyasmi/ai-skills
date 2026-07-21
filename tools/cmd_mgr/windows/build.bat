@echo off
setlocal

cd /d "%~dp0"

echo Building CmdMgr for Windows...

:: Copy icon
if exist "..\icon.ico" (
    copy /Y "..\icon.ico" "CmdMgr\icon.ico" >nul
)

:: Restore NuGet packages
dotnet restore CmdMgr.sln
if %ERRORLEVEL% NEQ 0 (
    echo ERROR: dotnet restore failed!
    exit /b %ERRORLEVEL%
)

:: Clean build artifacts to prevent cache pollution between FDD and SCD
if exist "CmdMgr\obj" rmdir /s /q "CmdMgr\obj"
if exist "CmdMgr\bin" rmdir /s /q "CmdMgr\bin"

:: Build 1: Framework-dependent deployment (FDD) - Lightweight
echo.
echo [1/2] Building Framework-Dependent version (Lightweight, requires .NET 8 Runtime)...
dotnet publish CmdMgr\CmdMgr.csproj ^
    -c Release ^
    -r win-x64 ^
    --self-contained false ^
    -p:PublishSingleFile=true ^
    -o dist\framework-dependent

if %ERRORLEVEL% NEQ 0 (
    echo Build failed for Framework-Dependent version!
    exit /b %ERRORLEVEL%
)

:: Clean again before the next build
if exist "CmdMgr\obj" rmdir /s /q "CmdMgr\obj"
if exist "CmdMgr\bin" rmdir /s /q "CmdMgr\bin"

:: Build 2: Self-contained deployment (SCD) - Standalone
echo.
echo [2/2] Building Self-Contained version (Standalone, includes .NET 8 Runtime)...
dotnet publish CmdMgr\CmdMgr.csproj ^
    -c Release ^
    -r win-x64 ^
    --self-contained true ^
    -p:PublishSingleFile=true ^
    -p:IncludeNativeLibrariesForSelfExtract=true ^
    -p:EnableCompressionInSingleFile=true ^
    -o dist\self-contained

if %ERRORLEVEL% NEQ 0 (
    echo Build failed for Self-Contained version!
    exit /b %ERRORLEVEL%
)

echo.
echo Build successful!
echo =======================================================
echo Artifact 1 (Lightweight): %CD%\dist\framework-dependent\CmdMgr.exe
for %%A in ("dist\framework-dependent\CmdMgr.exe") do echo Size: %%~zA bytes
echo.
echo Artifact 2 (Standalone):  %CD%\dist\self-contained\CmdMgr.exe
for %%A in ("dist\self-contained\CmdMgr.exe") do echo Size: %%~zA bytes
echo =======================================================

endlocal
