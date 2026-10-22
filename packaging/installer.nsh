; GoRex's own installer steps (mygo.json windows.installerScript): a
; gorex command in $INSTDIR\bin, put on the user's PATH, and taken off as
; GoRex is uninstalled. PowerShell edits PATH: NSIS's strings are cut at
; 1024 characters, which many a PATH outgrows. Setting it through .NET
; also tells running programs it changed, so new terminals see it.

!macro customInstall
  CreateDirectory "$INSTDIR\bin"
  FileOpen $0 "$INSTDIR\bin\gorex.cmd" w
  FileWrite $0 '@"%~dp0..\GoRex.exe" %*$\r$\n'
  FileClose $0
  nsExec::Exec `powershell -NoProfile -NonInteractive -Command "$$d = '$INSTDIR\bin'; $$p = [Environment]::GetEnvironmentVariable('Path', 'User'); $$l = @($$p -split ';' | Where-Object { $$_ -and $$_ -ne $$d }); [Environment]::SetEnvironmentVariable('Path', (($$l + $$d) -join ';'), 'User')"`
  Pop $0
!macroend

!macro customUninstall
  nsExec::Exec `powershell -NoProfile -NonInteractive -Command "$$d = '$INSTDIR\bin'; $$p = [Environment]::GetEnvironmentVariable('Path', 'User'); $$l = @($$p -split ';' | Where-Object { $$_ -and $$_ -ne $$d }); [Environment]::SetEnvironmentVariable('Path', ($$l -join ';'), 'User')"`
  Pop $0
!macroend
