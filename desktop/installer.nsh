; The template keeps an existing shortcut untouched on update (its
; keepShortcuts mechanism), so a link left pointing at another Routevane.exe,
; such as a development build, would survive every update and open that build.
; Re-point a shortcut that already exists at the executable just installed; a
; shortcut the operator deleted stays deleted.
!macro routevaneRepointShortcut LINK
  ${if} ${FileExists} "${LINK}"
    CreateShortCut "${LINK}" "$appExe" "" "$appExe" 0 "" "" "${APP_DESCRIPTION}"
    ClearErrors
    WinShell::SetLnkAUMI "${LINK}" "${APP_ID}"
  ${endIf}
!macroend

; Runs after the template's addStartMenuLink/addDesktopLink, so a link the
; template created or renamed is already at its new path.
!macro customInstall
  FileOpen $0 "$INSTDIR\resources\desktop-installed" w
  FileWrite $0 "nsis"
  FileClose $0
  !insertmacro routevaneRepointShortcut "$newStartMenuLink"
  !insertmacro routevaneRepointShortcut "$newDesktopLink"
  System::Call 'Shell32::SHChangeNotify(i 0x8000000, i 0, i 0, i 0)'
!macroend
