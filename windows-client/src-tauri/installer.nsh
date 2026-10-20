!macro NSIS_HOOK_PREUNINSTALL
  ; Override Tauri's optional recursive AppData deletion as well.
  StrCpy $DeleteAppDataCheckboxState 0
  DetailPrint "Stopping SpeekTo.Me. Recordings and user settings will be preserved."
  nsExec::Exec 'taskkill /IM speektome.exe'
  nsExec::Exec 'taskkill /IM SpeekTo.Me.exe'
!macroend
