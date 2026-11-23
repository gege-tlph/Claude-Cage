; Use the Windows display language instead of the user's number/date locale:
; Simplified Chinese for any Chinese display language, English otherwise.
!macro CageSelectInstallerLanguage
  Push $0
  System::Call 'kernel32::GetUserDefaultUILanguage() i .r0'
  IntOp $0 $0 & 0x03ff
  ${If} $0 = 4
    StrCpy $LANGUAGE ${LANG_SIMPCHINESE}
  ${Else}
    StrCpy $LANGUAGE ${LANG_ENGLISH}
  ${EndIf}
  Pop $0
!macroend
