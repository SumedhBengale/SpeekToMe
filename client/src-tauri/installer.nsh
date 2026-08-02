!macro NSIS_HOOK_PREUNINSTALL
  DetailPrint "Terminating any running SpeekTo.Me background processes..."
  nsExec::Exec 'taskkill /F /IM speektome.exe'
  nsExec::Exec 'taskkill /F /IM SpeekTo.Me.exe'

  DetailPrint "Removing SpeekTo.Me credentials from Windows Vault..."
  nsExec::Exec 'cmdkey /delete:session_token.SpeekToMe'
  nsExec::Exec 'cmdkey /delete:SpeekToMe'
  nsExec::Exec 'cmdkey /delete:SpeekTo.Me'
  nsExec::Exec 'cmdkey /delete:com.speektome.app'
  nsExec::Exec 'cmdkey /delete:26746SumedhBengale.SpeekTo.Me'

  DetailPrint "Removing SpeekTo.Me AppData and user config..."
  SetShellVarContext current
  RMDir /r /REBOOTOK "$APPDATA\SpeekToMe"
  RMDir /r /REBOOTOK "$LOCALAPPDATA\SpeekToMe"
  RMDir /r /REBOOTOK "$APPDATA\com.speektome.app"
  RMDir /r /REBOOTOK "$LOCALAPPDATA\com.speektome.app"
  RMDir /r /REBOOTOK "$APPDATA\26746SumedhBengale.SpeekTo.Me"
  RMDir /r /REBOOTOK "$LOCALAPPDATA\26746SumedhBengale.SpeekTo.Me"

  DetailPrint "Cleaning up user profile AppData & WebView2 storage..."
  nsExec::Exec 'powershell -Command "Remove-Item -Path \"$env:APPDATA\SpeekToMe\" -Recurse -Force -ErrorAction SilentlyContinue"'
  nsExec::Exec 'powershell -Command "Remove-Item -Path \"$env:LOCALAPPDATA\SpeekToMe\" -Recurse -Force -ErrorAction SilentlyContinue"'
  nsExec::Exec 'powershell -Command "Remove-Item -Path \"$env:APPDATA\26746SumedhBengale.SpeekTo.Me\" -Recurse -Force -ErrorAction SilentlyContinue"'
  nsExec::Exec 'powershell -Command "Remove-Item -Path \"$env:LOCALAPPDATA\26746SumedhBengale.SpeekTo.Me\" -Recurse -Force -ErrorAction SilentlyContinue"'
  nsExec::Exec 'powershell -Command "Get-ChildItem -Path \"C:\Users\*\AppData\Roaming\SpeekToMe\" | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue"'
  nsExec::Exec 'powershell -Command "Get-ChildItem -Path \"C:\Users\*\AppData\Local\SpeekToMe\" | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue"'
!macroend
