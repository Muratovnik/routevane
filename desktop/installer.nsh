!macro customInstall
  FileOpen $0 "$INSTDIR\resources\desktop-installed" w
  FileWrite $0 "nsis"
  FileClose $0
!macroend
