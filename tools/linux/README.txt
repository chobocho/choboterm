choboterm 리눅스 버전 (x86_64)

실행:   ./choboterm
설치:   ./install.sh   (~/.local/bin/choboterm 과 프로그램 메뉴 항목)

필요한 것: GTK 3, WebKitGTK 4.1 (glibc 2.35 이상)
  Ubuntu / Debian:  sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0
  Fedora:           sudo dnf install gtk3 webkit2gtk4.1

있으면 좋은 것:
  - 키링(GNOME Keyring, KWallet): Telnet·세션 비밀번호 저장. 없으면 비밀번호를 저장하지 않습니다
  - 글꼴: D2Coding 또는 fonts-noto-cjk(한글), fonts-noto-color-emoji
  - xdg-utils(로그·스크립트 폴더 열기), docker 또는 podman

설정: ~/.config/choboterm   로그·스크립트: ~/Documents/choboterm
반투명 창과 PuTTY 세션 가져오기는 Windows에서만 됩니다.
