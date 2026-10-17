; Zarf command line tool: Windows installer (Inno Setup 6).
; Build with installer\build.ps1, which compiles both zarf.exe files first.

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#define AppName "Zarf"
#define AppPublisher "Miraç Deprem"
#define AppURL "https://zarf.miracdeprem.com"

[Setup]
AppId={{5B7E2C1A-8F4D-4E6B-9A3C-2D1F0E7B6A54}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL=https://github.com/MrcDprm/zarf-file-share/issues
DefaultDirName={autopf}\Zarf
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
; Installed for the current user: no administrator rights needed, and PATH is the user's own.
PrivilegesRequired=lowest
OutputDir=Output
OutputBaseFilename=zarf-setup
SetupIconFile=zarf.ico
LicenseFile=..\LICENSE
UninstallDisplayIcon={app}\zarf.ico
UninstallDisplayName={#AppName}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ChangesEnvironment=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible

[Languages]
Name: "turkish"; MessagesFile: "compiler:Languages\Turkish.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Messages]
turkish.FinishedLabel=Zarf kuruldu.%n%nNasıl kullanılır:%n• Başlat menüsünden "Zarf"ı aç, ya da%n• herhangi bir PowerShell veya Komut İstemi penceresinde zarf yaz.%n%nÖrnek: zarf send rapor.pdf%nBütün komutlar: zarf help
english.FinishedLabel=Zarf is installed.%n%nHow to use it:%n• Open "Zarf" from the Start menu, or%n• type zarf in any PowerShell or Command Prompt window.%n%nExample: zarf send report.pdf%nEvery command: zarf help

[CustomMessages]
turkish.OpenZarf=Zarf'ı şimdi aç
english.OpenZarf=Open Zarf now

[Files]
; The right build for the machine: ARM64 Windows gets the native one.
Source: "build\amd64\zarf.exe"; DestDir: "{app}"; Flags: ignoreversion; Check: not IsArm64
Source: "build\arm64\zarf.exe"; DestDir: "{app}"; Flags: ignoreversion; Check: IsArm64
Source: "zarf.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
; A PowerShell window that stays open, shows the welcome, and has zarf on its PATH even before
; Windows hands the new PATH to programs started earlier.
Name: "{autoprograms}\Zarf"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoExit -NoLogo -Command ""$env:Path += ';{app}'; zarf"""; WorkingDir: "{%USERPROFILE}"; IconFilename: "{app}\zarf.ico"; Comment: "Zarf"

[Registry]
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Check: NeedsAddPath(ExpandConstant('{app}'))

[Run]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoExit -NoLogo -Command ""$env:Path += ';{app}'; zarf"""; WorkingDir: "{%USERPROFILE}"; Description: "{cm:OpenZarf}"; Flags: postinstall nowait skipifsilent

[Code]
function NeedsAddPath(Dir: string): Boolean;
var
  Path: string;
begin
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path) then
  begin
    Result := True;
    exit;
  end;
  Result := Pos(';' + Uppercase(Dir) + ';', ';' + Uppercase(Path) + ';') = 0;
end;

{ On uninstall, take the folder out of the user's PATH again. }
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Path, Dir: string;
  P: Integer;
begin
  if CurUninstallStep <> usPostUninstall then
    exit;
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path) then
    exit;
  Dir := ExpandConstant('{app}');
  P := Pos(';' + Uppercase(Dir), Uppercase(Path));
  if P > 0 then
  begin
    Delete(Path, P, Length(Dir) + 1);
    RegWriteExpandStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path);
  end;
end;
