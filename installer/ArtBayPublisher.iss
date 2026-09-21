; ArtBay Publisher — Inno Setup installer script
; Build locally:   iscc installer\ArtBayPublisher.iss
; Build in CI:      see .github/workflows/release.yml (windows-latest ships Inno Setup 6 preinstalled)

#define MyAppName "ArtBay Publisher"
#define MyAppVersion "4.0.4"
#define MyAppPublisher "RovelLabs"
#define MyAppURL "https://github.com/RovelLabs/ArtBay-Publisher-FunPay-bot-"
#define MyAppExeName "ArtBayPublisher.exe"

[Setup]
AppId={{29652F2C-1997-4678-94C3-DE226EB3639A}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}/issues
AppUpdatesURL={#MyAppURL}/releases
; Per-user install by default: no admin rights / UAC prompt required, matches the
; app's local-first design (everything lives under the current user's profile).
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
DefaultDirName={autopf}\ArtBayPublisher
DefaultGroupName=ArtBay Publisher
DisableProgramGroupPage=yes
OutputDir=..\dist
OutputBaseFilename=ArtBayPublisher-Setup-{#MyAppVersion}
SetupIconFile=..\assets\icon.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ArchitecturesInstallIn64BitMode=x64compatible
DisableWelcomePage=no
LicenseFile=..\LICENSE

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "..\ArtBayPublisher.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README_EN.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\CHANGELOG.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\examples\*"; DestDir: "{app}\examples"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\{#MyAppExeName}"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; IconFilename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
; Local data (config, tokens, checkpoints, backups) lives in %APPDATA%\ArtBayPublisher
; and is intentionally left in place on uninstall so re-installing does not lose it.
Type: filesandordirs; Name: "{app}"
