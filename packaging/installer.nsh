; GoRex's own installer steps (mygo.json windows.installerScript): its
; bin, which holds gorex.com (cmd/gorex, from resources), put on the
; user's PATH, and taken off as GoRex is uninstalled. A shell runs
; gorex.com for `gorex`, a console program, which runs GoRex's commands
; and starts GoRex.exe for the rest. The gorex.cmd that older installers
; wrote there, which only started GoRex.exe, goes. PowerShell edits PATH:
; NSIS's strings are cut at 1024 characters, which many a PATH outgrows.
; Setting it through .NET also tells running programs it changed, so new
; terminals see it.

!macro customInstall
  Delete "$INSTDIR\bin\gorex.cmd"
  nsExec::Exec `powershell -NoProfile -NonInteractive -Command "$$d = '$INSTDIR\bin'; $$p = [Environment]::GetEnvironmentVariable('Path', 'User'); $$l = @($$p -split ';' | Where-Object { $$_ -and $$_ -ne $$d }); [Environment]::SetEnvironmentVariable('Path', (($$l + $$d) -join ';'), 'User')"`
  Pop $0
!macroend

!macro customUninstall
  nsExec::Exec `powershell -NoProfile -NonInteractive -Command "$$d = '$INSTDIR\bin'; $$p = [Environment]::GetEnvironmentVariable('Path', 'User'); $$l = @($$p -split ';' | Where-Object { $$_ -and $$_ -ne $$d }); [Environment]::SetEnvironmentVariable('Path', ($$l -join ';'), 'User')"`
  Pop $0
!macroend
